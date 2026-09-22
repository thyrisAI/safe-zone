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

const agentgatewayResponsesPolicyID = "agentgateway-responses"

type agentgatewayResponsesInspector func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error)

func (fn agentgatewayResponsesInspector) Inspect(ctx context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
	return fn(ctx, input)
}

type agentgatewayResponsesPolicyCache struct{}

func (agentgatewayResponsesPolicyCache) Ready() bool { return true }

func (agentgatewayResponsesPolicyCache) Get(policyID string) (policy.CompiledSnapshot, bool) {
	if policyID != agentgatewayResponsesPolicyID {
		return policy.CompiledSnapshot{}, false
	}
	return policy.CompiledSnapshot{
		PolicyID: agentgatewayResponsesPolicyID,
		Version:  2,
		Definition: policy.PolicyDefinition{
			Request: policy.RequestPolicy{PII: policy.ActionMask, Secret: policy.ActionBlock},
			Response: policy.ResponsePolicy{
				Enabled: true, PII: policy.ActionMask, Secret: policy.ActionBlock,
			},
			FailurePolicy: policy.FailurePolicy{Request: policy.FailureModeClosed, Response: policy.FailureModeClosed},
		},
	}, true
}

func TestAgentgatewayOpenAIResponsesCompatibility(t *testing.T) {
	processor, err := extproc.NewOpenAIRequestProcessor(agentgatewayResponsesInspector(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
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
	server, err := NewServerWithSettings(processor, agentgatewayResponsesPolicyCache{}, nil, ServerSettings{AdapterName: "agentgateway"})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	client := newExternalProcessorTestClientForServer(t, server)

	t.Run("masks request and response through shared ExtProc", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayResponsesHeaders(t, stream)

		requestBody := []byte(`{"model":"gpt-test","instructions":"safe","input":[{"role":"user","content":[{"type":"input_text","text":"contact person@example.test"},{"type":"input_image","image_url":"https://example.test/image.png"}]}]}`)
		requestResponse := exchangeAgentgatewayMessage(t, stream, requestBodyForAdapterTest(requestBody, true))
		requestMutation := requestResponse.GetRequestBody().GetResponse().GetBodyMutation().GetBody()
		if strings.Contains(string(requestMutation), "person@example.test") || !strings.Contains(string(requestMutation), "[MASKED]") || !strings.Contains(string(requestMutation), "image.png") {
			t.Fatalf("request mutation = %s", requestMutation)
		}
		assertAgentgatewayResponsesMetadata(t, requestResponse, extproc.StageRequest, extproc.ActionMask)

		exchangeAgentgatewayMessage(t, stream, responseHeadersForAdapterTest(false))
		responseBody := []byte(`{"id":"resp_1","object":"response","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"person@example.test","annotations":[]}]}],"output_text":"person@example.test"}`)
		responseResponse := exchangeAgentgatewayMessage(t, stream, responseBodyForAdapterTest(responseBody, true))
		responseMutation := responseResponse.GetResponseBody().GetResponse().GetBodyMutation().GetBody()
		if strings.Contains(string(responseMutation), "person@example.test") || strings.Count(string(responseMutation), "[MASKED]") != 2 {
			t.Fatalf("response mutation = %s", responseMutation)
		}
		assertAgentgatewayResponsesMetadata(t, responseResponse, extproc.StageResponse, extproc.ActionMask)
		if err := stream.CloseSend(); err != nil {
			t.Fatalf("CloseSend() error = %v", err)
		}
	})

	t.Run("blocks function arguments before upstream", func(t *testing.T) {
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		sendAgentgatewayResponsesHeaders(t, stream)
		body := []byte(`{"input":[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"token\":\"blocked-secret\"}"}]}`)
		response := exchangeAgentgatewayMessage(t, stream, requestBodyForAdapterTest(body, true))
		if response.GetImmediateResponse() == nil {
			t.Fatal("blocked Responses function arguments did not produce an immediate response")
		}
		wireBody := response.GetImmediateResponse().GetBody()
		if bytes.Contains(wireBody, []byte("blocked-secret")) {
			t.Fatalf("immediate response leaked inspected content: %s", wireBody)
		}
		assertAgentgatewayResponsesMetadata(t, response, extproc.StageRequest, extproc.ActionBlock)
	})
}

func sendAgentgatewayResponsesHeaders(t *testing.T, stream extprocv3.ExternalProcessor_ProcessClient) {
	t.Helper()
	headers := requestHeadersMessage("RID-agentgateway-responses", "agentgateway-request-1", agentgatewayResponsesPolicyID)
	headers.GetRequestHeaders().Headers.Headers = append(headers.GetRequestHeaders().Headers.Headers,
		&corev3.HeaderValue{Key: "content-type", RawValue: []byte("application/json")},
		&corev3.HeaderValue{Key: ":path", RawValue: []byte("/v1/responses")},
		&corev3.HeaderValue{Key: "x-tsz-gateway", RawValue: []byte("agentgateway-proxy")},
		&corev3.HeaderValue{Key: "x-tsz-route", RawValue: []byte("responses-api")},
	)
	response := exchangeAgentgatewayMessage(t, stream, headers)
	if response.GetRequestHeaders() == nil {
		t.Fatal("request headers did not receive an ExtProc headers response")
	}
}

func exchangeAgentgatewayMessage(t *testing.T, stream extprocv3.ExternalProcessor_ProcessClient, request *extprocv3.ProcessingRequest) *extprocv3.ProcessingResponse {
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

func assertAgentgatewayResponsesMetadata(t *testing.T, response *extprocv3.ProcessingResponse, stage extproc.ProcessingStage, action extproc.Action) {
	t.Helper()
	metadata, found, err := envoyContractMetadata(response.GetDynamicMetadata())
	if err != nil {
		t.Fatalf("decode safe metadata: %v", err)
	}
	if !found {
		t.Fatal("safe metadata is missing")
	}
	if metadata.Adapter != "agentgateway" || metadata.Stage != stage || metadata.Action != action || metadata.PolicyID != agentgatewayResponsesPolicyID || metadata.PolicyVersion != 2 {
		t.Fatalf("safe metadata = %+v", metadata)
	}
}
