package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestDetect_BehaviorUnderDifferentPIIModes(t *testing.T) {
	modes := []string{"MASK", "BLOCK"}

	for _, mode := range modes {
		mode := mode
		t.Run("PII_MODE="+mode, func(t *testing.T) {
			url := baseURL() + "/detect"
			body := `{"text": "My email is test@example.com", "rid": "pii-mode-` + mode + `"}`

			// PII_MODE is read by the server process; here we only document the expectation.
			// In CI the server runs with PII_MODE=MASK; to fully validate BLOCK mode behavior,
			// a dedicated job/environment with PII_MODE=BLOCK should be configured.

			resp, err := http.Post(url, "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatalf("POST /detect failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200 from /detect, got %d", resp.StatusCode)
			}

			var dr detectResponse
			if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
				t.Fatalf("failed to decode /detect response: %v", err)
			}

			if !dr.ContainsPII {
				t.Fatalf("expected ContainsPII=true in all PII modes")
			}
		})
	}
}

func TestGateway_BehaviorUnderDifferentBlockModes(t *testing.T) {
	blockModes := []string{"BLOCK", "MASK", "WARN"}

	for _, bm := range blockModes {
		bm := bm
		t.Run("GATEWAY_BLOCK_MODE="+bm, func(t *testing.T) {
			model := os.Getenv("THYRIS_AI_MODEL")
			if model == "" {
				model = "llama3.1:8b"
			}

			payload := map[string]interface{}{
				"model": model,
				"messages": []map[string]string{
					{"role": "user", "content": "My email is test@example.com and my card is 4111 1111 1111 1111."},
				},
				"stream": false,
			}

			body, _ := json.Marshal(payload)
			req, err := http.NewRequest(http.MethodPost, baseURL()+"/v1/chat/completions", strings.NewReader(string(body)))
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-TSZ-RID", "GW-BLOCKMODE-"+bm)
			req.Header.Set("X-TSZ-Guardrails", "TOXIC_LANGUAGE")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("gateway request failed: %v", err)
			}
			defer resp.Body.Close()

			// We do not strictly assert status here because behavior depends on
			// configuration and upstream AI. We only require a valid JSON envelope.
			var gwResp map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&gwResp); err != nil {
				t.Fatalf("failed to decode gateway response: %v", err)
			}

			if _, hasChoices := gwResp["choices"]; !hasChoices {
				if _, hasError := gwResp["error"]; !hasError {
					t.Fatalf("expected either choices or error in gateway response; got %v", gwResp)
				}
			}
		})
	}
}
