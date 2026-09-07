package envoy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"thyris-sz/internal/controller/capabilities"
	"thyris-sz/internal/extproc"
	"thyris-sz/internal/extproc/adaptertest"
	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

func TestAdapterConformance(t *testing.T) {
	adaptertest.RunConformance(t, envoyConformanceDriver{t: t})
}

type envoyConformanceDriver struct{ t *testing.T }

func (envoyConformanceDriver) Name() string { return "envoy-gateway" }
func (envoyConformanceDriver) Capabilities() capabilities.AdapterCapabilities {
	return capabilities.EnvoyGatewayCapabilities
}

func (driver envoyConformanceDriver) Execute(ctx context.Context, scenario adaptertest.ConformanceScenario) (adaptertest.ConformanceObservation, error) {
	auditor := &conformanceAuditor{}
	metrics := newConformanceMetrics()
	server, err := NewServerWithSettings(
		scenario.Processor,
		conformancePolicyCache{snapshot: scenario.Snapshot},
		auditor,
		ServerSettings{FailMode: scenario.DefaultFailureMode, MaxBodyBytes: 1024 * 1024, ProcessingTimeout: defaultServerSettings().ProcessingTimeout, MetricsObserver: metrics},
	)
	if err != nil {
		return adaptertest.ConformanceObservation{}, err
	}
	client := newExternalProcessorTestClientForServer(driver.t, server)
	stream, err := client.Process(ctx)
	if err != nil {
		return adaptertest.ConformanceObservation{}, fmt.Errorf("open Envoy conformance stream: %w", err)
	}

	observation := adaptertest.ConformanceObservation{}
	requestHeaders := requestHeadersMessage("ignored-client-rid", "envoy-conformance-request", scenario.Snapshot.PolicyID)
	requestHeaders.GetRequestHeaders().Headers.Headers = append(requestHeaders.GetRequestHeaders().Headers.Headers,
		&corev3.HeaderValue{Key: "content-type", RawValue: []byte("application/json")},
		&corev3.HeaderValue{Key: "x-tsz-gateway", RawValue: []byte("conformance-gateway")},
		&corev3.HeaderValue{Key: "x-tsz-route", RawValue: []byte("conformance-route")},
	)
	if _, err := envoyConformanceExchange(stream, requestHeaders, &observation); err != nil {
		return observation, err
	}
	requestResponse, err := envoyConformanceExchange(stream, requestBodyForAdapterTest(scenario.RequestBody, true), &observation)
	if err != nil {
		return observation, err
	}
	if immediate := requestResponse.GetImmediateResponse(); immediate != nil {
		observation.Request.Blocked = true
		observation.Request.StatusCode = int(immediate.GetStatus().GetCode())
	} else {
		observation.Request.Forwarded = true
		observation.Request.Body = append([]byte(nil), scenario.RequestBody...)
		common := requestResponse.GetRequestBody().GetResponse()
		if mutation := common.GetBodyMutation(); mutation != nil {
			observation.Request.Body = append([]byte(nil), mutation.GetBody()...)
		}
		observation.Request.HeaderMutations = envoyHeaderMutations(common.GetHeaderMutation())
	}

	if scenario.IncludeResponse && !observation.Request.Blocked {
		if _, err := envoyConformanceExchange(stream, responseHeadersForAdapterTest(false), &observation); err != nil {
			return observation, err
		}
		responseResponse, err := envoyConformanceExchange(stream, responseBodyForAdapterTest(scenario.ResponseBody, true), &observation)
		if err != nil {
			return observation, err
		}
		if immediate := responseResponse.GetImmediateResponse(); immediate != nil {
			observation.Response.Blocked = true
			observation.Response.StatusCode = int(immediate.GetStatus().GetCode())
		} else {
			observation.Response.Forwarded = true
			observation.Response.Body = append([]byte(nil), scenario.ResponseBody...)
			common := responseResponse.GetResponseBody().GetResponse()
			if mutation := common.GetBodyMutation(); mutation != nil {
				observation.Response.Body = append([]byte(nil), mutation.GetBody()...)
			}
			observation.Response.HeaderMutations = envoyHeaderMutations(common.GetHeaderMutation())
		}
	}

	if err := stream.CloseSend(); err != nil {
		return observation, fmt.Errorf("close Envoy conformance stream: %w", err)
	}
	for {
		if _, err := stream.Recv(); err != nil {
			if err != io.EOF {
				return observation, fmt.Errorf("finish Envoy conformance stream: %w", err)
			}
			break
		}
	}
	observation.AuditEvents = auditor.snapshot()
	observation.Metrics = metrics.snapshot()
	telemetry, err := json.Marshal(struct {
		Metadata []extproc.SafeMetadata         `json:"metadata"`
		Audit    []guardrails.AuditEvent        `json:"audit"`
		Metrics  adaptertest.MetricsObservation `json:"metrics"`
		Wire     string                         `json:"wire"`
	}{Metadata: observation.Metadata, Audit: observation.AuditEvents, Metrics: observation.Metrics, Wire: string(observation.TelemetryWire)})
	if err != nil {
		return observation, err
	}
	observation.TelemetryWire = telemetry
	return observation, nil
}

