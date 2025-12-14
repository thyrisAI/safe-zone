package unit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"thyris-sz/internal/ai"
	"thyris-sz/internal/cache"
	"thyris-sz/internal/config"
	"thyris-sz/internal/guardrails"
	"thyris-sz/internal/models"
)

// --- SIEM tests ---

// fakeRoundTripper captures outgoing HTTP requests for inspection.
type fakeRoundTripper struct {
	req *http.Request
}

func (f *fakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	f.req = r
	// Return 200 OK with empty body
	return &http.Response{
		StatusCode: 200,
		Body:       http.NoBody,
		Header:     make(http.Header),
	}, nil
}

func TestPublishSecurityEvent_RespectsEnvAndSendsJSON(t *testing.T) {
	// Arrange
	_ = os.Setenv("SIEM_WEBHOOK_URL", "http://siem.local/webhook")
	defer os.Unsetenv("SIEM_WEBHOOK_URL")

	// Swap default transport
	origTransport := http.DefaultTransport
	fake := &fakeRoundTripper{}
	http.DefaultTransport = fake
	defer func() { http.DefaultTransport = origTransport }()

	ev := models.SecurityEvent{
		Type:            "BLOCK",
		Action:          "BLOCK",
		Category:        "EMAIL",
		Pattern:         "EMAIL",
		ConfidenceScore: 0.95,
		Threshold:       0.85,
		RequestID:       "RID-UNIT-1",
		Timestamp:       time.Now().Unix(),
	}

	// Act
	guardrails.TestPublishSecurityEventForUnit(ev)

	// Assert
	if fake.req == nil {
		t.Fatalf("expected HTTP request to SIEM webhook, got nil")
	}
	if fake.req.URL.String() != "http://siem.local/webhook" {
		t.Fatalf("unexpected SIEM URL: %s", fake.req.URL.String())
	}
	if ct := fake.req.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", ct)
	}

	var got models.SecurityEvent
	if err := json.NewDecoder(fake.req.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode SIEM payload: %v", err)
	}
	if got.Type != "BLOCK" || got.Pattern != "EMAIL" {
		t.Fatalf("unexpected SIEM payload: %+v", got)
	}
}

// --- AI client tests ---

func TestCheckWithAI_PropagatesNon200Status(t *testing.T) {
	// Fake upstream AI server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad prompt"}`))
	}))
	defer ts.Close()

	// Minimal config
	config.AppConfig = &config.Config{
		AIModelURL:  ts.URL,
		AIAPIKey:    "test-key",
		AIModelName: "test-model",
	}

	ok, err := ai.CheckWithAI("text", "Respond YES if ok", "YES")
	if err == nil || ok {
		t.Fatalf("expected error and false for non-200 AI response, got ok=%v err=%v", ok, err)
	}
}

func TestCheckWithAI_SuccessfulYESResponse(t *testing.T) {
	// Fake upstream AI server that returns a YES-like content
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"content":"YES, looks fine"}}]}`))
	}))
	defer ts.Close()

	config.AppConfig = &config.Config{
		AIModelURL:  ts.URL,
		AIAPIKey:    "test-key",
		AIModelName: "test-model",
	}

	ok, err := ai.CheckWithAI("some text", "Respond YES if ok", "YES")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true for YES response")
	}
}

// --- Repository + cache tests ---

func initTestDBAndRedis(t *testing.T) {
	config.AppConfig = &config.Config{
		DBDSN:    "postgres://user:pass@localhost:5432/dbname?sslmode=disable",
		RedisURL: "redis://localhost:6379/0",
	}

	cache.RDB = redis.NewClient(&redis.Options{Addr: "localhost:6379"})
}

func TestAIConfidenceCache_KeyAndRoundtrip(t *testing.T) {
	initTestDBAndRedis(t)

	label := "EMAIL"
	text := "alice@example.com"

	ai.SetCachedConfidence(label, text, 0.87, time.Second)

	val, ok := ai.GetCachedConfidence(label, text)
	if !ok {
		t.Skip("Redis not available or cache miss; skipping AI confidence cache test")
	}
	if val != 0.87 {
		t.Fatalf("expected cached value 0.87, got %v", val)
	}
}
