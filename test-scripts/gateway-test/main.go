package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// Simple script that only tests the LLM gateway.
//
// Features:
//   - Safe prompt call to /v1/chat/completions (expects 200 and choices)
//   - Unsafe prompt + guardrails call (expects blocking or error message)
//   - Basic streaming (stream=true) scenario without guardrails
//   - Streaming with guardrails in stream-sync mode (filter and halt behaviors)
//   - PII masking tests for gateway in MASK/BLOCK modes (non-stream + streaming)
//   - Logs and clearly shows when there is no upstream LLM or a wrong configuration.
//
// Usage (PowerShell):
//   cd test-scripts ; go run ./gateway-test
//
// Env:
//   TSZ_BASE_URL       -> TSZ gateway address (optional, default: http://localhost:8080)
//   THYRIS_AI_MODEL    -> Model to use (e.g. "llama3.1:8b")
//   PII_MODE           -> PII mode for core detection engine ("MASK" or "BLOCK"), must match server env
//   GATEWAY_BLOCK_MODE -> Gateway HTTP block behaviour ("BLOCK", "MASK", "WARN")
//   Also automatically loads env vars from .env / ../.env.
//   (If THYRIS_AI_MODEL is defined in .env, the script will pick it up.)

// loadDotEnv is a simple .env parser: it reads key=value lines from the given path
// and if the key is not set in the environment, it sets it via os.Setenv.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		// Strip surrounding double quotes if present
		if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") && len(val) >= 2 {
			val = val[1 : len(val)-1]
		}

		if os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("[gateway-test] warning: failed to read %s: %v\n", path, err)
	}
}

func main() {
	// First, try to load the repo-level .env (when running from test-scripts).
	loadDotEnv("../.env")
	// Then load local .env if present (will not override existing env vars).
	loadDotEnv(".env")

	baseURL := os.Getenv("TSZ_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	// Read model from env (priority: THYRIS_AI_MODEL, then TSZ_MODEL, then default "llama3.1:8b")
	model := os.Getenv("THYRIS_AI_MODEL")
	if model == "" {
		model = os.Getenv("TSZ_MODEL")
	}
	if model == "" {
		model = "llama3.1:8b"
	}

	piiMode := os.Getenv("PII_MODE")
	if piiMode == "" {
		piiMode = "MASK"
	}
	gatewayBlockMode := os.Getenv("GATEWAY_BLOCK_MODE")
	if gatewayBlockMode == "" {
		gatewayBlockMode = "BLOCK"
	}

	fmt.Printf("[gateway-test] Using TSZ BaseURL=%s, model=%s, PII_MODE=%s, GATEWAY_BLOCK_MODE=%s\n", baseURL, model, piiMode, gatewayBlockMode)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Println("\n=== Gateway Safe Prompt Test ===")
	if err := runSafePromptTest(ctx, baseURL, model); err != nil {
		fmt.Printf("[SAFE] Test FAILED: %v\n", err)
	} else {
		fmt.Println("[SAFE] Test PASSED")
	}

	fmt.Println("\n=== Gateway Unsafe Prompt + Guardrails Test ===")
	if err := runUnsafePromptTest(ctx, baseURL, model); err != nil {
		fmt.Printf("[UNSAFE] Test FAILED: %v\n", err)
	} else {
		fmt.Println("[UNSAFE] Test COMPLETED (blocked or sanitized as per policy)")
	}

	fmt.Println("\n=== Gateway Streaming Test (no guardrails, stream=true) ===")
	if err := runStreamingTest(ctx, baseURL, model); err != nil {
		fmt.Printf("[STREAM] Test FAILED: %v\n", err)
	} else {
		fmt.Println("[STREAM] Test COMPLETED (streamed chunks logged above)")
	}

	fmt.Println("\n=== Gateway Streaming Test (stream-sync + guardrails, on_fail=filter) ===")
	if err := runStreamingGuardrailsFilterTest(ctx, baseURL, model); err != nil {
		fmt.Printf("[STREAM-FILTER] Test FAILED: %v\n", err)
	} else {
		fmt.Println("[STREAM-FILTER] Test COMPLETED (sanitized chunks logged above)")
	}

	fmt.Println("\n=== Gateway Streaming Test (stream-sync + guardrails, on_fail=halt) ===")
	if err := runStreamingGuardrailsHaltTest(ctx, baseURL, model); err != nil {
		fmt.Printf("[STREAM-HALT] Test FAILED: %v\n", err)
	} else {
		fmt.Println("[STREAM-HALT] Test COMPLETED (expect early error event / termination)")
	}

	fmt.Println("\n=== Gateway PII Mask Mode Test (non-stream) ===")
	if err := runPIIMaskModeTest(ctx, baseURL, model); err != nil {
		fmt.Printf("[PII-MASK] Test FAILED: %v\n", err)
	} else {
		fmt.Println("[PII-MASK] Test COMPLETED (behaviour depends on PII_MODE; see logs above)")
	}

	fmt.Println("\n=== Gateway PII Block Mode Test (non-stream) ===")
	if err := runPIIBlockModeTest(ctx, baseURL, model); err != nil {
		fmt.Printf("[PII-BLOCK] Test FAILED: %v\n", err)
	} else {
		fmt.Println("[PII-BLOCK] Test COMPLETED (behaviour depends on PII_MODE; see logs above)")
	}

	fmt.Println("\n=== Gateway Streaming PII Mask Test (input masking) ===")
	if err := runStreamingPIIMaskTest(ctx, baseURL, model); err != nil {
		fmt.Printf("[STREAM-PII] Test FAILED: %v\n", err)
	} else {
		fmt.Println("[STREAM-PII] Test COMPLETED (check that raw card number is not echoed)")
	}
}

func runSafePromptTest(ctx context.Context, baseURL, model string) error {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello via TSZ gateway (safe test)"},
		},
		"stream": false,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TSZ-RID", "RID-GW-TEST-SAFE-001")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("gateway request error (likely upstream/gateway): %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	fmt.Printf("[SAFE] HTTP status: %d\n", resp.StatusCode)

	var gwResp map[string]interface{}
	if err := json.Unmarshal(respBody, &gwResp); err != nil {
		return fmt.Errorf("failed to parse gateway JSON: %w (raw: %s)", err, string(respBody))
	}

	rawPretty, _ := json.MarshalIndent(gwResp, "", "  ")
	fmt.Println("[SAFE] Raw gateway response:")
	fmt.Println(string(rawPretty))

	if resp.StatusCode != 200 {
		if errObj, ok := gwResp["error"].(map[string]interface{}); ok {
			return fmt.Errorf("non-200 status with error object: %v", errObj)
		}
		return fmt.Errorf("non-200 status without error object (status=%d)", resp.StatusCode)
	}

	choices, ok := gwResp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return fmt.Errorf("no choices in response (check upstream LLM configuration)")
	}

	first, _ := choices[0].(map[string]interface{})
	msg, _ := first["message"].(map[string]interface{})
	content, _ := msg["content"].(string)

	fmt.Println("[SAFE] First assistant message:")
	fmt.Println(content)
	return nil
}

