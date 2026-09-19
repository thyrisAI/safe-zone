package envoy

import (
	"bytes"
	"context"
	"strings"
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"thyris-sz/internal/extproc"
	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

const agentgatewayGenericJSONPolicyID = "agentgateway-generic-json"

type agentgatewayGenericJSONInspector func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error)

func (fn agentgatewayGenericJSONInspector) Inspect(ctx context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
	return fn(ctx, input)
}

type agentgatewayGenericJSONPolicyCache struct{}

func (agentgatewayGenericJSONPolicyCache) Ready() bool { return true }

func (agentgatewayGenericJSONPolicyCache) Get(policyID string) (policy.CompiledSnapshot, bool) {
	if policyID != agentgatewayGenericJSONPolicyID {
		return policy.CompiledSnapshot{}, false
	}
	return policy.CompiledSnapshot{
		PolicyID: agentgatewayGenericJSONPolicyID,
		Version:  6,
		Definition: policy.PolicyDefinition{
			Request: policy.RequestPolicy{PII: policy.ActionMask, Secret: policy.ActionBlock},
			Response: policy.ResponsePolicy{
				Enabled: true, PII: policy.ActionMask, Secret: policy.ActionBlock,
			},
			FailurePolicy: policy.FailurePolicy{Request: policy.FailureModeClosed, Response: policy.FailureModeClosed},
		},
	}, true
}

func TestAgentgatewayGenericJSONCompatibility(t *testing.T) {
	processor, err := extproc.NewOpenAIRequestProcessor(agentgatewayGenericJSONInspector(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		switch {
		case strings.Contains(input.Text, "blocked-secret"):
			return guardrails.InspectResult{Action: guardrails.RuleActionBlock, DetectionCount: 1, Categories: []string{"SECRET"}}, nil
		case strings.Contains(input.Text, "audit-only"):
			return guardrails.InspectResult{Action: guardrails.RuleActionAuditOnly, SafeContent: input.Text, DetectionCount: 1, Categories: []string{"CUSTOM"}}, nil
		case strings.Contains(input.Text, "person@example.test"):
			return guardrails.InspectResult{
				Action: guardrails.RuleActionMask, SafeContent: strings.ReplaceAll(input.Text, "person@example.test", "[MASKED]"),
				DetectionCount: 1, Categories: []string{"PII"},
			}, nil
		default:
			return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
		}
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	server, err := NewServerWithSettings(processor, agentgatewayGenericJSONPolicyCache{}, nil, ServerSettings{AdapterName: "agentgateway"})
	if err != nil {
		t.Fatalf("NewServerWithSettings() error = %v", err)
	}
	client := newExternalProcessorTestClientForServer(t, server)

	t.Run("masks complete request and response documents", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayGenericJSONHeaders(t, stream)

		requestBody := []byte(`{"customer":{"email":"person@example.test"},"active":true,"attempts":2}`)
		requestResponse := exchangeAgentgatewayGenericJSONMessage(t, stream, requestBodyForAdapterTest(requestBody, true))
		requestMutation := requestResponse.GetRequestBody().GetResponse().GetBodyMutation().GetBody()
		if bytes.Contains(requestMutation, []byte("person@example.test")) || !bytes.Contains(requestMutation, []byte("[MASKED]")) || !bytes.Contains(requestMutation, []byte(`"active":true`)) {
			t.Fatalf("request mutation = %s", requestMutation)
		}
		assertAgentgatewayGenericJSONMetadata(t, requestResponse, extproc.StageRequest, extproc.ActionMask)

		responseHeaders := responseHeadersForAdapterTest(false)
		responseHeaders.GetResponseHeaders().Headers = testHeaderMap([2]string{"content-type", "application/problem+json"})
		exchangeAgentgatewayGenericJSONMessage(t, stream, responseHeaders)
		responseBody := []byte(`{"status":200,"result":{"owner":"person@example.test"}}`)
		responseResponse := exchangeAgentgatewayGenericJSONMessage(t, stream, responseBodyForAdapterTest(responseBody, true))
		responseMutation := responseResponse.GetResponseBody().GetResponse().GetBodyMutation().GetBody()
		if bytes.Contains(responseMutation, []byte("person@example.test")) || !bytes.Contains(responseMutation, []byte("[MASKED]")) || !bytes.Contains(responseMutation, []byte(`"status":200`)) {
			t.Fatalf("response mutation = %s", responseMutation)
		}
		assertAgentgatewayGenericJSONMetadata(t, responseResponse, extproc.StageResponse, extproc.ActionMask)
		if err := stream.CloseSend(); err != nil {
			t.Fatalf("CloseSend() error = %v", err)
		}
	})

	t.Run("blocks request before HTTP backend", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayGenericJSONHeaders(t, stream)
		body := []byte(`{"credentials":{"token":"blocked-secret"}}`)
		response := exchangeAgentgatewayGenericJSONMessage(t, stream, requestBodyForAdapterTest(body, true))
		if response.GetImmediateResponse() == nil || bytes.Contains(response.GetImmediateResponse().GetBody(), []byte("blocked-secret")) {
			t.Fatalf("unsafe immediate response = %v", response.GetImmediateResponse())
		}
		assertAgentgatewayGenericJSONMetadata(t, response, extproc.StageRequest, extproc.ActionBlock)
	})

	t.Run("audits without mutating", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayGenericJSONHeaders(t, stream)
		body := []byte(`{"note":"audit-only"}`)
		response := exchangeAgentgatewayGenericJSONMessage(t, stream, requestBodyForAdapterTest(body, true))
		if mutation := response.GetRequestBody().GetResponse().GetBodyMutation(); mutation != nil {
			t.Fatalf("audit-only body was unexpectedly mutated: %v", mutation)
		}
		assertAgentgatewayGenericJSONMetadata(t, response, extproc.StageRequest, extproc.ActionAuditOnly)
	})
}

