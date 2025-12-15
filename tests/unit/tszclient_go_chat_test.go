package unit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tszclient "github.com/thyrisAI/safe-zone/pkg/tszclient-go"
)

// TestChatCompletions_Success exercises the tszclient-go ChatCompletions helper
// against a fake OpenAI-compatible endpoint and verifies that the returned
// ChatCompletionResponse map contains a well-formed `choices` slice.
func TestChatCompletions_Success(t *testing.T) {
	// Arrange: fake TSZ gateway that exposes /v1/chat/completions
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if gotModel, _ := body["model"].(string); gotModel != "test-model" {
			t.Fatalf("unexpected model in request: %q", gotModel)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "chatcmpl-test",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "hello from test",
					},
				},
			},
		}); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := tszclient.New(tszclient.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.ChatCompletions(
		ctx,
		tszclient.ChatCompletionRequest{
			Model: "test-model",
			Messages: []map[string]interface{}{
				{"role": "user", "content": "hi"},
			},
			Stream: false,
		},
		map[string]string{
			"X-TSZ-RID":        "RID-TEST-001",
			"X-TSZ-Guardrails": "TOXIC_LANGUAGE",
		},
	)
	if err != nil {
		t.Fatalf("ChatCompletions returned error: %v", err)
	}

	// Assert: response has a choices slice with the expected content.
	rawChoices, ok := resp["choices"].([]interface{})
	if !ok {
		t.Fatalf("choices not present or wrong type: %T", resp["choices"])
	}
	if len(rawChoices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(rawChoices))
	}

	first, ok := rawChoices[0].(map[string]interface{})
	if !ok {
		t.Fatalf("first choice has wrong type: %T", rawChoices[0])
	}

	msg, ok := first["message"].(map[string]interface{})
	if !ok {
		t.Fatalf("message field has wrong type: %T", first["message"])
	}

	content, _ := msg["content"].(string)
	if content != "hello from test" {
		t.Fatalf("unexpected content: %q", content)
	}
}
