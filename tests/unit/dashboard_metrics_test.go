package unit

import (
	"testing"
	"time"

	"thyris-sz/internal/metrics"
)

func TestRecordEvent_IncrementsCounters(t *testing.T) {
	before := metrics.GetSummary()

	metrics.RecordEvent(metrics.Event{
		Timestamp: time.Now(),
		RequestID: "TEST-ALLOWED-PII",
		Blocked:   false,
		Reason:    "PII",
	})
	metrics.RecordEvent(metrics.Event{
		Timestamp: time.Now(),
		RequestID: "TEST-BLOCKED-GUARDRAIL",
		Blocked:   true,
		Reason:    "GUARDRAIL",
	})

	after := metrics.GetSummary()

	if got := after.TotalRequests - before.TotalRequests; got != 2 {
		t.Fatalf("expected TotalRequests to increase by 2, got %d", got)
	}
	if got := after.Allowed - before.Allowed; got != 1 {
		t.Fatalf("expected Allowed to increase by 1, got %d", got)
	}
	if got := after.Blocked - before.Blocked; got != 1 {
		t.Fatalf("expected Blocked to increase by 1, got %d", got)
	}
	if got := after.PIIDetections - before.PIIDetections; got != 1 {
		t.Fatalf("expected PIIDetections to increase by 1, got %d", got)
	}
}

// TestRecordEvent_RingBufferRespectsLimit, event listesinin
// sınırsız büyümediğini doğrular (bkz. internal/metrics/store.go
// -- maxEvents = 50).
func TestRecordEvent_RingBufferRespectsLimit(t *testing.T) {
	const ringBufferCap = 50 // internal/metrics/store.go içindeki maxEvents ile eşleşmeli

	// Sınırın kesinlikle üzerine çıkacak kadar event kaydediyoruz.
	for i := 0; i < ringBufferCap+10; i++ {
		metrics.RecordEvent(metrics.Event{
			Timestamp: time.Now(),
			RequestID: "TEST-RING-BUFFER",
			Blocked:   false,
			Reason:    "RULE",
		})
	}

	// Büyük bir limit isteyerek, gerçekte kaç event tutulduğunu görüyoruz.
	events := metrics.GetRecentEvents(1000)

	if len(events) > ringBufferCap {
		t.Fatalf("expected at most %d events in buffer, got %d", ringBufferCap, len(events))
	}
}

// TestGetRecentEvents_NewestFirst, en son kaydedilen event'in
// listenin başında (en yeni ilk sırada) döndüğünü doğrular --
// Events ekranının doğru sırada görünmesi için kritik.
func TestGetRecentEvents_NewestFirst(t *testing.T) {
	metrics.RecordEvent(metrics.Event{
		Timestamp: time.Now(),
		RequestID: "TEST-ORDER-OLDER",
		Blocked:   false,
		Reason:    "RULE",
	})
	metrics.RecordEvent(metrics.Event{
		Timestamp: time.Now(),
		RequestID: "TEST-ORDER-NEWEST",
		Blocked:   false,
		Reason:    "RULE",
	})

	events := metrics.GetRecentEvents(1)

	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event, got %d", len(events))
	}
	if events[0].RequestID != "TEST-ORDER-NEWEST" {
		t.Fatalf("expected newest event first, got RequestID=%s", events[0].RequestID)
	}
}

// TestGetDashboardEvents_LimitZeroReturnsAll, limit<=0 verildiğinde
// mevcut tüm event'lerin döndüğünü doğrular (store.go'daki
// "limit <= 0" fallback davranışı).
func TestGetRecentEvents_ZeroLimitReturnsAll(t *testing.T) {
	metrics.RecordEvent(metrics.Event{
		Timestamp: time.Now(),
		RequestID: "TEST-ZERO-LIMIT",
		Blocked:   false,
		Reason:    "RULE",
	})

	all := metrics.GetRecentEvents(0)
	explicit := metrics.GetRecentEvents(len(all))

	if len(all) != len(explicit) {
		t.Fatalf("expected GetRecentEvents(0) to return all %d events, got %d", len(explicit), len(all))
	}
}
