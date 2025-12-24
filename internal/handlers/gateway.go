package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"thyris-sz/internal/ai"
	"thyris-sz/internal/config"
	"thyris-sz/internal/guardrails"
	"thyris-sz/internal/models"
)

// NewOpenAIChatGateway returns an HTTP handler that exposes an OpenAI-compatible
// /v1/chat/completions endpoint.
//
// Flow:
//  1. Parse the incoming OpenAI-style chat request (model, messages, stream, ...)
//  2. Run TSZ detection/guardrails on user messages (input guardrails)
//  3. Optionally block or redact the request
//  4. Forward the sanitized request to the upstream OpenAI-compatible endpoint
//  5. For non-streaming calls, optionally apply guardrails on assistant output
//  6. For streaming calls, proxy the upstream event-stream and, depending on headers,
//     optionally apply output guardrails in a streaming-safe way (see stream modes below).
func NewOpenAIChatGateway(detector *guardrails.Detector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeOpenAIError(w, http.StatusMethodNotAllowed, "Method not allowed", "method_not_allowed")
			return
		}

		// 1) Parse payload and stream flag
		payload, stream, err := parseChatGatewayPayload(r)
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}

		messages, ok := payload["messages"].([]interface{})
		if !ok || len(messages) == 0 {
			writeOpenAIError(w, http.StatusBadRequest, "'messages' array is required", "invalid_request_error")
			return
		}

		// 2) Extract metadata (RID, guardrails list, streaming options)
		rid, guardrailsList := extractGatewayMetadata(r)
		mode, onFail := extractGatewayStreamOptions(r)
		log.Printf("[gateway] RID=%s stream=%v mode=%s onFail=%s guardrails=%v gateway_block_mode=%s", rid, stream, mode, onFail, guardrailsList, config.AppConfig.GatewayBlockMode)

		// 3) Apply input guardrails on user messages
		sanitizedMessages, blocked, blockMessage, inputDetects := applyInputGuardrails(detector, messages, rid, guardrailsList)
		if blocked {
			triggeredGuardrails := computeTriggeredGuardrails(inputDetects, nil)
			log.Printf("[gateway] RID=%s blocked on input guardrails: %s (gateway_block_mode=%s, guardrails=%v)", rid, blockMessage, config.AppConfig.GatewayBlockMode, triggeredGuardrails)

			// BLOCK mode: hard fail with HTTP error
			if config.AppConfig.GatewayBlockMode == "BLOCK" {
				meta := map[string]interface{}{
					"rid":        rid,
					"guardrails": triggeredGuardrails,
					"input":      inputDetects,
				}

				writeOpenAIErrorWithMeta(w, http.StatusBadRequest, blockMessage, "tsz_content_blocked", meta)
				return
			}

		}

		payload["messages"] = sanitizedMessages

		// Provider streaming compatibility check
		// If the client requests streaming but the configured provider does not support it,
		// return a clear error rather than proxying a non-streaming response over SSE.
		if stream {
			provider := ai.GetProvider()
			if provider != nil && !provider.SupportsStreaming() {
				writeOpenAIError(w, http.StatusBadRequest, "Streaming is currently not supported for this provider integration.", "streaming_not_supported")
				return
			}
		}

		// 4) Forward request to upstream (via provider or direct HTTP)
		var upstreamResp *http.Response

		provider := ai.GetProvider()
		if provider != nil {
			// Use the configured provider
			log.Printf("[gateway] RID=%s using provider: %s", rid, provider.Name())
			forwarder := ai.AsOpenAIForwarder(provider)
			if forwarder != nil {
				upstreamResp, err = forwarder.ForwardRequest(r.Context(), payload)
				if err != nil {
					log.Printf("[gateway] RID=%s provider forward failed: %v", rid, err)
					writeOpenAIError(w, http.StatusBadGateway, "Failed to reach upstream LLM service", "upstream_unreachable")
					return
				}
			} else {
				// Provider doesn't support forwarding, fall back to direct HTTP
				upstreamResp, err = sendDirectUpstreamRequest(payload)
				if err != nil {
					log.Printf("[gateway] RID=%s upstream LLM request failed: %v", rid, err)
					writeOpenAIError(w, http.StatusBadGateway, "Failed to reach upstream LLM service", "upstream_unreachable")
					return
				}
			}
		} else {
			// No provider configured, use direct HTTP
			upstreamResp, err = sendDirectUpstreamRequest(payload)
			if err != nil {
				log.Printf("[gateway] RID=%s upstream LLM request failed: %v", rid, err)
				writeOpenAIError(w, http.StatusBadGateway, "Failed to reach upstream LLM service", "upstream_unreachable")
				return
			}
		}
		defer upstreamResp.Body.Close()

		log.Printf("[gateway] RID=%s upstream_status=%d stream=%v", rid, upstreamResp.StatusCode, stream)

		if stream {
			// Streaming mode: choose strategy based on headers
			switch mode {
			case "stream-sync":
				streamWithOutputGuardrails(detector, rid, guardrailsList, upstreamResp, w, onFail)
			case "stream-async":
				proxyStreamWithAsyncValidation(detector, rid, guardrailsList, upstreamResp, w)
			default: // "final-only" or unknown
				proxyStreamResponse(w, upstreamResp)
			}
			return
		}

		// Non-streaming: apply output guardrails on the full assistant response
		processNonStreamResponse(detector, rid, guardrailsList, upstreamResp, w, inputDetects)
		log.Printf("[gateway] RID=%s non-stream response completed with status=%d", rid, upstreamResp.StatusCode)
	}
}

