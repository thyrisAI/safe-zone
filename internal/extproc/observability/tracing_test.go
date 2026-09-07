package observability

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"thyris-sz/internal/extproc"
)

func TestTracingContinuesTrustedTraceParentWithoutContentAttributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracing := &Tracing{tracer: provider.Tracer("test"), shutdown: provider.Shutdown}
	request := extproc.ProcessingRequest{
		Stage:         extproc.StageRequest,
		RID:           "RID-123",
		EnvoyReqID:    "envoy-123",
		PolicyID:      "production",
		PolicyVersion: 7,
		TraceParent:   "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}
	_, end := tracing.StartGuardrail(context.Background(), request)
	end(extproc.ProcessingResult{Action: extproc.ActionAllow}, nil)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if got := span.SpanContext().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace ID = %q, want propagated trace ID", got)
	}
	for _, attribute := range span.Attributes() {
		if attribute.Key == "http.request.body" || attribute.Key == "tsz.body" {
			t.Fatalf("unsafe content attribute %q was recorded", attribute.Key)
		}
	}
}
