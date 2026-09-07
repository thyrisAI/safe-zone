package extproc

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

func TestMCPRequestExtractsAndMutatesPromptAndToolArguments(t *testing.T) {
	tests := []struct {
		name, body, path string
	}{
		{"prompt", `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"review","arguments":{"code":"secret code","language":"go"}},"extra":true}`, ".params.arguments"},
		{"tool", `{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"lookup","arguments":{"customer":{"email":"secret@example.com"},"limit":2}},"extra":true}`, ".params.arguments"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, err := ParseMCPRequest("application/json; charset=utf-8", []byte(test.body))
			if err != nil || payload.Provider != mcpProvider || len(payload.Contents) != 1 || payload.Contents[0].JSONPath != test.path || !payload.Contents[0].rawJSON {
				t.Fatalf("payload=%+v error=%v", payload, err)
			}
			mutatedContent := strings.ReplaceAll(payload.Contents[0].Content, "secret", "[MASKED]")
			mutated, err := payload.Mutate([]ProviderContentMutation{{ID: 0, Content: mutatedContent}})
			if err != nil || !json.Valid(mutated) || strings.Contains(string(mutated), "secret") || !strings.Contains(string(mutated), `"extra":true`) {
				t.Fatalf("mutation=%s error=%v", mutated, err)
			}
		})
	}
}

func TestMCPResponseExtractsPromptTextAndResources(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"result":{"description":"kept","messages":[{"role":"user","content":{"type":"text","text":"secret prompt"}},{"role":"assistant","content":{"type":"resource","resource":{"uri":"file:///safe.txt","mimeType":"text/plain","text":"secret resource"}}},{"role":"user","content":{"type":"image","mimeType":"image/png","data":"AAAA"}},{"role":"assistant","content":{"type":"audio","mimeType":"audio/wav","data":"BBBB"}}]}}`)
	payload, err := ParseMCPResponse("application/json", body, "prompts/get")
	if err != nil || len(payload.Contents) != 2 {
		t.Fatalf("payload=%+v error=%v", payload, err)
	}
	wantPaths := []string{".result.messages[0].content.text", ".result.messages[1].content.resource.text"}
	mutations := make([]ProviderContentMutation, len(wantPaths))
	for index, path := range wantPaths {
		if payload.Contents[index].JSONPath != path {
			t.Fatalf("content[%d]=%+v", index, payload.Contents[index])
		}
		mutations[index] = ProviderContentMutation{ID: index, Content: "[MASKED]"}
	}
	mutated, err := payload.Mutate(mutations)
	if err != nil || strings.Contains(string(mutated), "secret") || !strings.Contains(string(mutated), `"data":"AAAA"`) || !strings.Contains(string(mutated), `"data":"BBBB"`) {
		t.Fatalf("mutation=%s error=%v", mutated, err)
	}
}

func TestMCPResponseExtractsToolTextResourcesAndStructuredContent(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"secret result"},{"type":"resource","resource":{"uri":"file:///result.txt","text":"secret embedded"}},{"type":"resource","resource":{"uri":"file:///binary","blob":"QklOQVJZ"}},{"type":"image","data":"AAAA","mimeType":"image/png"},{"type":"audio","data":"BBBB","mimeType":"audio/wav"},{"type":"resource_link","uri":"file:///linked","name":"linked"}],"structuredContent":{"account":{"email":"secret@example.com"}},"isError":false}}`)
	payload, err := ParseMCPResponse("application/json", body, "tools/call")
	if err != nil || len(payload.Contents) != 3 || !payload.Contents[2].rawJSON {
		t.Fatalf("payload=%+v error=%v", payload, err)
	}
	mutations := []ProviderContentMutation{
		{ID: 0, Content: "[MASKED] result"},
		{ID: 1, Content: "[MASKED] embedded"},
		{ID: 2, Content: `{"account":{"email":"[MASKED]"}}`},
	}
	mutated, err := payload.Mutate(mutations)
	if err != nil || strings.Contains(string(mutated), "secret") || !strings.Contains(string(mutated), `"blob":"QklOQVJZ"`) || !strings.Contains(string(mutated), `"uri":"file:///linked"`) {
		t.Fatalf("mutation=%s error=%v", mutated, err)
	}
}