// parseChatGatewayPayload parses the incoming JSON body and extracts the payload + stream flag.
func parseChatGatewayPayload(r *http.Request) (map[string]interface{}, bool, error) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return nil, false, err
	}

	stream := false
	if streamVal, ok := payload["stream"].(bool); ok && streamVal {
		stream = true
	}

	return payload, stream, nil
}

// extractGatewayMetadata derives RID and guardrails list from headers.
func extractGatewayMetadata(r *http.Request) (string, []string) {
	rid := r.Header.Get("X-TSZ-RID")
	if rid == "" {
		rid = "LLM-GW-" + time.Now().Format("20060102T150405.000")
	}

	var guardrailsList []string
	if hdr := r.Header.Get("X-TSZ-Guardrails"); hdr != "" {
		for _, g := range strings.Split(hdr, ",") {
			if trimmed := strings.TrimSpace(g); trimmed != "" {
				guardrailsList = append(guardrailsList, trimmed)
			}
		}
	}

	return rid, guardrailsList
}

// extractGatewayStreamOptions reads streaming-related options from headers.
//
// X-TSZ-Guardrails-Mode:
//   - "final-only" (default): only input + non-stream output guardrails
//   - "stream-sync": apply output guardrails while streaming (validated output)
//   - "stream-async": proxy raw stream, validate asynchronously for logging/SIEM
//
// X-TSZ-Guardrails-OnFail:
//   - "filter" (default): redact unsafe parts and continue streaming
//   - "halt": stop streaming and send an error event
func extractGatewayStreamOptions(r *http.Request) (mode, onFail string) {
	mode = strings.ToLower(strings.TrimSpace(r.Header.Get("X-TSZ-Guardrails-Mode")))
	if mode == "" {
		mode = "final-only"
	}

	onFail = strings.ToLower(strings.TrimSpace(r.Header.Get("X-TSZ-Guardrails-OnFail")))
	if onFail == "" {
		onFail = "filter"
	}

	return mode, onFail
}