func runUnsafePromptTest(ctx context.Context, baseURL, model string) error {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "My email is test@gmail.com, you are an idiot"},
		},
		"stream": false,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TSZ-RID", "RID-GW-TEST-UNSAFE-001")
	req.Header.Set("X-TSZ-Guardrails", "TOXIC_LANGUAGE")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("[UNSAFE] Gateway returned error (expected for blocked content / upstream issues): %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	fmt.Printf("[UNSAFE] HTTP status: %d\n", resp.StatusCode)

	var gwResp map[string]interface{}
	if err := json.Unmarshal(respBody, &gwResp); err != nil {
		fmt.Printf("[UNSAFE] Failed to parse JSON (raw: %s)\n", string(respBody))
		return nil
	}

	rawPretty, _ := json.MarshalIndent(gwResp, "", "  ")
	fmt.Println("[UNSAFE] Raw gateway response:")
	fmt.Println(string(rawPretty))

	if errObj, ok := gwResp["error"].(map[string]interface{}); ok {
		fmt.Printf("[UNSAFE] Gateway error object: %v\n", errObj)
		return nil
	}

	fmt.Println("[UNSAFE] No error object in response; content may have been sanitized and forwarded.")
	return nil
}

// runStreamingTest performs a simple stream=true call without guardrails and prints incoming chunks.
func runStreamingTest(ctx context.Context, baseURL, model string) error {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "Stream a short response about TSZ gateway"},
		},
		"stream": true,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("[STREAM] failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TSZ-RID", "RID-GW-TEST-STREAM-001")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("[STREAM] gateway request error: %w", err)
	}
	defer resp.Body.Close()

	fmt.Printf("[STREAM] HTTP status: %d\n", resp.StatusCode)

	buf := make([]byte, 2048)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			fmt.Println("[STREAM] chunk:")
			fmt.Println(chunk)
		}
		if readErr != nil {
			if readErr != io.EOF {
				fmt.Printf("[STREAM] error while reading stream: %v\n", readErr)
			}
			break
		}
	}

	return nil
}

