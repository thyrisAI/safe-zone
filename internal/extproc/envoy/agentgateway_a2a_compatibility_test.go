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

const agentgatewayA2APolicyID = "agentgateway-a2a"

type agentgatewayA2AInspector func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error)

func (fn agentgatewayA2AInspector) Inspect(ctx context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
	return fn(ctx, input)
}

type agentgatewayA2APolicyCache struct{}

func (agentgatewayA2APolicyCache) Ready() bool { return true }

func (agentgatewayA2APolicyCache) Get(policyID string) (policy.CompiledSnapshot, bool) {
	if policyID != agentgatewayA2APolicyID {
		return policy.CompiledSnapshot{}, false
	}
	return policy.CompiledSnapshot{
		PolicyID: agentgatewayA2APolicyID,
		Version:  5,
		Definition: policy.PolicyDefinition{
			Request: policy.RequestPolicy{PII: policy.ActionMask, Secret: policy.ActionBlock},
			Response: policy.ResponsePolicy{
				Enabled: true, PII: policy.ActionMask, Secret: policy.ActionBlock,
			},
			FailurePolicy: policy.FailurePolicy{Request: policy.FailureModeClosed, Response: policy.FailureModeClosed},
		},
	}, true
}

func TestAgentgatewayA2ACompatibility(t *testing.T) {
	processor, err := extproc.NewOpenAIRequestProcessor(agentgatewayA2AInspector(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
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
	server, err := NewServerWithSettings(processor, agentgatewayA2APolicyCache{}, nil, ServerSettings{AdapterName: "agentgateway"})
	if err != nil {
		t.Fatalf("NewServerWithSettings() error = %v", err)
	}
	client := newExternalProcessorTestClientForServer(t, server)

	t.Run("masks message and correlated agent result", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayA2AHeaders(t, stream)

		requestBody := []byte(`{"jsonrpc":"2.0","id":"req-1","method":"tasks/send","params":{"id":"task-1","message":{"role":"user","parts":[{"type":"text","text":"contact person@example.test"},{"type":"data","data":{"email":"person@example.test"}},{"type":"file","file":{"mimeType":"image/png","bytes":"AAAA"}}]}}}`)
		requestResponse := exchangeAgentgatewayA2AMessage(t, stream, requestBodyForAdapterTest(requestBody, true))
		requestMutation := requestResponse.GetRequestBody().GetResponse().GetBodyMutation().GetBody()
		if bytes.Contains(requestMutation, []byte("person@example.test")) || bytes.Count(requestMutation, []byte("[MASKED]")) != 2 || !bytes.Contains(requestMutation, []byte(`"bytes":"AAAA"`)) || !bytes.Contains(requestMutation, []byte(`"method":"tasks/send"`)) {
			t.Fatalf("request mutation = %s", requestMutation)
		}
		assertAgentgatewayA2AMetadata(t, requestResponse, extproc.StageRequest, extproc.ActionMask)

		exchangeAgentgatewayA2AMessage(t, stream, responseHeadersForAdapterTest(false))
		responseBody := []byte(`{"jsonrpc":"2.0","id":"req-1","result":{"id":"task-1","message":{"role":"assistant","parts":[{"type":"text","text":"reply to person@example.test"}]},"artifacts":[{"artifactId":"artifact-1","parts":[{"kind":"data","data":{"email":"person@example.test"}},{"kind":"file","file":{"uri":"https://example.test/result.png"}}]}]}}`)
		responseResponse := exchangeAgentgatewayA2AMessage(t, stream, responseBodyForAdapterTest(responseBody, true))
		responseMutation := responseResponse.GetResponseBody().GetResponse().GetBodyMutation().GetBody()
		if bytes.Contains(responseMutation, []byte("person@example.test")) || bytes.Count(responseMutation, []byte("[MASKED]")) != 2 || !bytes.Contains(responseMutation, []byte(`"uri":"https://example.test/result.png"`)) || !bytes.Contains(responseMutation, []byte(`"id":"req-1"`)) {
			t.Fatalf("response mutation = %s", responseMutation)
		}
		assertAgentgatewayA2AMetadata(t, responseResponse, extproc.StageResponse, extproc.ActionMask)
		if err := stream.CloseSend(); err != nil {
			t.Fatalf("CloseSend() error = %v", err)
		}
	})

	t.Run("blocks message before agent backend", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayA2AHeaders(t, stream)
		body := []byte(`{"jsonrpc":"2.0","id":2,"method":"tasks/send","params":{"message":{"role":"user","parts":[{"type":"text","text":"blocked-secret"}]}}}`)
		response := exchangeAgentgatewayA2AMessage(t, stream, requestBodyForAdapterTest(body, true))
		if response.GetImmediateResponse() == nil {
			t.Fatal("blocked A2A message did not produce an immediate response")
		}
		if bytes.Contains(response.GetImmediateResponse().GetBody(), []byte("blocked-secret")) {
			t.Fatalf("immediate response leaked inspected content: %s", response.GetImmediateResponse().GetBody())
		}
		assertAgentgatewayA2AMetadata(t, response, extproc.StageRequest, extproc.ActionBlock)
	})

	t.Run("audits message without mutation", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayA2AHeaders(t, stream)
		body := []byte(`{"jsonrpc":"2.0","id":3,"method":"message/send","params":{"message":{"role":"user","parts":[{"kind":"text","text":"audit-only"}]}}}`)
		response := exchangeAgentgatewayA2AMessage(t, stream, requestBodyForAdapterTest(body, true))
		if mutation := response.GetRequestBody().GetResponse().GetBodyMutation(); mutation != nil {
			t.Fatalf("audit-only A2A message was unexpectedly mutated: %v", mutation)
		}
		assertAgentgatewayA2AMetadata(t, response, extproc.StageRequest, extproc.ActionAuditOnly)
	})
}

func sendAgentgatewayA2AHeaders(t *testing.T, stream extprocv3.ExternalProcessor_ProcessClient) {
	t.Helper()
	headers := requestHeadersMessage("RID-agentgateway-a2a", "agentgateway-a2a-request-1", agentgatewayA2APolicyID)
	headers.GetRequestHeaders().Headers.Headers = append(headers.GetRequestHeaders().Headers.Headers,
		&corev3.HeaderValue{Key: "content-type", RawValue: []byte("application/json")},
		&corev3.HeaderValue{Key: ":path", RawValue: []byte("/agents/support")},
		&corev3.HeaderValue{Key: "x-tsz-gateway", RawValue: []byte("agentgateway-proxy")},
		&corev3.HeaderValue{Key: "x-tsz-route", RawValue: []byte("a2a-support-agent")},
	)
	response := exchangeAgentgatewayA2AMessage(t, stream, headers)
	if response.GetRequestHeaders() == nil {
		t.Fatal("request headers did not receive an ExtProc headers response")
	}
}

func exchangeAgentgatewayA2AMessage(t *testing.T, stream extprocv3.ExternalProcessor_ProcessClient, request *extprocv3.ProcessingRequest) *extprocv3.ProcessingResponse {
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

func assertAgentgatewayA2AMetadata(t *testing.T, response *extprocv3.ProcessingResponse, stage extproc.ProcessingStage, action extproc.Action) {
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
	if metadata.Adapter != "agentgateway" || metadata.Stage != stage || metadata.Action != action || metadata.PolicyID != agentgatewayA2APolicyID || metadata.PolicyVersion != 5 {
		t.Fatalf("safe metadata = %+v", metadata)
	}
}
