package guardrails

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebhookAuditorDeliversSafeAuditEvent(t *testing.T) {
	var received AuditEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected request %s content-type=%q", r.Method, r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	auditor, err := NewWebhookAuditor(server.URL)
	if err != nil {
		t.Fatalf("NewWebhookAuditor() error = %v", err)
	}
	event := AuditEvent{Timestamp: time.Now().UTC(), RID: "RID-1", RequestID: "envoy-1", Adapter: "envoy-gateway", PolicyID: "default", PolicyVersion: 1, Stage: AuditStageRequest, Action: "MASK", Categories: []string{"PII"}, DetectionCount: 1}
	if err := auditor.Audit(context.Background(), event); err != nil {
		t.Fatalf("Audit() error = %v", err)
	}
	if received.RID != event.RID || received.RequestID != event.RequestID || received.Action != event.Action || received.Categories[0] != "PII" {
		t.Fatalf("received event = %+v", received)
	}
}

func TestNewWebhookAuditorRejectsInvalidEndpoint(t *testing.T) {
	if _, err := NewWebhookAuditor("file:///tmp/audit"); err == nil {
		t.Fatal("NewWebhookAuditor() accepted non-HTTP endpoint")
	}
}