func envoyConformanceExchange(stream extprocv3.ExternalProcessor_ProcessClient, message *extprocv3.ProcessingRequest, observation *adaptertest.ConformanceObservation) (*extprocv3.ProcessingResponse, error) {
	if err := stream.Send(message); err != nil {
		return nil, fmt.Errorf("send Envoy conformance message: %w", err)
	}
	response, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("receive Envoy conformance response: %w", err)
	}
	wire, err := protojson.Marshal(response)
	if err != nil {
		return nil, err
	}
	observation.TelemetryWire = append(observation.TelemetryWire, wire...)
	if response.GetDynamicMetadata() != nil {
		metadata, found, err := envoyContractMetadata(response.GetDynamicMetadata())
		if err != nil {
			return nil, err
		}
		if found {
			observation.Metadata = append(observation.Metadata, metadata)
			observation.Decisions = append(observation.Decisions, adaptertest.DecisionObservation{Stage: metadata.Stage, Action: metadata.Action, Degraded: metadata.Degraded})
		}
	}
	return response, nil
}

type conformancePolicyCache struct{ snapshot policy.CompiledSnapshot }

func (cache conformancePolicyCache) Ready() bool { return true }
func (cache conformancePolicyCache) Get(policyID string) (policy.CompiledSnapshot, bool) {
	if policyID != cache.snapshot.PolicyID {
		return policy.CompiledSnapshot{}, false
	}
	return cache.snapshot.Clone(), true
}

type conformanceAuditor struct {
	mu     sync.Mutex
	events []guardrails.AuditEvent
}

func (auditor *conformanceAuditor) Audit(_ context.Context, event guardrails.AuditEvent) error {
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	auditor.events = append(auditor.events, event)
	return nil
}

func (auditor *conformanceAuditor) snapshot() []guardrails.AuditEvent {
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	return append([]guardrails.AuditEvent(nil), auditor.events...)
}

type conformanceMetrics struct {
	mu            sync.Mutex
	requests      int
	responses     int
	activeStreams int
	actions       map[string]int
	failures      map[string]int
}

func newConformanceMetrics() *conformanceMetrics {
	return &conformanceMetrics{actions: make(map[string]int), failures: make(map[string]int)}
}

func (metrics *conformanceMetrics) IncRequest()  { metrics.update(func() { metrics.requests++ }) }
func (metrics *conformanceMetrics) IncResponse() { metrics.update(func() { metrics.responses++ }) }
func (metrics *conformanceMetrics) ObserveAction(action extproc.Action, stage extproc.ProcessingStage, _ string) {
	metrics.update(func() { metrics.actions[adaptertest.MetricKey(action, stage)]++ })
}
func (*conformanceMetrics) ObserveDetections([]string, extproc.ProcessingStage) {}
func (*conformanceMetrics) ObserveDuration(extproc.ProcessingStage, float64)    {}
func (metrics *conformanceMetrics) IncFailure(reason string) {
	metrics.update(func() { metrics.failures[reason]++ })
}
func (*conformanceMetrics) IncTimeout()                  {}
func (*conformanceMetrics) ObserveBodyBytes(string, int) {}
func (metrics *conformanceMetrics) IncActiveStreams() {
	metrics.update(func() { metrics.activeStreams++ })
}
func (metrics *conformanceMetrics) DecActiveStreams() {
	metrics.update(func() { metrics.activeStreams-- })
}
func (*conformanceMetrics) IncStreamHalt() {}

func (metrics *conformanceMetrics) update(fn func()) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	fn()
}

func (metrics *conformanceMetrics) snapshot() adaptertest.MetricsObservation {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	actions := make(map[string]int, len(metrics.actions))
	for key, value := range metrics.actions {
		actions[key] = value
	}
	failures := make(map[string]int, len(metrics.failures))
	for key, value := range metrics.failures {
		failures[key] = value
	}
	return adaptertest.MetricsObservation{Requests: metrics.requests, Responses: metrics.responses, ActiveStreams: metrics.activeStreams, Actions: actions, Failures: failures}
}
