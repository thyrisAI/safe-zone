package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	tszclient "github.com/thyrisAI/safe-zone/pkg/tszclient-go"
)

// This example uses the Go client (pkg/tszclient-go) together with
// plain HTTP helpers to demonstrate several TSZ capabilities:
//
//  1. Core /detect API (PII detection & guardrails)
//  2. Allowlist management (/allowlist)
//  3. Blocklist (blacklist) management (/blacklist)
//  4. Listing validators and patterns (/validators, /patterns)
//  5. OpenAI-compatible LLM gateway (/v1/chat/completions)
//
// It is structured as if it were an external project that depends on
// the SDK via the GitHub module path:
//
//	import tszclient "github.com/thyrisAI/safe-zone/pkg/tszclient-go"
//
// How to run (from the repository root):
//
//	cd examples/go-sdk-demo
//	go run .
//
// Your TSZ gateway should be running on http://localhost:8080
// (e.g. via docker-compose or a local binary).
const tszBaseURL = "http://localhost:8080"

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := tszclient.New(tszclient.Config{
		BaseURL: tszBaseURL,
	})
	if err != nil {
		log.Fatalf("failed to create tsz client: %v", err)
	}

	log.Println("=== 1) Basic /detect demo ===")
	if err := runDetectDemo(ctx, client); err != nil {
		log.Printf("detect demo failed: %v", err)
	}

	log.Println("\n=== 2) Allowlist demo (/allowlist + /detect) ===")
	if err := runAllowlistDemo(ctx, client); err != nil {
		log.Printf("allowlist demo failed: %v", err)
	}

	log.Println("\n=== 3) Blocklist demo (/blacklist + /detect) ===")
	if err := runBlocklistDemo(ctx, client); err != nil {
		log.Printf("blocklist demo failed: %v", err)
	}

	log.Println("\n=== 4) Validators & patterns overview ===")
	if err := runValidatorsAndPatternsDemo(ctx); err != nil {
		log.Printf("validators/patterns demo failed: %v", err)
	}

	log.Println("\n=== 5) LLM gateway demo (/v1/chat/completions) ===")
	if err := runLLMDemo(ctx, client); err != nil {
		log.Printf("LLM demo failed: %v", err)
	}
}

// runDetectDemo shows a simple /detect call via the Go SDK.
func runDetectDemo(ctx context.Context, client *tszclient.Client) error {
	resp, err := client.DetectText(
		ctx,
		"Contact me at john@example.com regarding order #99281.",
		tszclient.WithRID("RID-GO-SDK-DEMO-DETECT-001"),
		tszclient.WithGuardrails("TOXIC_LANGUAGE"),
	)
	if err != nil {
		return fmt.Errorf("detect failed: %w", err)
	}

	if resp.Blocked {
		log.Printf("[DETECT] blocked by TSZ: %s", resp.Message)
	} else {
		log.Printf("[DETECT] redacted text: %s", resp.RedactedText)
		log.Printf("[DETECT] breakdown: %+v", resp.Breakdown)
	}

	return nil
}

// runAllowlistDemo creates an allowlist item, then calls /detect to
// show how a trusted value can be ignored by detection.
func runAllowlistDemo(ctx context.Context, client *tszclient.Client) error {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// 1) Create an allowlist item for a specific email.
	allowValue := "support@company.com"
	payload := map[string]any{
		"value":       allowValue,
		"description": "Support mailbox allowlisted from Go SDK demo",
	}

	var created map[string]any
	if err := postJSON(ctx, httpClient, tszBaseURL+"/allowlist", payload, &created); err != nil {
		return fmt.Errorf("failed to create allowlist item: %w", err)
	}
	log.Printf("[ALLOWLIST] created item: %+v", created)

	// 2) Call /detect with a text that includes the allowlisted value.
	text := fmt.Sprintf("You can contact support at %s for help.", allowValue)
	resp, err := client.DetectText(
		ctx,
		text,
		tszclient.WithRID("RID-GO-SDK-DEMO-ALLOWLIST-001"),
	)
	if err != nil {
		return fmt.Errorf("detect with allowlist text failed: %w", err)
	}

	log.Printf("[ALLOWLIST] input text: %q", text)
	log.Printf("[ALLOWLIST] redacted text: %s", resp.RedactedText)
	log.Printf("[ALLOWLIST] breakdown: %+v (allowlisted values may be ignored)", resp.Breakdown)

	return nil
}

