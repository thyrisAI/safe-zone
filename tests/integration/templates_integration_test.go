package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

func templateBaseURL() string {
	if v := os.Getenv("TSZ_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func TestTemplates_ImportAndDetectFlow(t *testing.T) {
	// 1) Import a simple template with one pattern and one validator
	templatePayload := map[string]interface{}{
		"template": map[string]interface{}{
			"name": "TEST_TEMPLATE_INTEGRATION",
			"patterns": []map[string]interface{}{
				{
					"name":        "TEST_PATTERN_CODE",
					"regex":       "CODE-[0-9]{4}",
					"description": "Test code pattern",
					"category":    "PII",
					"is_active":   true,
				},
			},
			"validators": []map[string]interface{}{
				{
					"name":        "JSON_PERSON",
					"type":        "SCHEMA",
					"rule":        "",
					"description": "Person schema (predefined)",
				},
			},
		},
	}

	body, _ := json.Marshal(templatePayload)
	resp, err := http.Post(templateBaseURL()+"/templates/import", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /templates/import failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /templates/import, got %d", resp.StatusCode)
	}

	// 2) Call /detect with text that should match the new pattern
	detectBody := `{"text": "Order CODE-1234 should be processed", "rid": "tmpl-it-1"}`
	resp2, err := http.Post(templateBaseURL()+"/detect", "application/json", bytes.NewReader([]byte(detectBody)))
	if err != nil {
		t.Fatalf("POST /detect failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /detect, got %d", resp2.StatusCode)
	}
}
