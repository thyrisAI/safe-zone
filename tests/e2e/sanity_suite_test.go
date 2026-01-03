package e2e

import (
	"context"
	"os"
	"testing"

	tszclient "github.com/thyrisAI/safe-zone/pkg/tszclient-go"
)

func baseURL() string {
	if v := os.Getenv("TSZ_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func getClient(t *testing.T) *tszclient.Client {
	client, err := tszclient.New(tszclient.Config{
		BaseURL: baseURL(),
		APIKey:  "test-admin-key",
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return client
}

func TestSanity_HealthAndReady(t *testing.T) {
	client := getClient(t)
	ctx := context.Background()

	t.Run("healthz", func(t *testing.T) {
		up, err := client.Health(ctx)
		if err != nil {
			t.Fatalf("Health check failed: %v", err)
		}
		if !up {
			t.Fatal("Service is not UP")
		}
	})

	t.Run("ready", func(t *testing.T) {
		ready, err := client.Ready(ctx)
		if err != nil {
			t.Fatalf("Readiness check failed: %v", err)
		}
		if !ready {
			t.Fatal("Service is not READY")
		}
	})
}

func TestSanity_PatternsAllowlistValidatorsRoundtrip(t *testing.T) {
	client := getClient(t)
	ctx := context.Background()

	// 1) List patterns
	patterns, err := client.ListPatterns(ctx)
	if err != nil {
		t.Fatalf("ListPatterns failed: %v", err)
	}
	// We expect at least default patterns (e.g. EMAIL)
	if len(patterns) == 0 {
		t.Log("Warning: No patterns found (DB might be empty)")
	}

	// 2) List validators
	validators, err := client.ListValidators(ctx)
	if err != nil {
		t.Fatalf("ListValidators failed: %v", err)
	}
	_ = validators // ignored

	// 3) List allowlist
	allowlist, err := client.ListAllowlist(ctx)
	if err != nil {
		t.Fatalf("ListAllowlist failed: %v", err)
	}
	_ = allowlist // ignored
}

func TestSanity_DetectAndGatewayBasicFlow(t *testing.T) {
	client := getClient(t)
	ctx := context.Background()

	t.Run("detect-email", func(t *testing.T) {
		resp, err := client.DetectText(
			ctx,
			"My email is test@example.com",
			tszclient.WithRID("e2e-sanity-detect"),
		)
		if err != nil {
			t.Fatalf("DetectText failed: %v", err)
		}

		if !resp.ContainsPII {
			t.Fatal("expected ContainsPII=true for email payload")
		}
	})

	t.Run("gateway-safe", func(t *testing.T) {
		req := tszclient.ChatCompletionRequest{
			Model: "llama3.1:8b",
			Messages: []map[string]interface{}{
				{"role": "user", "content": "Hello from E2E sanity"},
			},
			Stream: false,
		}

		// Use empty headers map if none needed
		resp, err := client.ChatCompletions(ctx, req, map[string]string{"X-TSZ-RID": "E2E-SANITY-GW"})
		if err != nil {
			t.Fatalf("ChatCompletions failed: %v", err)
		}

		// Ensure we got a response map
		if resp == nil {
			t.Fatal("ChatCompletions returned nil response")
		}
	})
}
