package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"thyris-sz/internal/config"
	"thyris-sz/internal/guardrails"
	"thyris-sz/internal/models"
)

// proxyStreamResponse proxies the upstream streaming body as-is to the client.
func proxyStreamResponse(w http.ResponseWriter, resp *http.Response) {
	// Preserve Content-Type (e.g. text/event-stream)
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.WriteHeader(resp.StatusCode)

	if flusher, ok := w.(http.Flusher); ok {
		buf := make([]byte, 4096)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				if _, err := w.Write(buf[:n]); err != nil {
					log.Printf("[gateway-stream] Failed to write streaming chunk: %v", err)
					break
				}
				flusher.Flush()
			}
			if readErr != nil {
				if readErr != io.EOF {
					log.Printf("[gateway-stream] Error reading streaming response body: %v", readErr)
				}
				break
			}
		}
	} else {
		if _, err := io.Copy(w, resp.Body); err != nil {
			log.Printf("[gateway-stream] Failed to proxy streaming body: %v", err)
		}
	}
}

// streamWithOutputGuardrails proxies a streaming response while applying output guardrails
// on the accumulated assistant content and streaming only the sanitized output.
func streamWithOutputGuardrails(
	service guardrails.GuardrailService,
	rid string,
	guardrailsList []string,
	upstreamResp *http.Response,
	w http.ResponseWriter,
	onFail string,
) {
	if ct := upstreamResp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(upstreamResp.StatusCode)

	flusher, ok := w.(http.Flusher)
	if !ok {
		// Fallback: if flusher is not available, proxy as-is.
		log.Printf("[gateway-stream] http.Flusher not supported by response writer; falling back to raw proxy")
		proxyStreamResponse(w, upstreamResp)
		return
	}

	maxBuf := config.AppConfig.StreamMaxBufferBytes
	failMode := strings.ToUpper(config.AppConfig.StreamFailMode)

	var rawBuffer strings.Builder      // raw assistant content from upstream
	var validatedSoFar strings.Builder // sanitized content already sent to the client

	reader := bufio.NewReader(upstreamResp.Body)

	log.Printf("[gateway-stream] RID=%s mode=stream-sync guardrails=%v onFail=%s maxBuf=%d failMode=%s", rid, guardrailsList, onFail, maxBuf, failMode)

	for {
		select {
		case <-upstreamResp.Request.Context().Done():
			log.Printf("[gateway-stream] upstream context canceled for RID=%s: %v", rid, upstreamResp.Request.Context().Err())
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")

			if strings.HasPrefix(trimmed, "data: ") {
				jsonPart := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))

				// Forward [DONE] as-is.
				if jsonPart == "[DONE]" {
					log.Printf("[gateway-stream] RID=%s received [DONE]", rid)
					if _, writeErr := w.Write([]byte(line)); writeErr != nil {
						log.Printf("[gateway-stream] Failed to write [DONE] event: %v", writeErr)
					}
					flusher.Flush()
					break
				}

				if jsonPart == "" {
					// Empty data event, forward as-is.
					if _, writeErr := w.Write([]byte(line)); writeErr != nil {
						log.Printf("[gateway-stream] Failed to write empty data event: %v", writeErr)
					}
					flusher.Flush()
					continue
				}

				var event map[string]interface{}
				if err := json.Unmarshal([]byte(jsonPart), &event); err != nil {
					msg := "Failed to parse upstream SSE JSON"
					log.Printf("[gateway-stream] %s RID=%s error=%v", msg, rid, err)
					if failMode == "STRICT" {
						writeStreamErrorEvent(w, flusher, msg)
						return
					}
					// LENIENT: forward raw line.
					if _, writeErr := w.Write([]byte(line)); writeErr != nil {
						log.Printf("[gateway-stream] Failed to write unparsed SSE line: %v", writeErr)
					}
					flusher.Flush()
					continue
				}

				contentDelta := extractDeltaContent(event)
				if contentDelta == "" {
					// No content in this event; forward as-is.
					if _, writeErr := w.Write([]byte(line)); writeErr != nil {
						log.Printf("[gateway-stream] Failed to write SSE line without content: %v", writeErr)
					}
					flusher.Flush()
					continue
				}

				// Append new content to the raw buffer.
				rawBuffer.WriteString(contentDelta)

				// Enforce max buffer size if configured.
				if maxBuf > 0 && rawBuffer.Len() > maxBuf {
					// Keep only the last maxBuf bytes to bound memory usage.
					raw := rawBuffer.String()
					if len(raw) > maxBuf {
						trimmedRaw := raw[len(raw)-maxBuf:]
						rawBuffer.Reset()
						rawBuffer.WriteString(trimmedRaw)
						log.Printf("[gateway-stream] RID=%s raw buffer truncated to %d bytes", rid, maxBuf)
					}
				}

				blocked, sanitized, errMsg := runOutputGuardrails(service, rid, guardrailsList, rawBuffer.String(), onFail)
				if blocked {
					log.Printf("[gateway-stream] RID=%s output blocked by guardrails: %s", rid, errMsg)
					writeStreamErrorEvent(w, flusher, errMsg)
					return
				}

				// Compute the new portion that has not yet been sent to the client.
				if len(sanitized) < validatedSoFar.Len() {
					// This should never happen; log and continue without sending new data.
					log.Printf("[gateway-stream] RID=%s sanitized output length (%d) is smaller than already streamed length (%d)", rid, len(sanitized), validatedSoFar.Len())
					continue
				}

				newDelta := sanitized[validatedSoFar.Len():]
				if len(newDelta) == 0 {
					// Nothing new to send (e.g. only earlier content was sanitized).
					continue
				}

				setDeltaContent(event, newDelta)
				payload, err := json.Marshal(event)
				if err != nil {
					log.Printf("[gateway-stream] Failed to marshal sanitized SSE event RID=%s: %v", rid, err)
					continue
				}

				if _, writeErr := w.Write([]byte("data: ")); writeErr != nil {
					log.Printf("[gateway-stream] Failed to write SSE data prefix RID=%s: %v", rid, writeErr)
					return
				}
				if _, writeErr := w.Write(payload); writeErr != nil {
					log.Printf("[gateway-stream] Failed to write SSE payload RID=%s: %v", rid, writeErr)
					return
				}
				if _, writeErr := w.Write([]byte("\n\n")); writeErr != nil {
					log.Printf("[gateway-stream] Failed to write SSE newline RID=%s: %v", rid, writeErr)
					return
				}

				flusher.Flush()
				validatedSoFar.WriteString(newDelta)
				continue
			}

			// Non-data line (comments, empty lines, etc.) are forwarded as-is.
			if _, writeErr := w.Write([]byte(line)); writeErr != nil {
				log.Printf("[gateway-stream] Failed to write non-data SSE line RID=%s: %v", rid, writeErr)
				break
			}
			flusher.Flush()
		}

		if err != nil {
			if err != io.EOF {
				log.Printf("[gateway-stream] Error reading streaming response body with guardrails RID=%s: %v", rid, err)
			}
			break
		}
	}

	log.Printf("[gateway-stream] RID=%s stream-sync completed", rid)
}

