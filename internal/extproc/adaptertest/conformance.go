package adaptertest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"thyris-sz/internal/controller/capabilities"
	"thyris-sz/internal/extproc"
	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

const (
	conformancePolicyID      = "adapter-conformance"
	conformancePolicyVersion = 17
	requestSensitiveFixture  = "request-secret@example.test"
	responseSensitiveFixture = "response-secret@example.test"
	maskedFixture            = "[REDACTED:CONFORMANCE]"
)

// ConformanceScenario supplies the shared processor and immutable policy that
// a gateway driver must run through its real native request/response pipeline.
type ConformanceScenario struct {
	Processor          extproc.Processor
	Snapshot           policy.CompiledSnapshot
	DefaultFailureMode policy.FailureMode
	RequestBody        []byte
	ResponseBody       []byte
	IncludeResponse    bool
}

// TrafficObservation describes the body that crossed a gateway boundary.
// Forwarded is false when the adapter terminated that direction locally.
type TrafficObservation struct {
	Forwarded       bool
	Blocked         bool
	StatusCode      int
	Body            []byte
	HeaderMutations map[string]string
}

// DecisionObservation is the safe semantic outcome observed at the native
// boundary. It remains available even when native dynamic metadata is absent.
type DecisionObservation struct {
	Stage    extproc.ProcessingStage
	Action   extproc.Action
	Degraded bool
}

// MetricsObservation contains only bounded labels and aggregate counters.
type MetricsObservation struct {
	Requests      int
	Responses     int
	ActiveStreams int
	Actions       map[string]int
	Failures      map[string]int
}

// ConformanceObservation is the gateway-neutral result of one complete native
// exchange. TelemetryWire contains only what the adapter exported to native
// metadata, audit, metrics, or tracing sinks; it must not include input bodies.
type ConformanceObservation struct {
	Request       TrafficObservation
	Response      TrafficObservation
	Decisions     []DecisionObservation
	Metadata      []extproc.SafeMetadata
	AuditEvents   []guardrails.AuditEvent
	Metrics       MetricsObservation
	TelemetryWire []byte
}

// ConformanceDriver runs shared scenarios through a gateway's real server,
// middleware, or plugin entry point and observes the resulting boundaries.
type ConformanceDriver interface {
	Name() string
	Capabilities() capabilities.AdapterCapabilities
	Execute(context.Context, ConformanceScenario) (ConformanceObservation, error)
}