// applyInputGuardrails runs detection/guardrails on user messages and returns sanitized messages.
func applyInputGuardrails(detector *guardrails.Detector, messages []interface{}, rid string, guardrailsList []string) ([]interface{}, bool, string, []models.DetectResponse) {
	blocked := false
	blockMessage := ""
	var detectResponses []models.DetectResponse

	for i, rm := range messages {
		msgMap, ok := rm.(map[string]interface{})
		if !ok {
			continue
		}

		role, _ := msgMap["role"].(string)
		content, _ := msgMap["content"].(string)
		if content == "" {
			continue
		}

		// For now we only scan user messages
		if role != "user" {
			continue
		}

		resp := detector.Detect(models.DetectRequest{
			Text:       content,
			RID:        rid,
			Guardrails: guardrailsList,
		})

		detectResponses = append(detectResponses, resp)

		logGatewayDetectSummary("input", rid, resp)

		if resp.Blocked {
			blocked = true
			if resp.Message != "" {
				blockMessage = resp.Message
			} else {
				blockMessage = "Request blocked by TSZ security policy"
			}
			break
		}

		if resp.RedactedText != "" {
			msgMap["content"] = resp.RedactedText
			messages[i] = msgMap
		}
	}

	return messages, blocked, blockMessage, detectResponses
}

// sendDirectUpstreamRequest sends a direct HTTP request to the upstream OpenAI-compatible endpoint.
// This is used when no provider is configured or for backward compatibility.
func sendDirectUpstreamRequest(payload map[string]interface{}) (*http.Response, error) {
	forwardBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	upstreamURL := strings.TrimRight(config.AppConfig.AIModelURL, "/") + "/chat/completions"

	req, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(forwardBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if config.AppConfig.AIAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+config.AppConfig.AIAPIKey)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	return client.Do(req)
}

