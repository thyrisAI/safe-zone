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

const agentgatewayAnthropicPolicyID = "agentgateway-anthropic-messages"

type agentgatewayAnthropicInspector func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error)

func (fn agentgatewayAnthropicInspector) Inspect(ctx context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
	return fn(ctx, input)
}

type agentgatewayAnthropicPolicyCache struct{}

func (agentgatewayAnthropicPolicyCache) Ready() bool { return true }

func (agentgatewayAnthropicPolicyCache) Get(policyID string) (policy.CompiledSnapshot, bool) {
	if policyID != agentgatewayAnthropicPolicyID {
		return policy.CompiledSnapshot{}, false
	}
	return policy.CompiledSnapshot{
		PolicyID: agentgatewayAnthropicPolicyID,
		Version:  3,
		Definition: policy.PolicyDefinition{
			Request: policy.RequestPolicy{PII: policy.ActionMask, Secret: policy.ActionBlock},
			Response: policy.ResponsePolicy{
				Enabled: true, PII: policy.ActionMask, Secret: policy.ActionBlock,
			},
			FailurePolicy: policy.FailurePolicy{Request: policy.FailureModeClosed, Response: policy.FailureModeClosed},
		},
	}, true
}

func TestAgentgatewayAnthropicMessagesCompatibility(t *testing.T) {
	processor, err := extproc.NewOpenAIRequestProcessor(agentgatewayAnthropicInspector(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		switch {
		case strings.Contains(input.Text, "blocked-secret"):
			return guardrails.InspectResult{Action: guardrails.RuleActionBlock, DetectionCount: 1, Categories: []string{"SECRET"}}, nil
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
	server, err := NewServerWithSettings(processor, agentgatewayAnthropicPolicyCache{}, nil, ServerSettings{AdapterName: "agentgateway"})
	if err != nil {
		t.Fatalf("NewServerWithSettings() error = %v", err)
	}
	client := newExternalProcessorTestClientForServer(t, server)

	t.Run("masks Messages request and response without Anthropic version header", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayAnthropicHeaders(t, stream)

		requestBody := []byte(`{"model":"claude-test","max_tokens":256,"system":[{"type":"text","text":"contact person@example.test"}],"messages":[{"role":"user","content":[{"type":"text","text":"email person@example.test"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]},{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"email":"person@example.test"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"found person@example.test"}]}]}`)
		requestResponse := exchangeAgentgatewayAnthropicMessage(t, stream, requestBodyForAdapterTest(requestBody, true))
		requestMutation := requestResponse.GetRequestBody().GetResponse().GetBodyMutation().GetBody()
		if bytes.Contains(requestMutation, []byte("person@example.test")) || bytes.Count(requestMutation, []byte("[MASKED]")) != 4 || !bytes.Contains(requestMutation, []byte(`"data":"AAAA"`)) {
			t.Fatalf("request mutation = %s", requestMutation)
		}
		assertAgentgatewayAnthropicMetadata(t, requestResponse, extproc.StageRequest, extproc.ActionMask)

		exchangeAgentgatewayAnthropicMessage(t, stream, responseHeadersForAdapterTest(false))
		responseBody := []byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"thinking","thinking":"opaque","signature":"sig"},{"type":"text","text":"reply to person@example.test"},{"type":"tool_use","id":"toolu_2","name":"lookup","input":{"email":"person@example.test"}}],"stop_reason":"tool_use"}`)
		responseResponse := exchangeAgentgatewayAnthropicMessage(t, stream, responseBodyForAdapterTest(responseBody, true))
		responseMutation := responseResponse.GetResponseBody().GetResponse().GetBodyMutation().GetBody()
		if bytes.Contains(responseMutation, []byte("person@example.test")) || bytes.Count(responseMutation, []byte("[MASKED]")) != 2 || !bytes.Contains(responseMutation, []byte(`"signature":"sig"`)) {
			t.Fatalf("response mutation = %s", responseMutation)
		}
		assertAgentgatewayAnthropicMetadata(t, responseResponse, extproc.StageResponse, extproc.ActionMask)
		if err := stream.CloseSend(); err != nil {
			t.Fatalf("CloseSend() error = %v", err)
		}
	})

	t.Run("blocks tool input before upstream", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayAnthropicHeaders(t, stream)
		body := []byte(`{"model":"claude-test","max_tokens":256,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"token":"blocked-secret"}}]}]}`)
		response := exchangeAgentgatewayAnthropicMessage(t, stream, requestBodyForAdapterTest(body, true))
		if response.GetImmediateResponse() == nil {
			t.Fatal("blocked Anthropic tool input did not produce an immediate response")
		}
		if bytes.Contains(response.GetImmediateResponse().GetBody(), []byte("blocked-secret")) {
			t.Fatalf("immediate response leaked inspected content: %s", response.GetImmediateResponse().GetBody())
		}
		assertAgentgatewayAnthropicMetadata(t, response, extproc.StageRequest, extproc.ActionBlock)
	})
}

func sendAgentgatewayAnthropicHeaders(t *testing.T, stream extprocv3.ExternalProcessor_ProcessClient) {
	t.Helper()
	headers := requestHeadersMessage("RID-agentgateway-anthropic", "agentgateway-anthropic-request-1", agentgatewayAnthropicPolicyID)
	headers.GetRequestHeaders().Headers.Headers = append(headers.GetRequestHeaders().Headers.Headers,
		&corev3.HeaderValue{Key: "content-type", RawValue: []byte("application/json")},
		&corev3.HeaderValue{Key: ":path", RawValue: []byte("/v1/messages")},
		&corev3.HeaderValue{Key: "x-tsz-gateway", RawValue: []byte("agentgateway-proxy")},
		&corev3.HeaderValue{Key: "x-tsz-route", RawValue: []byte("anthropic-messages")},
	)
	response := exchangeAgentgatewayAnthropicMessage(t, stream, headers)
	if response.GetRequestHeaders() == nil {
		t.Fatal("request headers did not receive an ExtProc headers response")
	}
}

func exchangeAgentgatewayAnthropicMessage(t *testing.T, stream extprocv3.ExternalProcessor_ProcessClient, request *extprocv3.ProcessingRequest) *extprocv3.ProcessingResponse {
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

func assertAgentgatewayAnthropicMetadata(t *testing.T, response *extprocv3.ProcessingResponse, stage extproc.ProcessingStage, action extproc.Action) {
	t.Helper()
	metadata, found, err := envoyContractMetadata(response.GetDynamicMetadata())
	if err != nil {
		t.Fatalf("decode safe metadata: %v", err)
	}
	if !found {
		t.Fatal("safe metadata is missing")
	}
	if metadata.Adapter != "agentgateway" || metadata.Stage != stage || metadata.Action != action || metadata.PolicyID != agentgatewayAnthropicPolicyID || metadata.PolicyVersion != 3 {
		t.Fatalf("safe metadata = %+v", metadata)
	}
}