// RunConformance verifies policy outcomes across the complete adapter/runtime
// path. Protocol-shape assertions remain in Run; these tests exercise security
// behavior shared by every gateway implementation.
func RunConformance(t *testing.T, driver ConformanceDriver) {
	t.Helper()
	if driver == nil {
		t.Fatal("adapter conformance driver is nil")
	}
	caps := driver.Capabilities()
	if err := capabilities.ValidateDeclaration(caps); err != nil {
		t.Fatalf("%s capability declaration: %v", driver.Name(), err)
	}
	if driver.Name() != caps.Name {
		t.Fatalf("driver name %q does not match capability name %q", driver.Name(), caps.Name)
	}

	if caps.RequestBodyMutation {
		t.Run("masks request before upstream", func(t *testing.T) {
			observation := executeConformance(t, driver, contentScenario(extproc.ActionMask, extproc.StageRequest))
			if observation.Request.Blocked || !observation.Request.Forwarded {
				t.Fatalf("masked request was not forwarded: %+v", observation.Request)
			}
			assertMaskedTraffic(t, observation.Request, requestSensitiveFixture)
			assertDecision(t, observation, extproc.StageRequest, extproc.ActionMask, false)
		})
	}

	if caps.ImmediateResponse {
		t.Run("blocks request before upstream", func(t *testing.T) {
			observation := executeConformance(t, driver, contentScenario(extproc.ActionBlock, extproc.StageRequest))
			if !observation.Request.Blocked || observation.Request.Forwarded || observation.Request.StatusCode == 0 {
				t.Fatalf("blocked request boundary = %+v", observation.Request)
			}
			if bytes.Contains(observation.TelemetryWire, []byte(requestSensitiveFixture)) {
				t.Fatal("blocked request fixture leaked through native telemetry")
			}
			assertDecision(t, observation, extproc.StageRequest, extproc.ActionBlock, false)
		})
	}

	if caps.ResponseBufferedBody && caps.ResponseBodyMutation {
		t.Run("filters response before client", func(t *testing.T) {
			observation := executeConformance(t, driver, contentScenario(extproc.ActionMask, extproc.StageResponse))
			if !observation.Request.Forwarded || observation.Response.Blocked || !observation.Response.Forwarded {
				t.Fatalf("response filtering boundaries: request=%+v response=%+v", observation.Request, observation.Response)
			}
			assertMaskedTraffic(t, observation.Response, responseSensitiveFixture)
			assertDecision(t, observation, extproc.StageResponse, extproc.ActionMask, false)
		})
	}

	t.Run("applies request failure modes", func(t *testing.T) {
		for _, test := range []struct {
			name      string
			mode      policy.FailureMode
			wantBlock bool
		}{
			{name: "closed", mode: policy.FailureModeClosed, wantBlock: true},
			{name: "open", mode: policy.FailureModeOpen, wantBlock: false},
		} {
			t.Run(test.name, func(t *testing.T) {
				if test.wantBlock && !caps.ImmediateResponse {
					t.Skip("adapter does not declare immediate responses")
				}
				scenario := failureScenario(extproc.StageRequest, test.mode, policy.FailureModeClosed)
				observation := executeConformance(t, driver, scenario)
				if observation.Request.Blocked != test.wantBlock || observation.Request.Forwarded == test.wantBlock {
					t.Fatalf("request %s boundary = %+v", test.mode, observation.Request)
				}
				wantAction := extproc.ActionAllow
				if test.wantBlock {
					wantAction = extproc.ActionBlock
				} else if !bytes.Equal(observation.Request.Body, scenario.RequestBody) {
					t.Fatalf("fail-open request changed: %q", observation.Request.Body)
				}
				assertDecision(t, observation, extproc.StageRequest, wantAction, true)
				if observation.Metrics.Failures["processor"] == 0 {
					t.Fatalf("request processor failure metric missing: %+v", observation.Metrics)
				}
			})
		}
	})

	if caps.ResponseBufferedBody {
		t.Run("applies response failure modes", func(t *testing.T) {
			for _, test := range []struct {
				name      string
				mode      policy.FailureMode
				wantBlock bool
			}{
				{name: "closed", mode: policy.FailureModeClosed, wantBlock: true},
				{name: "open", mode: policy.FailureModeOpen, wantBlock: false},
			} {
				t.Run(test.name, func(t *testing.T) {
					if test.wantBlock && !caps.ImmediateResponse {
						t.Skip("adapter does not declare immediate responses")
					}
					scenario := failureScenario(extproc.StageResponse, policy.FailureModeClosed, test.mode)
					observation := executeConformance(t, driver, scenario)
					if !observation.Request.Forwarded || observation.Response.Blocked != test.wantBlock || observation.Response.Forwarded == test.wantBlock {
						t.Fatalf("response %s boundaries: request=%+v response=%+v", test.mode, observation.Request, observation.Response)
					}
					wantAction := extproc.ActionAllow
					if test.wantBlock {
						wantAction = extproc.ActionBlock
					} else if !bytes.Equal(observation.Response.Body, scenario.ResponseBody) {
						t.Fatalf("fail-open response changed: %q", observation.Response.Body)
					}
					assertDecision(t, observation, extproc.StageResponse, wantAction, true)
					if observation.Metrics.Failures["processor"] == 0 {
						t.Fatalf("response processor failure metric missing: %+v", observation.Metrics)
					}
				})
			}
		})
	}

	t.Run("exports correlated PII-safe telemetry", func(t *testing.T) {
		scenario := telemetryScenario(caps.ResponseBufferedBody, caps.ResponseBufferedBody && caps.ResponseBodyMutation)
		observation := executeConformance(t, driver, scenario)
		wantResponses := 0
		if scenario.IncludeResponse {
			wantResponses = 1
		}
		if observation.Metrics.Requests != 1 || observation.Metrics.Responses != wantResponses || observation.Metrics.ActiveStreams != 0 {
			t.Fatalf("transaction metrics = %+v", observation.Metrics)
		}
		if observation.Metrics.Actions[MetricKey(extproc.ActionMask, extproc.StageRequest)] == 0 {
			t.Fatalf("request MASK metric missing: %+v", observation.Metrics.Actions)
		}
		if caps.ResponseBufferedBody && caps.ResponseBodyMutation && observation.Metrics.Actions[MetricKey(extproc.ActionMask, extproc.StageResponse)] == 0 {
			t.Fatalf("response MASK metric missing: %+v", observation.Metrics.Actions)
		}
		assertCorrelatedAudit(t, observation.AuditEvents)
		if caps.DynamicMetadata {
			assertCorrelatedMetadata(t, observation.Metadata)
		}
		for _, sensitive := range []string{requestSensitiveFixture, responseSensitiveFixture} {
			if bytes.Contains(observation.TelemetryWire, []byte(sensitive)) {
				t.Fatalf("raw sensitive fixture %q leaked into telemetry", sensitive)
			}
		}
	})
}