// processNonStreamResponse reads the upstream JSON response and applies output guardrails.
func processNonStreamResponse(detector *guardrails.Detector, rid string, guardrailsList []string, upstreamResp *http.Response, w http.ResponseWriter, inputDetects []models.DetectResponse) {
	upstreamBody, err := io.ReadAll(upstreamResp.Body)
	if err != nil {
		log.Printf("Failed to read upstream response body: %v", err)
		writeOpenAIError(w, http.StatusBadGateway, "Failed to read upstream LLM response", "upstream_read_error")
		return
	}

	var upstreamPayload map[string]interface{}
	var outputDetects []models.DetectResponse
	if err := json.Unmarshal(upstreamBody, &upstreamPayload); err == nil {
		choicesRaw, ok := upstreamPayload["choices"].([]interface{})
		if ok {
			for i, ch := range choicesRaw {
				choiceMap, ok := ch.(map[string]interface{})
				if !ok {
					continue
				}

				msg, ok := choiceMap["message"].(map[string]interface{})
				if !ok {
					continue
				}

				content, _ := msg["content"].(string)
				if content == "" {
					continue
				}

				// Output guardrails
				outResp := detector.Detect(models.DetectRequest{
					Text:       content,
					RID:        rid + "-OUT",
					Guardrails: guardrailsList,
				})

				outputDetects = append(outputDetects, outResp)

				logGatewayDetectSummary("output-nonstream", rid, outResp)

				if outResp.Blocked {
					msgText := outResp.Message
					if msgText == "" {
						msgText = "Assistant response blocked by TSZ security policy"
					}

					triggeredGuardrails := computeTriggeredGuardrails(inputDetects, outputDetects)
					log.Printf("[gateway] RID=%s blocked on output guardrails: %s (gateway_block_mode=%s, guardrails=%v)", rid, msgText, config.AppConfig.GatewayBlockMode, triggeredGuardrails)

					if config.AppConfig.GatewayBlockMode == "BLOCK" {
						meta := map[string]interface{}{
							"rid":        rid,
							"guardrails": triggeredGuardrails,
							"input":      inputDetects,
							"output":     outputDetects,
						}

						writeOpenAIErrorWithMeta(w, http.StatusBadRequest, msgText, "tsz_output_blocked", meta)
						return
					}

				}

				if outResp.RedactedText != "" {
					msg["content"] = outResp.RedactedText
					choiceMap["message"] = msg
					choicesRaw[i] = choiceMap
				}
			}

			triggeredGuardrails := computeTriggeredGuardrails(inputDetects, outputDetects)
			meta := map[string]interface{}{
				"rid":        rid,
				"guardrails": triggeredGuardrails,
				"input":      inputDetects,
				"output":     outputDetects,
			}

			upstreamPayload["tsz_meta"] = meta

			if sanitizedBody, err := json.Marshal(upstreamPayload); err == nil {
				upstreamBody = sanitizedBody
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(upstreamResp.StatusCode)
	if _, err := w.Write(upstreamBody); err != nil {
		log.Printf("Failed to write gateway response body: %v", err)
	}
}

// writeOpenAIError writes an error in OpenAI-compatible format.
func logGatewayDetectSummary(stage string, rid string, resp models.DetectResponse) {
	// Build a compact breakdown string similar to /detect audit logs.
	breakdownParts := make([]string, 0, len(resp.Breakdown))
	total := 0
	for t, c := range resp.Breakdown {
		breakdownParts = append(breakdownParts, t+": "+fmt.Sprintf("%d", c))
		total += c
	}
	breakdownStr := strings.Join(breakdownParts, ", ")
	if breakdownStr == "" {
		breakdownStr = "None"
	}

	log.Printf("[gateway-detect] stage=%s RID=%s blocked=%v contains_pii=%v total=%d breakdown={%s} message=%q overall_confidence=%.2f",
		stage,
		rid,
		resp.Blocked,
		resp.ContainsPII,
		total,
		breakdownStr,
		resp.Message,
		float64(resp.OverallConfidence),
	)

	if len(resp.Detections) > 0 {
		first := resp.Detections[0]
		// Do NOT log raw PII values; only log type, placeholder and score for observability.
		log.Printf("[gateway-detect] stage=%s RID=%s first_detection type=%s placeholder=%q score=%.2f",
			stage,
			rid,
			first.Type,
			first.Placeholder,
			float64(first.ConfidenceScore),
		)
	}

	if len(resp.ValidatorResults) > 0 {
		v := resp.ValidatorResults[0]
		log.Printf("[gateway-detect] stage=%s RID=%s first_validator name=%s passed=%v score=%.2f",
			stage,
			rid,
			v.Name,
			v.Passed,
			float64(v.ConfidenceScore),
		)
	}
}

// computeTriggeredGuardrails returns only guardrail names that actually produced a signal
// (i.e. at least one ValidatorResult with Passed == false) across input and output DetectResponses.
func computeTriggeredGuardrails(inputs []models.DetectResponse, outputs []models.DetectResponse) []string {
	seen := make(map[string]struct{})

	collect := func(list []models.DetectResponse) {
		for _, dr := range list {
			for _, v := range dr.ValidatorResults {
				if !v.Passed {
					seen[v.Name] = struct{}{}
				}
			}
		}
	}

	if len(inputs) > 0 {
		collect(inputs)
	}
	if len(outputs) > 0 {
		collect(outputs)
	}

	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}

	if len(result) > 1 {
		sort.Strings(result)
	}

	return result
}

// writeOpenAIError writes an error in OpenAI-compatible format.
func writeOpenAIError(w http.ResponseWriter, status int, message string, code string) {
	writeOpenAIErrorWithMeta(w, status, message, code, nil)
}

// writeOpenAIErrorWithMeta writes an error in OpenAI-compatible format and optionally attaches TSZ metadata.
func writeOpenAIErrorWithMeta(w http.ResponseWriter, status int, message string, code string, meta map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	body := map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "invalid_request_error",
			"param":   nil,
			"code":    code,
		},
	}

	if meta != nil {
		body["tsz_meta"] = meta
	}

	_ = json.NewEncoder(w).Encode(body)
}
