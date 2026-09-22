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

const agentgatewayMCPPolicyID = "agentgateway-mcp-jsonrpc"

type agentgatewayMCPInspector func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error)

func (fn agentgatewayMCPInspector) Inspect(ctx context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
	return fn(ctx, input)
}

type agentgatewayMCPPolicyCache struct{}

func (agentgatewayMCPPolicyCache) Ready() bool { return true }

func (agentgatewayMCPPolicyCache) Get(policyID string) (policy.CompiledSnapshot, bool) {
	if policyID != agentgatewayMCPPolicyID {
		return policy.CompiledSnapshot{}, false
	}
	return policy.CompiledSnapshot{
		PolicyID: agentgatewayMCPPolicyID,
		Version:  4,
		Definition: policy.PolicyDefinition{
			Request: policy.RequestPolicy{PII: policy.ActionMask, Secret: policy.ActionBlock},
			Response: policy.ResponsePolicy{
				Enabled: true, PII: policy.ActionMask, Secret: policy.ActionBlock,
			},
			FailurePolicy: policy.FailurePolicy{Request: policy.FailureModeClosed, Response: policy.FailureModeClosed},
		},
	}, true
}

func TestAgentgatewayMCPJSONRPCCompatibility(t *testing.T) {
	processor, err := extproc.NewOpenAIRequestProcessor(agentgatewayMCPInspector(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
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
	server, err := NewServerWithSettings(processor, agentgatewayMCPPolicyCache{}, nil, ServerSettings{AdapterName: "agentgateway"})
	if err != nil {
		t.Fatalf("NewServerWithSettings() error = %v", err)
	}
	client := newExternalProcessorTestClientForServer(t, server)

	t.Run("masks tool arguments and correlated result", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayMCPHeaders(t, stream)

		requestBody := []byte(`{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"lookup","arguments":{"email":"person@example.test"}}}`)
		requestResponse := exchangeAgentgatewayMCPMessage(t, stream, requestBodyForAdapterTest(requestBody, true))
		requestMutation := requestResponse.GetRequestBody().GetResponse().GetBodyMutation().GetBody()
		if bytes.Contains(requestMutation, []byte("person@example.test")) || !bytes.Contains(requestMutation, []byte(`"email":"[MASKED]"`)) || !bytes.Contains(requestMutation, []byte(`"name":"lookup"`)) {
			t.Fatalf("request mutation = %s", requestMutation)
		}
		assertAgentgatewayMCPMetadata(t, requestResponse, extproc.StageRequest, extproc.ActionMask)

		exchangeAgentgatewayMCPMessage(t, stream, responseHeadersForAdapterTest(false))
		responseBody := []byte(`{"jsonrpc":"2.0","id":"call-1","result":{"content":[{"type":"text","text":"contact person@example.test"},{"type":"resource","resource":{"uri":"file:///binary","blob":"QklOQVJZ"}}],"structuredContent":{"email":"person@example.test"},"isError":false}}`)
		responseResponse := exchangeAgentgatewayMCPMessage(t, stream, responseBodyForAdapterTest(responseBody, true))
		responseMutation := responseResponse.GetResponseBody().GetResponse().GetBodyMutation().GetBody()
		if bytes.Contains(responseMutation, []byte("person@example.test")) || bytes.Count(responseMutation, []byte("[MASKED]")) != 2 || !bytes.Contains(responseMutation, []byte(`"blob":"QklOQVJZ"`)) || !bytes.Contains(responseMutation, []byte(`"id":"call-1"`)) {
			t.Fatalf("response mutation = %s", responseMutation)
		}
		assertAgentgatewayMCPMetadata(t, responseResponse, extproc.StageResponse, extproc.ActionMask)
		if err := stream.CloseSend(); err != nil {
			t.Fatalf("CloseSend() error = %v", err)
		}
	})

	t.Run("blocks tool arguments before MCP server", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayMCPHeaders(t, stream)
		body := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"lookup","arguments":{"token":"blocked-secret"}}}`)
		response := exchangeAgentgatewayMCPMessage(t, stream, requestBodyForAdapterTest(body, true))
		if response.GetImmediateResponse() == nil {
			t.Fatal("blocked MCP tool arguments did not produce an immediate response")
		}
		if bytes.Contains(response.GetImmediateResponse().GetBody(), []byte("blocked-secret")) {
			t.Fatalf("immediate response leaked inspected content: %s", response.GetImmediateResponse().GetBody())
		}
		assertAgentgatewayMCPMetadata(t, response, extproc.StageRequest, extproc.ActionBlock)
	})

	t.Run("allows protocol messages without mutation", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayMCPHeaders(t, stream)
		body := []byte(`{"jsonrpc":"2.0","id":3,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
		response := exchangeAgentgatewayMCPMessage(t, stream, requestBodyForAdapterTest(body, true))
		if mutation := response.GetRequestBody().GetResponse().GetBodyMutation(); mutation != nil {
			t.Fatalf("protocol message was unexpectedly mutated: %v", mutation)
		}
		assertAgentgatewayMCPMetadata(t, response, extproc.StageRequest, extproc.ActionAllow)
	})

	t.Run("audits tool arguments without mutation", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayMCPHeaders(t, stream)
		body := []byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"lookup","arguments":{"note":"audit-only"}}}`)
		response := exchangeAgentgatewayMCPMessage(t, stream, requestBodyForAdapterTest(body, true))
		if mutation := response.GetRequestBody().GetResponse().GetBodyMutation(); mutation != nil {
			t.Fatalf("audit-only message was unexpectedly mutated: %v", mutation)
		}
		assertAgentgatewayMCPMetadata(t, response, extproc.StageRequest, extproc.ActionAuditOnly)
	})
}

func sendAgentgatewayMCPHeaders(t *testing.T, stream extprocv3.ExternalProcessor_ProcessClient) {
	t.Helper()
	headers := requestHeadersMessage("RID-agentgateway-mcp", "agentgateway-mcp-request-1", agentgatewayMCPPolicyID)
	headers.GetRequestHeaders().Headers.Headers = append(headers.GetRequestHeaders().Headers.Headers,
		&corev3.HeaderValue{Key: "content-type", RawValue: []byte("application/json")},
		&corev3.HeaderValue{Key: ":path", RawValue: []byte("/mcp/mcp")},
		&corev3.HeaderValue{Key: "x-tsz-gateway", RawValue: []byte("agentgateway-proxy")},
		&corev3.HeaderValue{Key: "x-tsz-route", RawValue: []byte("mcp-jsonrpc")},
	)
	response := exchangeAgentgatewayMCPMessage(t, stream, headers)
	if response.GetRequestHeaders() == nil {
		t.Fatal("request headers did not receive an ExtProc headers response")
	}
}

func exchangeAgentgatewayMCPMessage(t *testing.T, stream extprocv3.ExternalProcessor_ProcessClient, request *extprocv3.ProcessingRequest) *extprocv3.ProcessingResponse {
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

func assertAgentgatewayMCPMetadata(t *testing.T, response *extprocv3.ProcessingResponse, stage extproc.ProcessingStage, action extproc.Action) {
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
	if metadata.Adapter != "agentgateway" || metadata.Stage != stage || metadata.Action != action || metadata.PolicyID != agentgatewayMCPPolicyID || metadata.PolicyVersion != 4 {
		t.Fatalf("safe metadata = %+v", metadata)
	}
}
