package extproc

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

func TestA2ARequestExtractsAndMutatesMessageParts(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":"req-1","method":"tasks/send","params":{"id":"task-1","message":{"role":"user","parts":[{"type":"text","text":"secret prompt","metadata":{"kept":true}},{"type":"data","data":{"email":"secret@example.com"}},{"type":"file","file":{"name":"image.png","mimeType":"image/png","bytes":"AAAA"}}]}}}`)
	payload, err := ParseA2ARequest("application/json", body)
	if err != nil {
		t.Fatalf("ParseA2ARequest() error = %v", err)
	}
	if payload.Provider != a2aProvider || len(payload.Contents) != 2 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Contents[0].JSONPath != ".params.message.parts[0].text" || payload.Contents[1].JSONPath != ".params.message.parts[1].data" {
		t.Fatalf("contents = %+v", payload.Contents)
	}
	mutated, err := payload.Mutate([]ProviderContentMutation{{ID: 0, Content: "[MASKED]"}, {ID: 1, Content: `{"email":"[MASKED]"}`}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	if strings.Contains(string(mutated), "secret") || !strings.Contains(string(mutated), `"bytes":"AAAA"`) || !strings.Contains(string(mutated), `"id":"req-1"`) || !strings.Contains(string(mutated), `"kept":true`) {
		t.Fatalf("mutated payload = %s", mutated)
	}
}

func TestA2AResponseExtractsMessagesHistoryAndArtifacts(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":"req-1","result":{"id":"task-1","message":{"role":"assistant","parts":[{"type":"text","text":"direct"}]},"status":{"state":"completed","message":{"role":"agent","parts":[{"kind":"text","text":"status"}]}},"history":[{"role":"user","parts":[{"kind":"data","data":{"input":"history"}}]}],"artifacts":[{"artifactId":"artifact-1","parts":[{"kind":"text","text":"artifact"},{"kind":"file","file":{"uri":"https://example.test/result.png"}}]}]}}`)
	payload, err := ParseA2AResponse("application/json", body, "message/send")
	if err != nil {
		t.Fatalf("ParseA2AResponse() error = %v", err)
	}
	wantPaths := []string{
		".result.message.parts[0].text",
		".result.status.message.parts[0].text",
		".result.history[0].parts[0].data",
		".result.artifacts[0].parts[0].text",
	}
	if len(payload.Contents) != len(wantPaths) {
		t.Fatalf("contents = %+v", payload.Contents)
	}
	for index, path := range wantPaths {
		if payload.Contents[index].JSONPath != path {
			t.Fatalf("content[%d] = %+v, want %s", index, payload.Contents[index], path)
		}
	}
}

func TestA2AAdapterRejectsUnsafeOrUnsupportedShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "not A2A", body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{}}`, want: ErrUnsupportedProviderPayload},
		{name: "streaming", body: `{"jsonrpc":"2.0","id":1,"method":"message/stream","params":{"message":{"role":"user","parts":[{"kind":"text","text":"secret"}]}}}`, want: ErrUnsupportedProviderContent},
		{name: "unknown part", body: `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"role":"user","parts":[{"kind":"future","text":"bypass"}]}}}`, want: ErrUnsupportedProviderContent},
		{name: "ambiguous part", body: `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"role":"user","parts":[{"kind":"text","text":"safe","data":{"value":"bypass"}}]}}}`, want: ErrUnsupportedProviderContent},
		{name: "invalid role", body: `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"role":"agent","parts":[{"kind":"text","text":"bypass"}]}}}`, want: ErrUnsupportedProviderContent},
		{name: "duplicate structured key", body: `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"role":"user","parts":[{"kind":"data","data":{"value":"safe","value":"bypass"}}]}}}`, want: ErrUnsupportedProviderContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseA2ARequest("application/json", []byte(test.body))
			if !errors.Is(err, test.want) || strings.Contains(err.Error(), "bypass") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestA2AProcessorAppliesPinnedPoliciesBeforeMCPFallback(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if strings.Contains(input.Text, "block-me") {
			return guardrails.InspectResult{Action: guardrails.RuleActionBlock, DetectionCount: 1, Categories: []string{"SECRET"}}, nil
		}
		if strings.Contains(input.Text, "secret") {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: strings.ReplaceAll(input.Text, "secret", "[MASKED]"), DetectionCount: 1, Categories: []string{"PII"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &policy.CompiledSnapshot{PolicyID: "a2a", Version: 5, Definition: policy.PolicyDefinition{Request: requestPolicyDefinition().Request, Response: responsePolicyDefinition().Response}}
	requestBody := []byte(`{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"role":"user","parts":[{"type":"text","text":"secret input"}]}}}`)
	requestResult, err := processor.Process(context.Background(), ProcessingRequest{Stage: StageRequest, ContentType: "application/json", Body: requestBody, PolicyID: "a2a", PolicyVersion: 5, PolicySnapshot: snapshot})
	if err != nil || requestResult.Action != ActionMask || requestResult.Metadata.Adapter != a2aProvider || strings.Contains(string(requestResult.Body), "secret") || requestResult.HeaderMutations["content-length"] != strconv.Itoa(len(requestResult.Body)) {
		t.Fatalf("request result = %+v body=%s error=%v", requestResult, requestResult.Body, err)
	}
	responseBody := []byte(`{"jsonrpc":"2.0","id":1,"result":{"message":{"role":"assistant","parts":[{"type":"text","text":"block-me output"}]}}}`)
	responseResult, err := processor.Process(context.Background(), ProcessingRequest{Stage: StageResponse, RPCMethod: "tasks/send", ContentType: "application/json", Body: responseBody, PolicyID: "a2a", PolicyVersion: 5, PolicySnapshot: snapshot})
	if err != nil || responseResult.Action != ActionBlock || responseResult.ImmediateStatus != 403 || responseResult.Metadata.Adapter != a2aProvider {
		t.Fatalf("response result = %+v error=%v", responseResult, err)
	}
}

func TestA2ATaskManagementAndErrorsPassUnchanged(t *testing.T) {
	payload, err := ParseA2ARequest("application/json", []byte(`{"jsonrpc":"2.0","id":2,"method":"tasks/get","params":{"id":"task-1"}}`))
	if err != nil || len(payload.Contents) != 0 {
		t.Fatalf("task request payload = %+v, error = %v", payload, err)
	}
	payload, err = ParseA2AResponse("application/json", []byte(`{"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"invalid task"}}`), "tasks/get")
	if err != nil || len(payload.Contents) != 0 {
		t.Fatalf("error response payload = %+v, error = %v", payload, err)
	}
}

func TestJSONRPCMethodFromMessageSupportsA2AContentType(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"message/send","params":{}}`)
	if got := JSONRPCMethodFromMessage("application/a2a+json; charset=utf-8", body); got != "message/send" {
		t.Fatalf("JSONRPCMethodFromMessage() = %q", got)
	}
}

func TestA2ADetectionRetainsMalformedCoveredMessages(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"id":2,"method":"message/send","params":{}}`)
	if !isA2AMessage("application/json", body) {
		t.Fatal("malformed covered A2A message fell through protocol detection")
	}
	_, err := ParseA2ARequest("application/json", body)
	if !errors.Is(err, ErrUnsupportedProviderContent) {
		t.Fatalf("ParseA2ARequest() error = %v, want %v", err, ErrUnsupportedProviderContent)
	}
}
