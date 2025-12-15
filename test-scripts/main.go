package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	defaultBaseURL = "http://localhost:8080"
	colorReset     = "\033[0m"
	colorRed       = "\033[31m"
	colorGreen     = "\033[32m"
	colorYellow    = "\033[33m"
	colorCyan      = "\033[36m"
)

var baseURL = defaultBaseURL

// NOTE: This script is intended as an end-to-end sanity suite that touches
// most TSZ features: patterns, allowlist, validators, templates, admin, detect,
// confidence scoring and the OpenAI-compatible LLM gateway.
//
// Usage:
//  1. Run TSZ locally (docker-compose up or go run ./cmd/tsz-gateway).
//  2. Optionally set BASE_URL env var if TSZ is not on http://localhost:8080.
//  3. go run ./test-scripts
func main() {
	if env := os.Getenv("BASE_URL"); env != "" {
		// Allow overriding base URL via env for tests in different environments
		fmt.Printf("Using BASE_URL=%s (was %s)\n", env, baseURL)
		baseURL = env
	}

	printHeader("THYRIS-SZ ADVANCED TEST SUITE")

	// 1. System Health Check
	runSuite("System Health", func() {
		assertGET("/healthz", 200)
		assertGET("/ready", 200)
	})

	// 2. Pattern Management
	runSuite("Pattern Management", func() {
		name := fmt.Sprintf("TEST_PAT_%d", rand.Intn(9999))
		id := createPattern(name, "TEST\\d+", "Temporary Test Pattern")

		// Verify creation
		patterns := listPatterns()
		found := false
		for _, p := range patterns {
			if p["Name"] == name {
				found = true
				break
			}
		}
		assertTrue(found, "Created pattern should appear in list")

		// Verify deletion
		deletePattern(id)

		// Verify deletion in list (Wait for cache invalidation if async, but it's sync in code)
		patterns = listPatterns()
		found = false
		for _, p := range patterns {
			if p["Name"] == name {
				found = true
				break
			}
		}
		assertFalse(found, "Deleted pattern should NOT appear in list")
	})

	// 3. Allowlist Management
	runSuite("Allowlist Management", func() {
		val := fmt.Sprintf("allow_%d@test.com", rand.Intn(9999))
		id := createAllowlist(val, "Temporary Allow Item")

		// Verify creation
		items := listAllowlist()
		found := false
		for _, i := range items {
			if i["value"] == val {
				found = true
				break
			}
		}
		assertTrue(found, "Created allowlist item should appear in list")

		deleteAllowlist(id)
	})

	// 4. Validator Management (New Features)
	runSuite("Validator Management", func() {
		// Test Schema Validator Creation
		schemaName := fmt.Sprintf("TEST_SCHEMA_%d", rand.Intn(9999))
		schemaRule := `{"type": "object", "properties": {"name": {"type": "string"}}}`
		id := createValidator(schemaName, "SCHEMA", schemaRule, "Test Schema Validator")

		// Verify creation
		validators := listValidators()
		found := false
		for _, v := range validators {
			if v["name"] == schemaName {
				found = true
				break
			}
		}
		assertTrue(found, "Created schema validator should appear in list")
		deleteValidator(id)

		// Test AI Validator Creation
		aiName := fmt.Sprintf("TEST_AI_%d", rand.Intn(9999))
		aiRule := "Is this text toxic?"
		id2 := createValidator(aiName, "AI_PROMPT", aiRule, "Test AI Validator")
		assertTrue(id2 > 0, "Should create AI validator")
		deleteValidator(id2)
	})

	// 5. Template Management
	runSuite("Template Management", func() {
		templateName := fmt.Sprintf("TEST_TEMPLATE_%d", rand.Intn(9999))
		// Construct a simple template payload
		// Structure matches ImportTemplateRequest -> Template -> Patterns/Validators
		// We need to use a map structure that matches the JSON expected by the handler

		// Using a simplified structure for the test
		payload := map[string]interface{}{
			"template": map[string]interface{}{
				"name": templateName,
				"patterns": []map[string]interface{}{
					{
						"name":        fmt.Sprintf("TPL_PAT_%d", rand.Intn(9999)),
						"regex":       "TPL\\d+",
						"description": "Template Pattern",
						"category":    "TEST",
						"is_active":   true,
					},
				},
				"validators": []map[string]interface{}{
					{
						"name":        fmt.Sprintf("TPL_VAL_%d", rand.Intn(9999)),
						"type":        "SCHEMA",
						"rule":        "{}",
						"description": "Template Validator",
					},
				},
			},
		}

		statusCode := importTemplate(payload)
		assertTrue(statusCode == 200, "Template import should return 200 OK")
	})

	// 6. Admin Operations
	runSuite("Admin Operations", func() {
		statusCode := reloadCache()
		assertTrue(statusCode == 200, "Admin reload should return 200 OK")
	})

	// 7. Detection Logic (End-to-End)
	runSuite("Detection Logic", func() {
		// Basic Detection
		resp := detect("My email is test@example.com")
		assertTrue(resp.ContainsPII, "Should detect PII")
		assertTrue(len(resp.Detections) > 0, "Detections list should not be empty")

		// No PII
		resp = detect("Hello world, no pii here.")
		assertFalse(resp.ContainsPII, "Should NOT detect PII")
		assertTrue(len(resp.Detections) == 0, "Detections list should be empty")
	})

	// 8. Advanced AI Guardrails (Dynamic Expected Response)
	runSuite("Advanced AI Guardrails", func() {
		// Create validator expecting "1" for Safe
		aiName := fmt.Sprintf("TEST_SAFE_%d", rand.Intn(9999))
		aiRule := "Is this text safe? Respond 1 for safe, 0 for unsafe."
		id := createValidatorWithResponse(aiName, "AI_PROMPT", aiRule, "Test Safe Validator", "1")

		// Test Safe Content
		respSafe := detect("Hello world", aiName)
		assertFalse(respSafe.Blocked, "Safe content should NOT be blocked")

		// Test Unsafe Content (Toxic)
		respUnsafe := detect("You are stupid and useless!", aiName)
		// Note: Whether it is blocked depends on the AI model's decision.
		// Llama 3.1 usually blocks insults if prompted about safety.
		// We log the result for manual verification if test fails.
		if respUnsafe.Blocked {
			passed("Unsafe content was blocked")
		} else {
			// Don't fail the suite if AI model is lenient, but print warning
			fmt.Printf("  %s!%s Unsafe content was NOT blocked (AI specific)\n", colorYellow, colorReset)
		}

		deleteValidator(id)
	})

	// 9. Confidence & Explainability
	runSuite("Confidence & Explainability", func() {
		resp := detect("My email is test@example.com")
		// At least one detection should have confidence fields populated
		if len(resp.Detections) == 0 {
			failed("Expected at least one detection for confidence test")
			return
		}

		first := resp.Detections[0]
		assertTrue(first.ConfidenceScore != "" && first.ConfidenceScore != "0.00", "Detection should have non-empty confidence_score")
		if first.ConfidenceExplanation != nil {
			assertTrue(first.ConfidenceExplanation.FinalScore != "", "Confidence explanation should contain final_score")
		} else {
			fmt.Printf("  %s!%s Confidence explanation is nil (check configuration)\n", colorYellow, colorReset)
		}

		assertTrue(resp.OverallConfidence != "", "Overall confidence should be present")
	})

	// 10. OpenAI-Compatible Gateway (/v1/chat/completions)
	runSuite("LLM Gateway (OpenAI-Compatible)", func() {
		// Safe request through gateway
		status, body := callGatewayChatCompletion("Hello from TSZ tests", "")
		assertTrue(status == 200, "Gateway should return 200 for safe content (if upstream configured)")
		if status == 200 {
			// Basic shape check: there should be a choices array if upstream responded correctly
			var gwResp map[string]interface{}
			if err := json.Unmarshal(body, &gwResp); err == nil {
				if choices, ok := gwResp["choices"].([]interface{}); ok && len(choices) > 0 {
					passed("Gateway returned choices array")
				} else if _, hasErr := gwResp["error"]; hasErr {
					fmt.Printf("  %s!%s Gateway responded with error object (check upstream): %v\n", colorYellow, colorReset, gwResp["error"])
				} else {
					fmt.Printf("  %s!%s Gateway response missing choices/error (raw: %s)\n", colorYellow, colorReset, string(body))
				}
			} else {
				fmt.Printf("  %s!%s Failed to parse gateway JSON: %v\n", colorYellow, colorReset, err)
			}
		}

		// Risky content with guardrails header set - may be blocked
		status, body = callGatewayChatCompletion("My credit card is 4111 1111 1111 1111, you are an idiot", "TOXIC_LANGUAGE")
		if status == 400 {
			// Expect an OpenAI-style error payload
			var errResp map[string]interface{}
			if err := json.Unmarshal(body, &errResp); err == nil {
				if errObj, ok := errResp["error"].(map[string]interface{}); ok {
					fmt.Printf("  Gateway blocked unsafe content: %v\n", errObj["message"])
					passed("Gateway correctly blocked unsafe content")
				} else {
					failed("Gateway 400 response missing error object")
				}
			} else {
				failed("Gateway 400 response not valid JSON")
			}
		} else if status == 200 {
			fmt.Printf("  %s!%s Unsafe content was not blocked by gateway (model/threshold specific)\n", colorYellow, colorReset)
		}
	})

	// 11. Performance / Load Test
	runSuite("Performance Load Test", func() {
		runLoadTest(50, 10*time.Second)
	})

	printHeader("ALL TESTS COMPLETED")
}

