package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type detectGoldenCase struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type detectGoldenExpect struct {
	RedactedText string   `json:"redacted_text"`
	Types        []string `json:"types"`
}

func loadDetectGoldenInput(t *testing.T, name string) detectGoldenCase {
	t.Helper()

	path := filepath.Join("..", "data", name+"_input.json")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open golden input %s: %v", path, err)
	}
	defer f.Close()

	var c detectGoldenCase
	if err := json.NewDecoder(f).Decode(&c); err != nil {
		t.Fatalf("failed to decode golden input %s: %v", path, err)
	}
	return c
}

func loadDetectGoldenExpect(t *testing.T, name string) detectGoldenExpect {
	t.Helper()

	path := filepath.Join("..", "data", name+"_expect.json")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open golden expect %s: %v", path, err)
	}
	defer f.Close()

	var e detectGoldenExpect
	if err := json.NewDecoder(f).Decode(&e); err != nil {
		t.Fatalf("failed to decode golden expect %s: %v", path, err)
	}
	return e
}

func TestDetect_Golden_EmailAndSSN(t *testing.T) {
	name := "detect_email_ssn"
	input := loadDetectGoldenInput(t, name)
	expect := loadDetectGoldenExpect(t, name)

	url := baseURL() + "/detect"
	body := `{"text": "` + strings.ReplaceAll(input.Text, "\"", "\\\"") + `", "rid": "` + name + `"}`

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

	gotTypes := make(map[string]bool)
	for _, d := range dr.Detections {
		gotTypes[d.Type] = true
	}

	for _, et := range expect.Types {
		if !gotTypes[et] {
			t.Fatalf("expected detection type %s to be present; got %+v", et, gotTypes)
		}
	}
}