// MetricKey is the stable key used by aggregate conformance observations.
func MetricKey(action extproc.Action, stage extproc.ProcessingStage) string {
	return string(action) + "|" + string(stage)
}

func executeConformance(t *testing.T, driver ConformanceDriver, scenario ConformanceScenario) ConformanceObservation {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observation, err := driver.Execute(ctx, scenario)
	if err != nil {
		t.Fatalf("execute %s conformance scenario: %v", driver.Name(), err)
	}
	return observation
}

func contentScenario(action extproc.Action, stage extproc.ProcessingStage) ConformanceScenario {
	return ConformanceScenario{
		Processor:          contentProcessor(action, stage),
		Snapshot:           conformanceSnapshot(policy.FailureModeClosed, policy.FailureModeClosed),
		DefaultFailureMode: policy.FailureModeClosed,
		RequestBody:        []byte(`{"model":"test","messages":[{"role":"user","content":"` + requestSensitiveFixture + `"}]}`),
		ResponseBody:       []byte(`{"choices":[{"message":{"role":"assistant","content":"` + responseSensitiveFixture + `"}}]}`),
		IncludeResponse:    stage == extproc.StageResponse,
	}
}

func telemetryScenario(includeResponse, filterResponse bool) ConformanceScenario {
	stage := extproc.StageRequest
	if filterResponse {
		stage = ""
	}
	return ConformanceScenario{
		Processor:          contentProcessor(extproc.ActionMask, stage),
		Snapshot:           conformanceSnapshot(policy.FailureModeClosed, policy.FailureModeClosed),
		DefaultFailureMode: policy.FailureModeClosed,
		RequestBody:        []byte(`{"model":"test","messages":[{"role":"user","content":"` + requestSensitiveFixture + `"}]}`),
		ResponseBody:       []byte(`{"choices":[{"message":{"role":"assistant","content":"` + responseSensitiveFixture + `"}}]}`),
		IncludeResponse:    includeResponse,
	}
}

func failureScenario(failingStage extproc.ProcessingStage, requestMode, responseMode policy.FailureMode) ConformanceScenario {
	return ConformanceScenario{
		Processor:          failingBodyProcessor{stage: failingStage},
		Snapshot:           conformanceSnapshot(requestMode, responseMode),
		DefaultFailureMode: policy.FailureModeClosed,
		RequestBody:        []byte(`{"model":"test","messages":[{"role":"user","content":"failure request"}]}`),
		ResponseBody:       []byte(`{"choices":[{"message":{"role":"assistant","content":"failure response"}}]}`),
		IncludeResponse:    failingStage == extproc.StageResponse,
	}
}