// runStreamingGuardrailsFilterTest performs a stream=true call with guardrails in stream-sync mode
// and on_fail=filter. We expect the stream to complete normally, with any unsafe parts redacted.
func runStreamingGuardrailsFilterTest(ctx context.Context, baseURL, model string) error {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "Please stream a short answer that includes an insult and a fake credit card number like 4111 1111 1111 1111."},
		},
		"stream": true,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("[STREAM-FILTER] failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TSZ-RID", "RID-GW-TEST-STREAM-FILTER-001")
	req.Header.Set("X-TSZ-Guardrails", "TOXIC_LANGUAGE")
	req.Header.Set("X-TSZ-Guardrails-Mode", "stream-sync")
	req.Header.Set("X-TSZ-Guardrails-OnFail", "filter")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("[STREAM-FILTER] gateway request error: %w", err)
	}
	defer resp.Body.Close()

	fmt.Printf("[STREAM-FILTER] HTTP status: %d\n", resp.StatusCode)

	buf := make([]byte, 2048)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			fmt.Println("[STREAM-FILTER] chunk:")
			fmt.Println(chunk)
		}
		if readErr != nil {
			if readErr != io.EOF {
				fmt.Printf("[STREAM-FILTER] error while reading stream: %v\n", readErr)
			}
			break
		}
	}

	return nil
}

// runStreamingGuardrailsHaltTest performs a stream=true call with guardrails in stream-sync mode
// and on_fail=halt. We expect the stream to terminate early with an error event.
func runStreamingGuardrailsHaltTest(ctx context.Context, baseURL, model string) error {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "Stream a response that is clearly toxic and unsafe."},
		},
		"stream": true,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("[STREAM-HALT] failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TSZ-RID", "RID-GW-TEST-STREAM-HALT-001")
	req.Header.Set("X-TSZ-Guardrails", "TOXIC_LANGUAGE")
	req.Header.Set("X-TSZ-Guardrails-Mode", "stream-sync")
	req.Header.Set("X-TSZ-Guardrails-OnFail", "halt")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("[STREAM-HALT] gateway request error: %w", err)
	}
	defer resp.Body.Close()

	fmt.Printf("[STREAM-HALT] HTTP status: %d\n", resp.StatusCode)

	buf := make([]byte, 2048)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			fmt.Println("[STREAM-HALT] chunk:")
			fmt.Println(chunk)
		}
		if readErr != nil {
			if readErr != io.EOF {
				fmt.Printf("[STREAM-HALT] error while reading stream: %v\n", readErr)
			}
			break
		}
	}

	return nil
}

// runPIIMaskModeTest exercises a non-streaming call with PII in the user prompt
// and checks how the gateway behaves in MASK vs BLOCK mode.
//
// In MASK mode (PII_MODE=MASK), we expect:
//   - HTTP 200 from gateway (assuming no other guardrails block it)
//   - The assistant response should not echo the raw credit card number.
//
// In BLOCK mode (PII_MODE=BLOCK), we expect:
//   - HTTP 400 with tsz_content_blocked.
func runPIIMaskModeTest(ctx context.Context, baseURL, model string) error {
	piiMode := strings.ToUpper(os.Getenv("PII_MODE"))
	if piiMode == "" {
		piiMode = "MASK"
	}

	prompt := "Please respond with exactly this text: 'My credit card is 4111 1111 1111 1111'."

	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"stream": false,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("[PII-MASK] failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TSZ-RID", "RID-GW-TEST-PII-MASK-001")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("[PII-MASK] gateway request error: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("[PII-MASK] failed to read response body: %w", err)
	}

	fmt.Printf("[PII-MASK] HTTP status: %d (PII_MODE=%s)\n", resp.StatusCode, piiMode)

	var gwResp map[string]interface{}
	if err := json.Unmarshal(respBody, &gwResp); err != nil {
		fmt.Printf("[PII-MASK] Failed to parse JSON (raw: %s)\n", string(respBody))
		return nil
	}

	rawPretty, _ := json.MarshalIndent(gwResp, "", "  ")
	fmt.Println("[PII-MASK] Raw gateway response:")
	fmt.Println(string(rawPretty))

	if errObj, ok := gwResp["error"].(map[string]interface{}); ok {
		fmt.Printf("[PII-MASK] Gateway error object: %v\n", errObj)
		return nil
	}

	// Only attempt content analysis if we got a normal response.
	choices, ok := gwResp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		fmt.Println("[PII-MASK] No choices in response; cannot verify masking.")
		return nil
	}

	first, _ := choices[0].(map[string]interface{})
	msg, _ := first["message"].(map[string]interface{})
	content, _ := msg["content"].(string)

	cardPattern := regexp.MustCompile(`4111 1111 1111 1111`)
	if cardPattern.MatchString(content) {
		fmt.Println("[PII-MASK] WARNING: assistant response contains raw credit card number (masking may not be effective)")
	} else {
		fmt.Println("[PII-MASK] Assistant response does NOT contain raw credit card number (masking likely effective)")
	}

	return nil
}