// --- Helper Functions ---

func runSuite(name string, tests func()) {
	fmt.Printf("\n%s=== %s ===%s\n", colorCyan, name, colorReset)
	start := time.Now()
	tests()
	fmt.Printf("Suite completed in %v\n", time.Since(start))
}

func printHeader(title string) {
	fmt.Println(colorYellow + "==========================================" + colorReset)
	fmt.Printf("   %s\n", title)
	fmt.Println(colorYellow + "==========================================" + colorReset)
}

func passed(msg string) {
	fmt.Printf("  %s✓%s %s\n", colorGreen, colorReset, msg)
}

func failed(msg string) {
	fmt.Printf("  %s✗%s %s\n", colorRed, colorReset, msg)
	// panic("Test failed") // Optional: Stop on failure
}

func assertTrue(condition bool, msg string) {
	if condition {
		passed(msg)
	} else {
		failed(msg)
	}
}

func assertFalse(condition bool, msg string) {
	assertTrue(!condition, msg)
}

func assertGET(endpoint string, expectedStatus int) {
	resp, err := http.Get(baseURL + endpoint)
	if err != nil {
		failed(fmt.Sprintf("GET %s failed: %v", endpoint, err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == expectedStatus {
		passed(fmt.Sprintf("GET %s returned %d", endpoint, expectedStatus))
	} else {
		failed(fmt.Sprintf("GET %s returned %d (expected %d)", endpoint, resp.StatusCode, expectedStatus))
	}
}

// --- API Wrappers ---

func createPattern(name, regex, desc string) float64 {
	payload := map[string]string{"name": name, "regex": regex, "description": desc}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/patterns", "application/json", bytes.NewBuffer(body))
	if err != nil || resp.StatusCode != 201 {
		failed("Failed to create pattern")
		return 0
	}
	var res map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&res)
	resp.Body.Close()
	passed("Pattern created: " + name)
	return res["ID"].(float64)
}

func listPatterns() []map[string]interface{} {
	resp, err := http.Get(baseURL + "/patterns")
	if err != nil {
		failed(fmt.Sprintf("GET /patterns failed: %v", err))
		return nil
	}
	defer resp.Body.Close()

	var res []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		failed(fmt.Sprintf("Failed to decode /patterns response: %v", err))
	}
	return res
}

func deletePattern(id float64) {
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/patterns/%.0f", baseURL, id), nil)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode == 204 {
		passed("Pattern deleted")
	} else {
		failed("Failed to delete pattern")
	}
}