func conformanceSnapshot(requestMode, responseMode policy.FailureMode) policy.CompiledSnapshot {
	return policy.CompiledSnapshot{
		PolicyID: conformancePolicyID,
		Version:  conformancePolicyVersion,
		Definition: policy.PolicyDefinition{
			Request:       policy.RequestPolicy{PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly},
			Response:      policy.ResponsePolicy{Enabled: true, PII: policy.ActionMask, Secret: policy.ActionBlock, UnsafeContent: policy.ActionBlock},
			FailurePolicy: policy.FailurePolicy{Request: requestMode, Response: responseMode},
		},
	}
}

type inspectServiceFunc func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error)

func (fn inspectServiceFunc) Inspect(ctx context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
	return fn(ctx, input)
}

func contentProcessor(action extproc.Action, stage extproc.ProcessingStage) extproc.Processor {
	processor, err := extproc.NewOpenAIRequestProcessor(inspectServiceFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		matchedStage := stage == "" || (stage == extproc.StageRequest && strings.Contains(input.Text, requestSensitiveFixture)) || (stage == extproc.StageResponse && strings.Contains(input.Text, responseSensitiveFixture))
		if !matchedStage {
			return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
		}
		return guardrails.InspectResult{
			Action: guardrails.RuleAction(action), SafeContent: maskedFixture,
			DetectionCount: 1, Categories: []string{"PII"}, ContainsSensitive: true,
		}, nil
	}))
	if err != nil {
		panic(err)
	}
	return processor
}

type failingBodyProcessor struct{ stage extproc.ProcessingStage }

func (processor failingBodyProcessor) Process(_ context.Context, request extproc.ProcessingRequest) (extproc.ProcessingResult, error) {
	if request.Stage == processor.stage && request.Body != nil {
		return extproc.ProcessingResult{}, errors.New("injected conformance processor failure")
	}
	return extproc.ProcessingResult{Action: extproc.ActionAllow}, nil
}

func assertMaskedTraffic(t *testing.T, traffic TrafficObservation, sensitive string) {
	t.Helper()
	if bytes.Contains(traffic.Body, []byte(sensitive)) || !bytes.Contains(traffic.Body, []byte(maskedFixture)) {
		t.Fatalf("masked body = %q", traffic.Body)
	}
	if got := traffic.HeaderMutations["content-length"]; got != fmt.Sprintf("%d", len(traffic.Body)) {
		t.Fatalf("content-length mutation = %q, want %d", got, len(traffic.Body))
	}
}

func assertDecision(t *testing.T, observation ConformanceObservation, stage extproc.ProcessingStage, action extproc.Action, degraded bool) {
	t.Helper()
	for _, decision := range observation.Decisions {
		if decision.Stage == stage && decision.Action == action && decision.Degraded == degraded {
			return
		}
	}
	t.Fatalf("missing decision stage=%s action=%s degraded=%t in %+v", stage, action, degraded, observation.Decisions)
}

func assertCorrelatedAudit(t *testing.T, events []guardrails.AuditEvent) {
	t.Helper()
	for _, event := range events {
		if event.PolicyID == conformancePolicyID && event.PolicyVersion == conformancePolicyVersion && event.Adapter != "" && event.RequestID != "" && event.RID != "" && event.Action == guardrails.RuleActionMask && event.DetectionCount == 1 {
			return
		}
	}
	t.Fatalf("correlated MASK audit event missing: %+v", events)
}

func assertCorrelatedMetadata(t *testing.T, metadata []extproc.SafeMetadata) {
	t.Helper()
	for _, value := range metadata {
		if value.PolicyID == conformancePolicyID && value.PolicyVersion == conformancePolicyVersion && value.Adapter != "" && value.RequestID != "" && value.RID != "" && value.Action == extproc.ActionMask && value.DetectionCount == 1 {
			return
		}
	}
	encoded, _ := json.Marshal(metadata)
	t.Fatalf("correlated MASK dynamic metadata missing: %s", encoded)
}