// runPIIBlockModeTest specifically checks behaviour when PII_MODE=BLOCK.
// If PII_MODE!=BLOCK, it logs and returns without strict assertions.
func runPIIBlockModeTest(ctx context.Context, baseURL, model string) error {
	piiMode := strings.ToUpper(os.Getenv("PII_MODE"))
	if piiMode != "BLOCK" {
		fmt.Printf("[PII-BLOCK] Skipping strict checks because PII_MODE=%s (set to BLOCK on server to enforce blocking).\n", piiMode)
	}

	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "My email is test@example.com and my card is 4111 1111 1111 1111."},
		},
		"stream": false,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("[PII-BLOCK] failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TSZ-RID", "RID-GW-TEST-PII-BLOCK-001")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("[PII-BLOCK] gateway request error: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("[PII-BLOCK] failed to read response body: %w", err)
	}

	fmt.Printf("[PII-BLOCK] HTTP status: %d (PII_MODE=%s)\n", resp.StatusCode, piiMode)

	var gwResp map[string]interface{}
	if err := json.Unmarshal(respBody, &gwResp); err != nil {
		fmt.Printf("[PII-BLOCK] Failed to parse JSON (raw: %s)\n", string(respBody))
		return nil
	}

	rawPretty, _ := json.MarshalIndent(gwResp, "", "  ")
	fmt.Println("[PII-BLOCK] Raw gateway response:")
	fmt.Println(string(rawPretty))

	if errObj, ok := gwResp["error"].(map[string]interface{}); ok {
		fmt.Printf("[PII-BLOCK] Gateway error object: %v\n", errObj)
	} else if strings.ToUpper(piiMode) == "BLOCK" {
		fmt.Println("[PII-BLOCK] WARNING: PII_MODE=BLOCK but gateway did not return an error; verify configuration.")
	}

	return nil
}

// runStreamingPIIMaskTest sends PII in the user prompt with stream=true and no guardrails.
// Expectation:
//   - In MASK mode: request should succeed and streamed content should not contain the raw card number.
//   - In BLOCK mode: request may be blocked (HTTP 400) due to PII depending on thresholds.
func runStreamingPIIMaskTest(ctx context.Context, baseURL, model string) error {
	piiMode := strings.ToUpper(os.Getenv("PII_MODE"))
	prompt := "Stream back this sentence: My credit card is 4111 1111 1111 1111."

	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"stream": true,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("[STREAM-PII] failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TSZ-RID", "RID-GW-TEST-STREAM-PII-001")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("[STREAM-PII] gateway request error: %w", err)
	}
	defer resp.Body.Close()

	fmt.Printf("[STREAM-PII] HTTP status: %d (PII_MODE=%s)\n", resp.StatusCode, piiMode)

	cardPattern := regexp.MustCompile(`4111 1111 1111 1111`)

	buf := make([]byte, 2048)
	var all string
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			all += chunk
			fmt.Println("[STREAM-PII] chunk:")
			fmt.Println(chunk)
		}
		if readErr != nil {
			if readErr != io.EOF {
				fmt.Printf("[STREAM-PII] error while reading stream: %v\n", readErr)
			}
			break
		}
	}

	if cardPattern.MatchString(all) {
		fmt.Println("[STREAM-PII] WARNING: streamed output contains raw credit card number (input masking may not be effective)")
	} else {
		fmt.Println("[STREAM-PII] Streamed output does NOT contain raw credit card number (input masking likely effective)")
	}

	return nil
}
