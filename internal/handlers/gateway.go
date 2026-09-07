package handlers

import (
	"bytes"
	"context"
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
//  2. Run TSZ detection/guardrails on messages and tool payloads (input guardrails)
//  3. Optionally block or redact the request
//  4. Forward the sanitized request to the upstream OpenAI-compatible endpoint
//  5. For non-streaming calls, optionally apply guardrails on assistant output
//  6. For streaming calls, proxy the upstream event-stream and, depending on headers,
//     optionally apply output guardrails in a streaming-safe way (see stream modes below).
func NewOpenAIChatGateway(service guardrails.GuardrailService) http.HandlerFunc {
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

		// 3) Apply input guardrails on messages and tool payloads
		sanitizedMessages, blocked, blockMessage, inputDetects := applyInputGuardrails(r.Context(), service, messages, rid, guardrailsList)
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
				streamWithOutputGuardrails(service, rid, guardrailsList, upstreamResp, w, onFail)
			case "stream-async":
				proxyStreamWithAsyncValidation(service, rid, guardrailsList, upstreamResp, w)
			default: // "final-only" or unknown
				proxyStreamResponse(w, upstreamResp)
			}
			return
		}

		// Non-streaming: apply output guardrails on the full assistant response
		processNonStreamResponse(r.Context(), service, rid, guardrailsList, upstreamResp, w, inputDetects)
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