// proxyStreamWithAsyncValidation proxies the upstream streaming response as-is to the client,
// while also capturing the full stream and running guardrails asynchronously for logging/SIEM.
func proxyStreamWithAsyncValidation(
	service guardrails.GuardrailService,
	rid string,
	guardrailsList []string,
	upstreamResp *http.Response,
	w http.ResponseWriter,
) {
	if ct := upstreamResp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.WriteHeader(upstreamResp.StatusCode)

	var buf bytes.Buffer

	if flusher, ok := w.(http.Flusher); ok {
		chunk := make([]byte, 4096)
		for {
			select {
			case <-upstreamResp.Request.Context().Done():
				log.Printf("[gateway-stream] upstream context canceled for RID=%s: %v", rid, upstreamResp.Request.Context().Err())
				return
			default:
			}

			n, readErr := upstreamResp.Body.Read(chunk)
			if n > 0 {
				if _, err := w.Write(chunk[:n]); err != nil {
					log.Printf("[gateway-stream] Failed to write streaming chunk RID=%s: %v", rid, err)
					break
				}
				if _, err := buf.Write(chunk[:n]); err != nil {
					log.Printf("[gateway-stream] Failed to buffer streaming chunk for async validation RID=%s: %v", rid, err)
				}
				flusher.Flush()
			}
			if readErr != nil {
				if readErr != io.EOF {
					log.Printf("[gateway-stream] Error reading streaming response body RID=%s: %v", rid, readErr)
				}
				break
			}
		}
	} else {
		if _, err := io.Copy(io.MultiWriter(w, &buf), upstreamResp.Body); err != nil {
			log.Printf("[gateway-stream] Failed to proxy streaming body (async validation) RID=%s: %v", rid, err)
		}
	}

	// Run validation asynchronously on the captured stream content.
	go func(all []byte, rid string, guards []string) {
		if len(guards) == 0 {
			return
		}

		text := string(all)
		log.Printf("[gateway-stream] RID=%s starting async output validation (bytes=%d, guardrails=%v)", rid, len(all), guards)
		_, _ = guardrails.DetectLegacy(context.Background(), service, models.DetectRequest{
			Text:       text,
			RID:        rid + "-OUT-ASYNC",
			Guardrails: guards,
		})
	}(buf.Bytes(), rid, guardrailsList)
}

