package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type piiMatrixCase struct {
	Name              string   `json:"name"`
	Text              string   `json:"text"`
	ExpectContainsPII bool     `json:"expect_contains_pii"`
	ExpectedTypes     []string `json:"expected_types"`
}

func loadPIIMatrixCases(t *testing.T) []piiMatrixCase {
	t.Helper()

	filePath := filepath.Join("..", "data", "pii_cases.json")
	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("failed to open PII cases file: %v", err)
	}
	defer f.Close()

	var cases []piiMatrixCase
	if err := json.NewDecoder(f).Decode(&cases); err != nil {
		t.Fatalf("failed to decode PII cases JSON: %v", err)
	}

	return cases
}

func TestDetect_PIIMatrixFromFixtures(t *testing.T) {
	cases := loadPIIMatrixCases(t)

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			url := baseURL() + "/detect"
			body := `{"text": "` + strings.ReplaceAll(c.Text, "\"", "\\\"") + `", "rid": "pii-matrix-` + c.Name + `"}`

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

			if dr.ContainsPII != c.ExpectContainsPII {
				t.Fatalf("expected ContainsPII=%v, got %v", c.ExpectContainsPII, dr.ContainsPII)
			}

			if len(c.ExpectedTypes) == 0 {
				return
			}

			// Build a set of detected types
			types := make(map[string]bool)
			for _, d := range dr.Detections {
				types[d.Type] = true
			}

			for _, et := range c.ExpectedTypes {
				if !types[et] {
					t.Fatalf("expected detection type %s to be present; got %+v", et, types)
				}
			}
		})
	}
}