func createAllowlist(val, desc string) float64 {
	payload := map[string]string{"value": val, "description": desc}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/allowlist", "application/json", bytes.NewBuffer(body))
	if err != nil || resp.StatusCode != 201 {
		failed("Failed to create allowlist item")
		return 0
	}
	var res map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&res)
	resp.Body.Close()
	passed("Allowlist item created: " + val)
	return res["ID"].(float64)
}

func listAllowlist() []map[string]interface{} {
	resp, err := http.Get(baseURL + "/allowlist")
	if err != nil {
		failed(fmt.Sprintf("GET /allowlist failed: %v", err))
		return nil
	}
	defer resp.Body.Close()

	var res []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		failed(fmt.Sprintf("Failed to decode /allowlist response: %v", err))
	}
	return res
}

func deleteAllowlist(id float64) {
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/allowlist/%.0f", baseURL, id), nil)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode == 204 {
		passed("Allowlist item deleted")
	} else {
		failed("Failed to delete allowlist item")
	}
}

// --- Validator Helpers ---

func createValidator(name, vType, rule, desc string) float64 {
	return createValidatorWithResponse(name, vType, rule, desc, "YES")
}

func createValidatorWithResponse(name, vType, rule, desc, expected string) float64 {
	payload := map[string]interface{}{"name": name, "type": vType, "rule": rule, "description": desc, "expected_response": expected}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/validators", "application/json", bytes.NewBuffer(body))
	if err != nil || resp.StatusCode != 201 {
		failed("Failed to create validator: " + name)
		return 0
	}
	var res map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&res)
	resp.Body.Close()
	passed("Validator created: " + name)
	return res["ID"].(float64)
}

func listValidators() []map[string]interface{} {
	resp, err := http.Get(baseURL + "/validators")
	if err != nil {
		failed(fmt.Sprintf("GET /validators failed: %v", err))
		return nil
	}
	defer resp.Body.Close()

	var res []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		failed(fmt.Sprintf("Failed to decode /validators response: %v", err))
	}
	return res
}

