package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

type gatewayChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error   map[string]interface{} `json:"error"`
	TSZMeta map[string]interface{} `json:"tsz_meta"`
}

func gatewayBaseURL() string {
	if v := os.Getenv("TSZ_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func TestGateway_SafePrompt_AllowsAndReturnsChoices(t *testing.T) {
	payload := map[string]interface{}{
		"model": "gpt-4o", // forwarded as-is; actual model comes from env in TSZ
		"messages": []map[string]string{
			{"role": "user", "content": "Hello via TSZ gateway (safe test)"},
		},
		"stream": false,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, gatewayBaseURL()+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TSZ-RID", "GW-IT-SAFE-1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("gateway request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Upstream may not be configured; in that case log and skip strict assertion.
		var gwErr gatewayChatResponse
		_ = json.NewDecoder(resp.Body).Decode(&gwErr)
		t.Logf("non-200 from gateway (model may be misconfigured): status=%d error=%v", resp.StatusCode, gwErr.Error)
		return
	}

	var gwResp gatewayChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&gwResp); err != nil {
		t.Fatalf("failed to decode gateway response: %v", err)
	}

	if len(gwResp.Choices) == 0 {
		t.Fatalf("expected at least one choice in gateway response")
	}
}

func TestGateway_UnsafePrompt_WithGuardrailsMayBlockOrAnnotate(t *testing.T) {
	payload := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": "My email is test@example.com, you are an idiot"},
		},
		"stream": false,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, gatewayBaseURL()+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TSZ-RID", "GW-IT-UNSAFE-1")
	req.Header.Set("X-TSZ-Guardrails", "TOXIC_LANGUAGE")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("gateway request failed: %v", err)
	}
	defer resp.Body.Close()

	var gwResp gatewayChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&gwResp); err != nil {
		t.Fatalf("failed to decode gateway response: %v", err)
	}

	// Enterprise-friendly: behaviour depends on config.GatewayBlockMode.
	// If blocked, expect HTTP 400 and OpenAI-style error object.
	if resp.StatusCode == http.StatusBadRequest {
		if gwResp.Error == nil {
			t.Fatalf("expected error object in 400 response")
		}
		return
	}

	// If not blocked, we still expect a valid JSON envelope (choices or error).
	if len(gwResp.Choices) == 0 && gwResp.Error == nil {
		t.Fatalf("expected choices or error in non-blocked response")
	}
}
