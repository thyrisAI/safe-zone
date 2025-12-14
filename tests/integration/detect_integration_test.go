package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

type detectResponse struct {
	RedactedText      string            `json:"redacted_text"`
	Detections        []detectionResult `json:"detections"`
	Blocked           bool              `json:"blocked"`
	ContainsPII       bool              `json:"contains_pii"`
	OverallConfidence string            `json:"overall_confidence"`
}

type detectionResult struct {
	Type            string `json:"type"`
	Value           string `json:"value"`
	Placeholder     string `json:"placeholder"`
	ConfidenceScore string `json:"confidence_score"`
}

func baseURL() string {
	if v := os.Getenv("TSZ_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func TestDetect_PIIDetection_Email(t *testing.T) {
	t.Parallel()

	url := baseURL() + "/detect"
	body := `{"text": "My email is test@example.com", "rid": "it-email-1"}`

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
		t.Fatalf("expected ContainsPII=true for email payload")
	}
	if dr.RedactedText == "" || dr.RedactedText == "My email is test@example.com" {
		t.Fatalf("expected redacted_text to be populated and different from original")
	}

	if len(dr.Detections) == 0 {
		t.Fatalf("expected at least one detection")
	}
}

func TestDetect_NonPII_AllowsAndNoDetections(t *testing.T) {
	t.Parallel()

	url := baseURL() + "/detect"
	body := `{"text": "Hello world, nothing sensitive here", "rid": "it-nonpii-1"}`

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

	if dr.ContainsPII {
		t.Fatalf("expected ContainsPII=false for safe payload")
	}
	if len(dr.Detections) != 0 {
		t.Fatalf("expected no detections for safe payload, got %d", len(dr.Detections))
	}
}

func TestDetect_InvalidJSON_BadRequest(t *testing.T) {
	t.Parallel()

	url := baseURL() + "/detect"
	body := `{"text": "missing quote}`

	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /detect failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 400/422 for invalid JSON, got %d", resp.StatusCode)
	}
}