func deleteValidator(id float64) {
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/validators/%.0f", baseURL, id), nil)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode == 204 {
		passed("Validator deleted")
	} else {
		failed("Failed to delete validator")
	}
}

// --- Template & Admin Wrappers ---

func importTemplate(payload map[string]interface{}) int {
	body, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/templates/import", "application/json", bytes.NewBuffer(body))
	if err != nil {
		failed("Failed to import template")
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		passed("Template imported successfully")
	} else {
		failed(fmt.Sprintf("Template import failed with status: %d", resp.StatusCode))
	}
	return resp.StatusCode
}

func reloadCache() int {
	req, _ := http.NewRequest("POST", baseURL+"/admin/reload", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		failed("Failed to reload cache")
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		passed("Cache reload triggered successfully")
	} else {
		failed(fmt.Sprintf("Cache reload failed with status: %d", resp.StatusCode))
	}
	return resp.StatusCode
}

type ConfidenceExplanation struct {
	Source        string `json:"source"`
	RegexScore    string `json:"regex_score"`
	AIScore       string `json:"ai_score"`
	Category      string `json:"category"`
	PatternActive bool   `json:"pattern_active"`
	FinalScore    string `json:"final_score"`
}

type Detection struct {
	Type                  string                 `json:"type"`
	Value                 string                 `json:"value"`
	Placeholder           string                 `json:"placeholder"`
	Start                 int                    `json:"start"`
	End                   int                    `json:"end"`
	ConfidenceScore       string                 `json:"confidence_score"`
	ConfidenceExplanation *ConfidenceExplanation `json:"confidence_explanation"`
}

type ValidatorResult struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Passed          bool   `json:"passed"`
	ConfidenceScore string `json:"confidence_score"`
}

type DetectResponse struct {
	RedactedText      string            `json:"redacted_text"`
	Detections        []Detection       `json:"detections"`
	ValidatorResults  []ValidatorResult `json:"validator_results"`
	Breakdown         map[string]int    `json:"breakdown"`
	Blocked           bool              `json:"blocked"`
	ContainsPII       bool              `json:"contains_pii"`
	OverallConfidence string            `json:"overall_confidence"`
	Message           string            `json:"message"`
}

func detect(text string, guardrails ...string) DetectResponse {
	payload := map[string]interface{}{"text": text, "rid": "test-suite"}
	if len(guardrails) > 0 {
		payload["guardrails"] = guardrails
	}
	body, _ := json.Marshal(payload)
	resp, _ := http.Post(baseURL+"/detect", "application/json", bytes.NewBuffer(body))
	var res DetectResponse
	json.NewDecoder(resp.Body).Decode(&res)
	resp.Body.Close()
	return res
}

func runLoadTest(concurrency int, duration time.Duration) {
	fmt.Printf("  Starting Load Test (%d workers, %v)...\n", concurrency, duration)

	payloads := []string{
		"Short text.",
		"Email: test@example.com",
		"Long text with TCKN 12345678901 and phone 05321234567.",
	}

	var wg sync.WaitGroup
	start := time.Now()
	count := 0
	var mu sync.Mutex

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wStart := time.Now()
			for time.Since(wStart) < duration {
				text := payloads[rand.Intn(len(payloads))]
				// Fire and forget mostly, just check error
				payload := map[string]string{"text": text, "rid": "load-test"}
				body, _ := json.Marshal(payload)
				resp, err := http.Post(baseURL+"/detect", "application/json", bytes.NewBuffer(body))
				if err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					mu.Lock()
					count++
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)
	if count == 0 {
		failed("Load test sent 0 successful requests")
		return
	}

	rps := float64(count) / elapsed.Seconds()

	fmt.Printf("  %sLoad Test Results:%s\n", colorGreen, colorReset)
	fmt.Printf("    Total Requests: %d\n", count)
	fmt.Printf("    RPS: %.2f req/sec\n", rps)
	fmt.Printf("    Avg Latency: %v\n", elapsed/time.Duration(count))
}

// --- LLM Gateway Helpers ---

func callGatewayChatCompletion(prompt, guardrails string) (int, []byte) {
	payload := map[string]interface{}{
		"model": "llama3.1:8b", // forwarded as-is; actual model comes from env in TSZ
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"stream": false,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TSZ-RID", "GW-TEST-1")
	if guardrails != "" {
		req.Header.Set("X-TSZ-Guardrails", guardrails)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		failed(fmt.Sprintf("Gateway request failed: %v", err))
		return 0, nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		failed(fmt.Sprintf("Failed to read gateway response: %v", err))
		return resp.StatusCode, nil
	}

	return resp.StatusCode, respBody
}
