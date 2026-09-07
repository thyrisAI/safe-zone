package observability

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"thyris-sz/internal/extproc"
)

// TracingConfig is intentionally small: standard OTEL_EXPORTER_OTLP_*
// variables configure the collector, and tracing remains disabled unless an
// endpoint is explicitly supplied.
type TracingConfig struct {
	Endpoint string
	Insecure bool
}

func TracingConfigFromEnv() (TracingConfig, error) {
	insecure := false
	if raw := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_INSECURE")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return TracingConfig{}, fmt.Errorf("OTEL_EXPORTER_OTLP_INSECURE must be boolean: %w", err)
		}
		insecure = value
	}
	return TracingConfig{Endpoint: strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")), Insecure: insecure}, nil
}

// Tracing owns the optional OTLP provider. An empty endpoint leaves the
// process on OpenTelemetry's no-op provider, so local deployments need no
// collector configuration.
type Tracing struct {
	tracer   trace.Tracer
	shutdown func(context.Context) error
}

func NewTracing(ctx context.Context, config TracingConfig) (*Tracing, error) {
	tracing := &Tracing{tracer: otel.Tracer("thyris-sz/extproc"), shutdown: func(context.Context) error { return nil }}
	if config.Endpoint == "" {
		return tracing, nil
	}
	options := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(config.Endpoint)}
	if config.Insecure {
		options = append(options, otlptracegrpc.WithInsecure())
	}
	exporter, err := otlptracegrpc.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes("", attribute.String("service.name", "tsz-ext-proc"))),
	)
	otel.SetTracerProvider(provider)
	tracing.tracer = provider.Tracer("thyris-sz/extproc")
	tracing.shutdown = provider.Shutdown
	return tracing, nil
}

func (t *Tracing) Shutdown(ctx context.Context) error { return t.shutdown(ctx) }

// StartGuardrail extracts only a traceparent supplied in Envoy's configured,
// trusted attributes. It never trusts a client HTTP header directly.
func (t *Tracing) StartGuardrail(ctx context.Context, request extproc.ProcessingRequest) (context.Context, func(extproc.ProcessingResult, error)) {
	if request.TraceParent != "" {
		ctx = propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": request.TraceParent})
	}
	name := "tsz.extproc.request"
	if request.Stage == extproc.StageResponse {
		name = "tsz.extproc.response"
	}
	ctx, span := t.tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindInternal))
	span.SetAttributes(
		attribute.String("tsz.stage", string(request.Stage)),
		attribute.String("tsz.policy.id", request.PolicyID),
		attribute.Int("tsz.policy.version", request.PolicyVersion),
		attribute.String("tsz.rid", request.RID),
		attribute.String("envoy.request_id", request.EnvoyReqID),
	)
	return ctx, func(result extproc.ProcessingResult, processingErr error) {
		span.SetAttributes(
			attribute.String("tsz.action", string(result.Action)),
			attribute.Int("tsz.detection_count", result.DetectionCount),
			attribute.Bool("tsz.degraded", result.Degraded),
		)
		if processingErr != nil {
			span.RecordError(processingErr)
		}
		span.End()
	}
}
