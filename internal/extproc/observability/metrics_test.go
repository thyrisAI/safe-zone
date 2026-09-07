package observability

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"thyris-sz/internal/extproc"
)

func TestExtProcMetricsRegistersIssue34MetricFamilies(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewExtProcMetrics(registry)
	if err != nil {
		t.Fatalf("NewExtProcMetrics() error = %v", err)
	}

	metrics.IncRequest()
	metrics.IncResponse()
	metrics.ObserveAction(extproc.ActionMask, extproc.StageRequest, "production")
	metrics.ObserveDetections([]string{"PII"}, extproc.StageRequest)
	metrics.ObserveDuration(extproc.StageRequest, 0.01)
	metrics.IncFailure("processor")
	metrics.IncTimeout()
	metrics.ObserveBodyBytes("request", 42)
	metrics.IncActiveStreams()
	metrics.DecActiveStreams()
	metrics.IncStreamHalt()
	metrics.ObservePolicyReconcile("success", 10*time.Millisecond)
	metrics.ObservePolicyActivationNotification("success")
	metrics.SetActivePolicySnapshots(2)

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	want := map[string]bool{
		"tsz_extproc_requests_total":                      false,
		"tsz_extproc_responses_total":                     false,
		"tsz_extproc_actions_total":                       false,
		"tsz_extproc_detections_total":                    false,
		"tsz_extproc_processing_duration_seconds":         false,
		"tsz_extproc_failures_total":                      false,
		"tsz_extproc_timeouts_total":                      false,
		"tsz_extproc_body_bytes":                          false,
		"tsz_extproc_active_streams":                      false,
		"tsz_extproc_stream_halts_total":                  false,
		"tsz_policy_cache_reconcile_duration_seconds":     false,
		"tsz_policy_cache_activation_notifications_total": false,
		"tsz_policy_cache_active_snapshots":               false,
	}
	for _, family := range families {
		if _, ok := want[family.GetName()]; ok {
			want[family.GetName()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("metric family %q was not registered", name)
		}
	}
}
