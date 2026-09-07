package envoy

import (
	"context"
	"strings"
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	. "thyris-sz/internal/extproc"
	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

func TestMCPGRPCMasksToolArgumentsAndResults(t *testing.T) {
	service, err := guardrails.NewGuardrailService(&guardrails.Detector{})
	if err != nil {
		t.Fatal(err)
	}
	processor, err := NewOpenAIRequestProcessor(service)
	if err != nil {
		t.Fatal(err)
	}
	cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{PolicyID: "default", Version: 9, Definition: policy.PolicyDefinition{
		Request:       policy.RequestPolicy{PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly},
		Response:      policy.ResponsePolicy{Enabled: true, PII: policy.ActionMask, Secret: policy.ActionBlock, UnsafeContent: policy.ActionAuditOnly},
		FailurePolicy: policy.FailurePolicy{Request: policy.FailureModeClosed, Response: policy.FailureModeClosed},
	}}}
	stream, err := newExternalProcessorTestClient(t, processor, cache).Process(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.CloseSend()
	headers := requestHeadersMessage("rid-mcp", "envoy-mcp", "default")
	headers.GetRequestHeaders().Headers.Headers = append(headers.GetRequestHeaders().Headers.Headers,
		&corev3.HeaderValue{Key: ":path", RawValue: []byte("/mcp")},
		&corev3.HeaderValue{Key: "content-type", RawValue: []byte("application/json")})
	if err = stream.Send(headers); err != nil {
		t.Fatal(err)
	}
	if response, receiveErr := stream.Recv(); receiveErr != nil || response.GetImmediateResponse() != nil {
		t.Fatalf("headers response=%v error=%v", response, receiveErr)
	}
	requestBody := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lookup","arguments":{"email":"alice@example.com"}}}`)
	if err = stream.Send(requestBodyForAdapterTest(requestBody, true)); err != nil {
		t.Fatal(err)
	}
	response, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	requestMutation := response.GetRequestBody().GetResponse().GetBodyMutation().GetBody()
	if len(requestMutation) == 0 || strings.Contains(string(requestMutation), "alice@example.com") || !strings.Contains(string(requestMutation), `"name":"lookup"`) {
		t.Fatalf("request mutation=%s", requestMutation)
	}
	if err = stream.Send(responseHeadersForAdapterTest(false)); err != nil {
		t.Fatal(err)
	}
	if response, err = stream.Recv(); err != nil || response.GetImmediateResponse() != nil {
		t.Fatalf("response headers=%v error=%v", response, err)
	}
	responseBody := []byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"contact bob@example.com"}],"structuredContent":{"email":"carol@example.com"},"isError":false}}`)
	if err = stream.Send(responseBodyForAdapterTest(responseBody, true)); err != nil {
		t.Fatal(err)
	}
	response, err = stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	responseMutation := response.GetResponseBody().GetResponse().GetBodyMutation().GetBody()
	if len(responseMutation) == 0 || strings.Contains(string(responseMutation), "bob@example.com") || strings.Contains(string(responseMutation), "carol@example.com") || !strings.Contains(string(responseMutation), `"isError":false`) {
		t.Fatalf("response mutation=%s", responseMutation)
	}
}

func TestMCPGRPCMalformedToolPayloadFollowsFailClosed(t *testing.T) {
	service, err := guardrails.NewGuardrailService(&guardrails.Detector{})
	if err != nil {
		t.Fatal(err)
	}
	processor, err := NewOpenAIRequestProcessor(service)
	if err != nil {
		t.Fatal(err)
	}
	cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: policy.PolicyDefinition{
		Request:       policy.RequestPolicy{PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly},
		FailurePolicy: policy.FailurePolicy{Request: policy.FailureModeClosed},
	}}}
	stream, err := newExternalProcessorTestClient(t, processor, cache).Process(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	headers := requestHeadersMessage("rid-mcp", "envoy-mcp", "default")
	headers.GetRequestHeaders().Headers.Headers = append(headers.GetRequestHeaders().Headers.Headers,
		&corev3.HeaderValue{Key: "content-type", RawValue: []byte("application/json")})
	if err = stream.Send(headers); err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Recv(); err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(requestBodyForAdapterTest([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lookup","arguments":"uninspectable"}}`), true)); err != nil {
		t.Fatal(err)
	}
	response, err := stream.Recv()
	if err != nil || response.GetImmediateResponse() == nil {
		t.Fatalf("response=%v error=%v", response, err)
	}
}