func TestMCPAdapterRejectsUnsafeOrAmbiguousShapes(t *testing.T) {
	requests := []string{
		`{"jsonrpc":"1.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":"secret-value"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"x","arguments":{"value":{"nested":"secret-value"}}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{"value":"safe"},"arguments":{"value":"secret-value"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{"nested":{"value":"safe","value":"secret-value"}}}}`,
	}
	for _, body := range requests {
		_, err := ParseMCPRequest("application/json", []byte(body))
		if err == nil || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("request error=%v for %s", err, body)
		}
	}
	responses := []string{
		`{"jsonrpc":"2.0","id":1,"result":{"messages":[],"content":[]}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"messages":[{"role":"user","content":{"type":"future","text":"secret-value"}}]}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"resource","resource":{"text":"safe","blob":"secret-value"}}]}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"value":"secret-value"}}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"content":[],"structuredContent":{"nested":{"value":"safe","value":"secret-value"}}}}`,
	}
	for _, body := range responses {
		method := "tools/call"
		if strings.Contains(body, `"messages"`) {
			method = "prompts/get"
		}
		_, err := ParseMCPResponse("application/json", []byte(body), method)
		if err == nil || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("response error=%v for %s", err, body)
		}
	}
}

func TestMCPProcessorAppliesPinnedPoliciesToRequestsAndResponses(t *testing.T) {
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
	snapshot := &policy.CompiledSnapshot{PolicyID: "mcp", Version: 8, Definition: policy.PolicyDefinition{Request: requestPolicyDefinition().Request, Response: responsePolicyDefinition().Response}}
	tests := []struct {
		stage ProcessingStage
		body  string
		want  Action
	}{
		{StageRequest, `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"review","arguments":{"code":"secret input"}}}`, ActionMask},
		{StageRequest, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"lookup","arguments":{"query":"block-me"}}}`, ActionBlock},
		{StageResponse, `{"jsonrpc":"2.0","id":1,"result":{"messages":[{"role":"user","content":{"type":"text","text":"secret output"}}]}}`, ActionMask},
		{StageResponse, `{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"block-me"}],"isError":true}}`, ActionBlock},
	}
	for _, test := range tests {
		method := ""
		if test.stage == StageResponse {
			if strings.Contains(test.body, `"messages"`) {
				method = "prompts/get"
			} else {
				method = "tools/call"
			}
		}
		result, processErr := processor.Process(context.Background(), ProcessingRequest{Stage: test.stage, RPCMethod: method, ContentType: "application/json", Body: []byte(test.body), PolicyID: "mcp", PolicyVersion: 8, PolicySnapshot: snapshot})
		if processErr != nil || result.Action != test.want || result.Metadata.Adapter != mcpProvider || result.Metadata.PolicyVersion != 8 {
			t.Fatalf("result=%+v error=%v", result, processErr)
		}
		if test.want == ActionMask {
			if result.Body == nil || strings.Contains(string(result.Body), "secret") || result.HeaderMutations["content-length"] != strconv.Itoa(len(result.Body)) {
				t.Fatalf("mask result=%+v body=%s", result, result.Body)
			}
		}
		if test.stage == StageResponse && test.want == ActionBlock && result.ImmediateStatus != 403 {
			t.Fatalf("blocked response=%+v", result)
		}
	}
}

func TestMCPUnrelatedMethodsAndProtocolErrorsPassUnchanged(t *testing.T) {
	processor, _ := NewOpenAIRequestProcessor(inspectFunc(func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error) {
		t.Fatal("unrelated MCP message must not be inspected")
		return guardrails.InspectResult{}, nil
	}))
	snapshot := &policy.CompiledSnapshot{PolicyID: "mcp", Version: 1, Definition: policy.PolicyDefinition{Request: requestPolicyDefinition().Request, Response: responsePolicyDefinition().Response}}
	for _, test := range []struct {
		stage ProcessingStage
		body  string
	}{
		{StageRequest, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`},
		{StageRequest, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`},
		{StageResponse, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`},
		{StageResponse, `{"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"Unknown tool"}}`},
	} {
		method := ""
		if test.stage == StageResponse {
			method = "initialize"
		}
		result, err := processor.Process(context.Background(), ProcessingRequest{Stage: test.stage, RPCMethod: method, ContentType: "application/json", Body: []byte(test.body), PolicySnapshot: snapshot})
		if err != nil || result.Action != ActionAllow || result.Body != nil || result.HeaderMutations != nil || result.Metadata.Adapter != mcpProvider {
			t.Fatalf("result=%+v error=%v", result, err)
		}
	}
}

func TestMCPDetectorErrorsPropagateWithoutPayloadLeakage(t *testing.T) {
	detectorErr := errors.New("detector unavailable")
	processor, _ := NewOpenAIRequestProcessor(inspectFunc(func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error) {
		return guardrails.InspectResult{}, detectorErr
	}))
	_, err := processor.Process(context.Background(), ProcessingRequest{Stage: StageRequest, ContentType: "application/json", Body: []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{"token":"private-value"}}}`), PolicySnapshot: &policy.CompiledSnapshot{Definition: requestPolicyDefinition()}})
	if !errors.Is(err, detectorErr) || strings.Contains(err.Error(), "private-value") {
		t.Fatalf("error=%v", err)
	}
}