// runOutputGuardrails applies guardrails to the full assistant text and returns a sanitized version.
// Depending on onFail, it may instruct the caller to halt streaming.
func runOutputGuardrails(
	service guardrails.GuardrailService,
	rid string,
	guardrailsList []string,
	text string,
	onFail string,
) (blocked bool, sanitized string, msg string) {
	// If no guardrails are configured, return the original text.
	if len(guardrailsList) == 0 {
		return false, text, ""
	}

	resp, err := guardrails.DetectLegacy(context.Background(), service, models.DetectRequest{
		Text:       text,
		RID:        rid + "-OUT-STREAM",
		Guardrails: guardrailsList,
	})
	if err != nil {
		return true, "", "Guardrail inspection failed"
	}

	if resp.Blocked && onFail == "halt" {
		msg = resp.Message
		if msg == "" {
			msg = "Assistant response blocked by TSZ security policy"
		}
		return true, "", msg
	}

	if resp.RedactedText != "" {
		return false, resp.RedactedText, ""
	}

	return false, text, ""
}

// extractDeltaContent extracts the first choice.delta.content value from an SSE event payload.
func extractDeltaContent(event map[string]interface{}) string {
	choicesRaw, ok := event["choices"].([]interface{})
	if !ok || len(choicesRaw) == 0 {
		return ""
	}

	choiceMap, ok := choicesRaw[0].(map[string]interface{})
	if !ok {
		return ""
	}

	delta, ok := choiceMap["delta"].(map[string]interface{})
	if !ok {
		return ""
	}

	content, _ := delta["content"].(string)
	return content
}

// setDeltaContent sets the first choice.delta.content value in an SSE event payload.
func setDeltaContent(event map[string]interface{}, content string) {
	choicesRaw, ok := event["choices"].([]interface{})
	if !ok || len(choicesRaw) == 0 {
		return
	}

	choiceMap, ok := choicesRaw[0].(map[string]interface{})
	if !ok {
		return
	}

	delta, ok := choiceMap["delta"].(map[string]interface{})
	if !ok {
		delta = make(map[string]interface{})
	}

	delta["content"] = content
	choiceMap["delta"] = delta
	choicesRaw[0] = choiceMap
	event["choices"] = choicesRaw
}

// writeStreamErrorEvent sends an OpenAI-style error payload over an existing SSE stream
// and terminates the stream with a [DONE] event.
func writeStreamErrorEvent(w http.ResponseWriter, flusher http.Flusher, message string) {
	if message == "" {
		message = "Assistant response blocked by TSZ security policy"
	}

	payload, err := json.Marshal(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "invalid_request_error",
			"param":   nil,
			"code":    "tsz_output_blocked",
		},
	})
	if err != nil {
		log.Printf("[gateway-stream] Failed to marshal stream error event: %v", err)
		return
	}

	if _, writeErr := w.Write([]byte("data: ")); writeErr != nil {
		log.Printf("[gateway-stream] Failed to write error SSE prefix: %v", writeErr)
		return
	}
	if _, writeErr := w.Write(payload); writeErr != nil {
		log.Printf("[gateway-stream] Failed to write error SSE payload: %v", writeErr)
		return
	}
	if _, writeErr := w.Write([]byte("\n\n")); writeErr != nil {
		log.Printf("[gateway-stream] Failed to write error SSE newline: %v", writeErr)
		return
	}

	if _, writeErr := w.Write([]byte("data: [DONE]\n\n")); writeErr != nil {
		log.Printf("[gateway-stream] Failed to write error SSE DONE event: %v", writeErr)
		return
	}

	flusher.Flush()
}