// applyInputGuardrails scans supported message content, tool-call arguments,
// and tool results before the request is forwarded upstream.
func applyInputGuardrails(ctx context.Context, service guardrails.GuardrailService, messages []interface{}, rid string, guardrailsList []string) ([]interface{}, bool, string, []models.DetectResponse) {
	blocked := false
	blockMessage := ""
	var detectResponses []models.DetectResponse

	for i, rm := range messages {
		msgMap, ok := rm.(map[string]interface{})
		if !ok {
			continue
		}

		inspect := func(text string, apply func(string)) bool {
			if text == "" {
				return true
			}
			resp, err := guardrails.DetectLegacy(ctx, service, models.DetectRequest{Text: text, RID: rid, Guardrails: guardrailsList})
			if err != nil {
				blocked, blockMessage = true, "Guardrail inspection failed"
				return false
			}
			detectResponses = append(detectResponses, resp)
			logGatewayDetectSummary("input", rid, resp)
			if resp.Blocked {
				blocked, blockMessage = true, resp.Message
				if blockMessage == "" {
					blockMessage = "Request blocked by TSZ security policy"
				}
				return false
			}
			if resp.RedactedText != "" {
				apply(resp.RedactedText)
			}
			return true
		}

		role, _ := msgMap["role"].(string)
		if role == "tool" {
			if _, ok := msgMap["tool_call_id"].(string); !ok {
				blocked, blockMessage = true, "Unsupported tool result payload"
				break
			}
		}
		if role == "user" || role == "developer" || role == "system" || role == "assistant" || role == "tool" {
			refusalContent := false
			if rawRefusal, present := msgMap["refusal"]; present {
				if role != "assistant" {
					blocked, blockMessage = true, "Unsupported message refusal payload"
					break
				}
				switch refusal := rawRefusal.(type) {
				case string:
					refusalContent = true
					if !inspect(refusal, func(value string) { msgMap["refusal"] = value }) {
						break
					}
				case nil:
				default:
					blocked, blockMessage = true, "Unsupported message refusal payload"
				}
				if blocked {
					break
				}
			}
			switch content := msgMap["content"].(type) {
			case string:
				if !inspect(content, func(value string) { msgMap["content"] = value }) {
					break
				}
			case []interface{}:
				if len(content) == 0 {
					blocked, blockMessage = true, "Unsupported multimodal content payload"
					break
				}
				for partIndex, rawPart := range content {
					part, partOK := rawPart.(map[string]interface{})
					partType, typeOK := part["type"].(string)
					if !partOK || !typeOK {
						blocked, blockMessage = true, "Unsupported multimodal content payload"
						break
					}
					field := ""
					switch partType {
					case "text":
						field = "text"
					case "refusal":
						if role == "assistant" {
							field = "refusal"
						}
					case "image_url", "input_audio", "file":
						if role == "user" {
							if _, hasText := part["text"]; !hasText {
								continue
							}
						}
					}
					text, textOK := part[field].(string)
					if field == "" || !textOK {
						blocked, blockMessage = true, "Unsupported multimodal content payload"
						break
					}
					targetPart := part
					if !inspect(text, func(value string) { targetPart[field] = value }) {
						break
					}
					content[partIndex] = part
				}
				if blocked {
					break
				}
			case nil:
				if role != "assistant" || (msgMap["tool_calls"] == nil && !refusalContent) {
					blocked, blockMessage = true, "Unsupported message content payload"
				}
			default:
				blocked, blockMessage = true, "Unsupported message content payload"
			}
			if blocked {
				break
			}
		}
		if role == "assistant" {
			toolCalls, ok := msgMap["tool_calls"].([]interface{})
			if _, present := msgMap["tool_calls"]; present && !ok {
				blocked, blockMessage = true, "Unsupported tool call payload"
				break
			}
			for _, rawCall := range toolCalls {
				call, callOK := rawCall.(map[string]interface{})
				function, functionOK := call["function"].(map[string]interface{})
				_, idOK := call["id"].(string)
				callType, typeOK := call["type"].(string)
				_, nameOK := function["name"].(string)
				arguments, argumentsOK := function["arguments"].(string)
				if !callOK || !functionOK || !idOK || !typeOK || callType != "function" || !nameOK || !argumentsOK {
					blocked, blockMessage = true, "Unsupported tool call payload"
					break
				}
				if !inspect(arguments, func(value string) { function["arguments"] = value }) {
					break
				}
			}
			if blocked {
				break
			}
		}
		messages[i] = msgMap
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
func processNonStreamResponse(ctx context.Context, service guardrails.GuardrailService, rid string, guardrailsList []string, upstreamResp *http.Response, w http.ResponseWriter, inputDetects []models.DetectResponse) {
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

				type outputTarget struct {
					text  string
					apply func(string)
				}
				var targets []outputTarget
				refusalContent := false
				if rawRefusal, present := msg["refusal"]; present {
					switch refusal := rawRefusal.(type) {
					case string:
						refusalContent = true
						targets = append(targets, outputTarget{text: refusal, apply: func(value string) { msg["refusal"] = value }})
					case nil:
					default:
						writeOpenAIError(w, http.StatusInternalServerError, "Unsupported upstream message refusal payload", "guardrail_error")
						return
					}
				}
				switch content := msg["content"].(type) {
				case string:
					if content != "" {
						targets = append(targets, outputTarget{text: content, apply: func(value string) { msg["content"] = value }})
					}
				case []interface{}:
					if len(content) == 0 {
						writeOpenAIError(w, http.StatusInternalServerError, "Unsupported upstream multimodal content payload", "guardrail_error")
						return
					}
					for _, rawPart := range content {
						part, partOK := rawPart.(map[string]interface{})
						partType, typeOK := part["type"].(string)
						if !partOK || !typeOK || (partType != "text" && partType != "refusal") {
							writeOpenAIError(w, http.StatusInternalServerError, "Unsupported upstream multimodal content payload", "guardrail_error")
							return
						}
						field := "text"
						if partType == "refusal" {
							field = "refusal"
						}
						text, textOK := part[field].(string)
						if !textOK {
							writeOpenAIError(w, http.StatusInternalServerError, "Unsupported upstream multimodal content payload", "guardrail_error")
							return
						}
						targetPart, targetField := part, field
						targets = append(targets, outputTarget{text: text, apply: func(value string) { targetPart[targetField] = value }})
					}
				case nil:
					if msg["tool_calls"] == nil && !refusalContent {
						writeOpenAIError(w, http.StatusInternalServerError, "Unsupported upstream message content payload", "guardrail_error")
						return
					}
				default:
					writeOpenAIError(w, http.StatusInternalServerError, "Unsupported upstream message content payload", "guardrail_error")
					return
				}
				toolCalls, toolCallsOK := msg["tool_calls"].([]interface{})
				if _, present := msg["tool_calls"]; present && !toolCallsOK {
					writeOpenAIError(w, http.StatusInternalServerError, "Unsupported upstream tool call payload", "guardrail_error")
					return
				}
				for _, rawCall := range toolCalls {
					call, callOK := rawCall.(map[string]interface{})
					function, functionOK := call["function"].(map[string]interface{})
					_, idOK := call["id"].(string)
					callType, typeOK := call["type"].(string)
					_, nameOK := function["name"].(string)
					arguments, argumentsOK := function["arguments"].(string)
					if !callOK || !functionOK || !idOK || !typeOK || callType != "function" || !nameOK || !argumentsOK {
						writeOpenAIError(w, http.StatusInternalServerError, "Unsupported upstream tool call payload", "guardrail_error")
						return
					}
					targetFunction := function
					targets = append(targets, outputTarget{text: arguments, apply: func(value string) { targetFunction["arguments"] = value }})
				}

				for _, target := range targets {
					outResp, inspectErr := guardrails.DetectLegacy(ctx, service, models.DetectRequest{Text: target.text, RID: rid + "-OUT", Guardrails: guardrailsList})
					if inspectErr != nil {
						writeOpenAIError(w, http.StatusInternalServerError, "Guardrail inspection failed", "guardrail_error")
						return
					}
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
							meta := map[string]interface{}{"rid": rid, "guardrails": triggeredGuardrails, "input": inputDetects, "output": outputDetects}
							writeOpenAIErrorWithMeta(w, http.StatusBadRequest, msgText, "tsz_output_blocked", meta)
							return
						}
					}
					if outResp.RedactedText != "" {
						target.apply(outResp.RedactedText)
					}
				}
				choiceMap["message"] = msg
				choicesRaw[i] = choiceMap
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