// runBlocklistDemo creates a blocklist (blacklist) item and then shows
// how a forbidden value can cause a request to be blocked.
func runBlocklistDemo(ctx context.Context, client *tszclient.Client) error {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	blockValue := "internal_secret_token"
	payload := map[string]any{
		"value":       blockValue,
		"description": "Demo blocklist token from Go SDK demo",
	}

	var created map[string]any
	if err := postJSON(ctx, httpClient, tszBaseURL+"/blacklist", payload, &created); err != nil {
		return fmt.Errorf("failed to create blocklist item: %w", err)
	}
	log.Printf("[BLOCKLIST] created item: %+v", created)

	// Now trigger detection with the blocked value.
	text := fmt.Sprintf("This payload contains %s which should be blocked.", blockValue)
	resp, err := client.DetectText(
		ctx,
		text,
		tszclient.WithRID("RID-GO-SDK-DEMO-BLOCKLIST-001"),
	)
	if err != nil {
		return fmt.Errorf("detect with blocklist text failed: %w", err)
	}

	log.Printf("[BLOCKLIST] input text: %q", text)
	log.Printf("[BLOCKLIST] redacted text: %s", resp.RedactedText)
	log.Printf("[BLOCKLIST] breakdown: %+v", resp.Breakdown)
	log.Printf("[BLOCKLIST] blocked=%v message=%q", resp.Blocked, resp.Message)

	return nil
}

// runValidatorsAndPatternsDemo lists a few validators and patterns to
// show how additional guardrails can be configured.
func runValidatorsAndPatternsDemo(ctx context.Context) error {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// List validators
	var validators []map[string]any
	if err := getJSON(ctx, httpClient, tszBaseURL+"/validators", &validators); err != nil {
		return fmt.Errorf("failed to list validators: %w", err)
	}
	if len(validators) == 0 {
		log.Println("[VALIDATORS] no validators configured")
	} else {
		log.Printf("[VALIDATORS] first validator: %+v", validators[0])
	}

	// List patterns
	var patterns []map[string]any
	if err := getJSON(ctx, httpClient, tszBaseURL+"/patterns", &patterns); err != nil {
		return fmt.Errorf("failed to list patterns: %w", err)
	}
	if len(patterns) == 0 {
		log.Println("[PATTERNS] no patterns configured")
	} else {
		log.Printf("[PATTERNS] first pattern: %+v", patterns[0])
	}

	return nil
}

// runLLMDemo shows a non-streaming call to the OpenAI-compatible
// `/v1/chat/completions` gateway using the Go SDK.
func runLLMDemo(ctx context.Context, client *tszclient.Client) error {
	resp, err := client.ChatCompletions(
		ctx,
		tszclient.ChatCompletionRequest{
			Model: "llama3.1:8b", // Align with AI_MODEL in your .env
			Messages: []map[string]interface{}{
				{"role": "user", "content": "Hello from external Go SDK demo via TSZ gateway"},
			},
			Stream: false,
		},
		map[string]string{
			"X-TSZ-RID":        "RID-GO-SDK-DEMO-CHAT-001",
			"X-TSZ-Guardrails": "TOXIC_LANGUAGE",
		},
	)
	if err != nil {
		return fmt.Errorf("chat completions failed: %w", err)
	}

	choices, ok := resp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		log.Println("[CHAT] no choices in response")
		return nil
	}

	first, _ := choices[0].(map[string]interface{})
	msg, _ := first["message"].(map[string]interface{})
	content, _ := msg["content"].(string)

	fmt.Println("[CHAT] LLM response via TSZ:")
	fmt.Println(content)

	return nil
}

// postJSON is a small helper for posting JSON payloads and decoding
// the JSON response into `out`.
func postJSON(ctx context.Context, client *http.Client, url string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode response body: %w", err)
	}

	return nil
}

// getJSON is a small helper for GET requests that decode JSON into `out`.
func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode response body: %w", err)
	}

	return nil
}
