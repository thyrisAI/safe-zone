package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

type healthResponse struct {
	Status string `json:"status"`
}

type detectResponse struct {
	ContainsPII bool `json:"contains_pii"`
}

func baseURL() string {
	if v := os.Getenv("TSZ_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func TestSanity_HealthAndReady(t *testing.T) {
	t.Run("healthz", func(t *testing.T) {
		resp, err := http.Get(baseURL() + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from /healthz, got %d", resp.StatusCode)
		}
	})

	t.Run("ready", func(t *testing.T) {
		resp, err := http.Get(baseURL() + "/ready")
		if err != nil {
			t.Fatalf("GET /ready failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from /ready, got %d", resp.StatusCode)
		}
	})
}

func TestSanity_PatternsAllowlistValidatorsRoundtrip(t *testing.T) {
	// This is a thin wrapper that relies on existing REST handlers;
	// it is intentionally high-level and focuses on "does the round-trip work".

	// 1) List patterns
	resp, err := http.Get(baseURL() + "/patterns")
	if err != nil {
		t.Fatalf("GET /patterns failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /patterns, got %d", resp.StatusCode)
	}

	// 2) List validators
	resp2, err := http.Get(baseURL() + "/validators")
	if err != nil {
		t.Fatalf("GET /validators failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /validators, got %d", resp2.StatusCode)
	}

	// 3) List allowlist
	resp3, err := http.Get(baseURL() + "/allowlist")
	if err != nil {
		t.Fatalf("GET /allowlist failed: %v", err)
	}
	defer resp3.Body.Close()

	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /allowlist, got %d", resp3.StatusCode)
	}
}

func TestSanity_DetectAndGatewayBasicFlow(t *testing.T) {
	t.Run("detect-email", func(t *testing.T) {
		url := baseURL() + "/detect"
		body := `{"text": "My email is test@example.com", "rid": "e2e-sanity-detect"}`

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
	})

	t.Run("gateway-safe", func(t *testing.T) {
		payload := map[string]interface{}{
			"model": "gpt-4o",
			"messages": []map[string]string{
				{"role": "user", "content": "Hello from E2E sanity"},
			},
			"stream": false,
		}

		body, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPost, baseURL()+"/v1/chat/completions", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-TSZ-RID", "E2E-SANITY-GW")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("gateway request failed: %v", err)
		}
		defer resp.Body.Close()

		// Upstream may not be configured; we only assert that gateway responds with a JSON payload.
		var gwResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&gwResp); err != nil {
			t.Fatalf("failed to decode gateway response: %v", err)
		}
	})
}