func sendAgentgatewayGenericJSONHeaders(t *testing.T, stream extprocv3.ExternalProcessor_ProcessClient) {
	t.Helper()
	headers := requestHeadersMessage("RID-agentgateway-json", "agentgateway-json-request-1", agentgatewayGenericJSONPolicyID)
	headers.GetRequestHeaders().Headers.Headers = append(headers.GetRequestHeaders().Headers.Headers,
		&corev3.HeaderValue{Key: "content-type", RawValue: []byte("application/json")},
		&corev3.HeaderValue{Key: ":path", RawValue: []byte("/api/customer-profiles")},
		&corev3.HeaderValue{Key: "x-tsz-gateway", RawValue: []byte("agentgateway-proxy")},
		&corev3.HeaderValue{Key: "x-tsz-route", RawValue: []byte("customer-api")},
		&corev3.HeaderValue{Key: "x-tsz-content-adapter", RawValue: []byte(extproc.GenericJSONContentAdapter)},
	)
	response := exchangeAgentgatewayGenericJSONMessage(t, stream, headers)
	if response.GetRequestHeaders() == nil {
		t.Fatal("request headers did not receive an ExtProc headers response")
	}
}

func exchangeAgentgatewayGenericJSONMessage(t *testing.T, stream extprocv3.ExternalProcessor_ProcessClient, request *extprocv3.ProcessingRequest) *extprocv3.ProcessingResponse {
	t.Helper()
	if err := stream.Send(request); err != nil {
		t.Fatalf("send ExtProc message: %v", err)
	}
	response, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive ExtProc response: %v", err)
	}
	return response
}

func assertAgentgatewayGenericJSONMetadata(t *testing.T, response *extprocv3.ProcessingResponse, stage extproc.ProcessingStage, action extproc.Action) {
	t.Helper()
	metadata, found, err := envoyContractMetadata(response.GetDynamicMetadata())
	if err != nil {
		t.Fatalf("decode safe metadata: %v", err)
	}
	if !found {
		t.Fatal("safe metadata is missing")
	}
	metadataWire := response.GetDynamicMetadata().String()
	if strings.Contains(metadataWire, "person@example.test") || strings.Contains(metadataWire, "blocked-secret") || strings.Contains(metadataWire, "audit-only") {
		t.Fatalf("safe metadata leaked inspected content: %s", metadataWire)
	}
	if metadata.Adapter != "agentgateway" || metadata.Stage != stage || metadata.Action != action || metadata.PolicyID != agentgatewayGenericJSONPolicyID || metadata.PolicyVersion != 6 {
		t.Fatalf("safe metadata = %+v", metadata)
	}
}
