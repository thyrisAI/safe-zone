package extproc

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

type inspectFunc func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error)

func (fn inspectFunc) Inspect(ctx context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
	return fn(ctx, input)
}

func TestOpenAIRequestProcessorMasksDeveloperSystemUserAndAssistantContentAndUpdatesLength(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if input.Text == "secret user value" || input.Text == "secret developer value" || input.Text == "secret system value" || input.Text == "secret assistant refusal" || input.Text == "secret assistant value" {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", DetectionCount: 1, Categories: []string{"PII"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{"model":"kept","unknown":{"x":1},"messages":[{"role":"developer","content":"secret developer value"},{"role":"system","content":"secret system value"},{"role":"user","content":"secret user value"},{"role":"assistant","content":"secret assistant value","refusal":"secret assistant refusal"},{"role":"user","content":"safe user value"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-mask", EnvoyReqID: "envoy-mask", Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 3, PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 3, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionMask || result.DetectionCount != 5 || string(result.Body) == string(body) {
		t.Fatalf("result = %+v", result)
	}
	want := `{"model":"kept","unknown":{"x":1},"messages":[{"role":"developer","content":"[MASKED]"},{"role":"system","content":"[MASKED]"},{"role":"user","content":"[MASKED]"},{"role":"assistant","content":"[MASKED]","refusal":"[MASKED]"},{"role":"user","content":"safe user value"}]}`
	if got := string(result.Body); got != want {
		t.Fatalf("mutated body = %s\nwant = %s", got, want)
	}
	if result.HeaderMutations["content-length"] != strconv.Itoa(len(result.Body)) {
		t.Fatalf("content-length mutation = %q, want %d", result.HeaderMutations["content-length"], len(result.Body))
	}
	if result.Metadata.PolicyID != "default" || result.Metadata.PolicyVersion != 3 || result.Metadata.Action != ActionMask {
		t.Fatalf("metadata = %+v", result.Metadata)
	}
}

func TestOpenAIRequestProcessorMasksMultimodalTextAndPreservesBinaryParts(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if strings.Contains(input.Text, "secret") {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", DetectionCount: 1, Categories: []string{"PII"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{ "messages" : [ { "role" : "user", "content" : [ { "type" : "text", "text" : "secret one" }, { "type" : "image_url", "image_url" : { "url" : "data:image/png;base64,AAAA" } }, { "type" : "text", "text" : "secret two" } ] } ] }`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionMask || result.DetectionCount != 2 || strings.Count(string(result.Body), `"[MASKED]"`) != 2 || !strings.Contains(string(result.Body), `"url" : "data:image/png;base64,AAAA"`) {
		t.Fatalf("multimodal result = %+v body=%s", result, result.Body)
	}
	if result.HeaderMutations["content-length"] != strconv.Itoa(len(result.Body)) {
		t.Fatalf("content-length = %q, want %d", result.HeaderMutations["content-length"], len(result.Body))
	}
}

func TestOpenAIRequestProcessorMasksStreamingWindow(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if input.Text == "secret@example.test" {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", DetectionCount: 1, Categories: []string{"PII"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	parser := &OpenAISSEParser{}
	events, err := parser.Feed([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"secret@example.test\"}}]}\n\ndata: [DONE]\n\n"))
	if err != nil {
		t.Fatalf("parse SSE: %v", err)
	}
	result, mutated, err := processor.ProcessSSEWindow(context.Background(), ProcessingRequest{RID: "rid", EnvoyReqID: "envoy", Stage: StageResponse, PolicyID: "default", PolicyVersion: 1, PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: policy.PolicyDefinition{Response: policy.ResponsePolicy{Enabled: true, PII: policy.ActionMask, Secret: policy.ActionMask, UnsafeContent: policy.ActionMask}}}}, events)
	if err != nil {
		t.Fatalf("ProcessSSEWindow() error = %v", err)
	}
	if result.Action != ActionMask || result.DetectionCount != 1 || !strings.Contains(string(mutated[0].Raw), "[MASKED]") || !mutated[1].Done {
		t.Fatalf("window result = %+v, events = %+v", result, mutated)
	}
}

func TestOpenAIRequestProcessorBlocksStreamingWindowWithoutReturningUnsafeEvents(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		return guardrails.InspectResult{Action: guardrails.RuleActionBlock, DetectionCount: 1, Categories: []string{"SECRET"}}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	parser := &OpenAISSEParser{}
	events, err := parser.Feed([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"unsafe streamed value\"}}]}\n\ndata: [DONE]\n\n"))
	if err != nil {
		t.Fatalf("parse SSE: %v", err)
	}
	result, mutated, err := processor.ProcessSSEWindow(context.Background(), ProcessingRequest{
		RID: "rid", EnvoyReqID: "envoy", Stage: StageResponse, PolicyID: "default", PolicyVersion: 1,
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: policy.PolicyDefinition{
			Response: policy.ResponsePolicy{Enabled: true, PII: policy.ActionBlock, Secret: policy.ActionBlock, UnsafeContent: policy.ActionBlock},
		}},
	}, events)
	if err != nil {
		t.Fatalf("ProcessSSEWindow() error = %v", err)
	}
	if result.Action != ActionBlock || result.ImmediateStatus != 403 || result.DetectionCount != 1 {
		t.Fatalf("window block result = %+v", result)
	}
	if len(mutated) != len(events) {
		t.Fatalf("mutated events = %d, want %d for adapter-owned terminal handling", len(mutated), len(events))
	}
}

func TestOpenAIRequestProcessorStreamingWindowUsesCrossEventContext(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if input.Text != "secret@example.test" {
			t.Fatalf("Inspect text = %q, want concatenated SSE deltas", input.Text)
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", DetectionCount: 1, Categories: []string{"PII"}}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	parser := &OpenAISSEParser{}
	events, err := parser.Feed([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"secret@\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"example.test\"}}]}\n\n"))
	if err != nil {
		t.Fatalf("parse SSE: %v", err)
	}
	_, mutated, err := processor.ProcessSSEWindow(context.Background(), ProcessingRequest{Stage: StageResponse, PolicySnapshot: &policy.CompiledSnapshot{Definition: policy.PolicyDefinition{Response: policy.ResponsePolicy{Enabled: true, PII: policy.ActionMask, Secret: policy.ActionMask, UnsafeContent: policy.ActionMask}}}}, events)
	if err != nil {
		t.Fatalf("ProcessSSEWindow() error = %v", err)
	}
	if !strings.Contains(string(mutated[0].Raw), "[MASKED]") || strings.Contains(string(mutated[1].Raw), "example.test") {
		t.Fatalf("cross-event mutation = %q %q", mutated[0].Raw, mutated[1].Raw)
	}
}

func TestOpenAIRequestProcessorUsesStrongestActionWithoutMutatingAuditOrBlock(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		switch input.Text {
		case "audit":
			return guardrails.InspectResult{Action: guardrails.RuleActionAuditOnly, SafeContent: "changed", DetectionCount: 1, Categories: []string{"PROMPT_INJECTION"}}, nil
		case "mask":
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", DetectionCount: 1, Categories: []string{"PII"}}, nil
		default:
			return guardrails.InspectResult{Action: guardrails.RuleActionBlock, SafeContent: "[BLOCKED]", DetectionCount: 1, Categories: []string{"SECRET"}}, nil
		}
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{"messages":[{"role":"user","content":"audit"},{"role":"user","content":"mask"},{"role":"user","content":"block"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageRequest, ContentType: "application/json", Body: body, PolicyID: "default", PolicyVersion: 1,
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionBlock || result.DetectionCount != 3 {
		t.Fatalf("strongest result = %+v", result)
	}
	if result.Body != nil || result.HeaderMutations != nil {
		t.Fatalf("BLOCK must not mutate upstream body/header: %+v", result)
	}
	if got := result.Metadata.Categories; len(got) != 3 || got[0] != "PII" || got[1] != "PROMPT_INJECTION" || got[2] != "SECRET" {
		t.Fatalf("metadata categories = %v", got)
	}
}

func TestOpenAIRequestProcessorLeavesAuditOnlyBodyUnchanged(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		return guardrails.InspectResult{Action: guardrails.RuleActionAuditOnly, SafeContent: "must not be used", DetectionCount: 1, Categories: []string{"PROMPT_INJECTION"}}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{"messages":[{"role":"user","content":"audit"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionAuditOnly || result.DetectionCount != 1 || result.Body != nil || result.HeaderMutations != nil {
		t.Fatalf("AUDIT_ONLY result = %+v", result)
	}
}

func TestOpenAIRequestProcessorForwardsSafeBodyByteForByte(t *testing.T) {
	processor := compiledPolicyProcessor(t)
	body := []byte(`{"model":"kept","messages":[{"role":"user","content":"ordinary request"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-safe", EnvoyReqID: "envoy-safe", Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 1, PolicySnapshot: compiledSnapshot("default", 1, policy.RequestPolicy{
			PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly,
			CompiledRules: policy.CompiledRequestRules{CustomPatterns: []policy.CompiledPattern{
				{ID: "email", Name: "EMAIL", Category: "PII", Regex: `[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}`, Action: policy.ActionMask},
			}},
		}),
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionAllow || result.Body != nil || result.HeaderMutations != nil {
		t.Fatalf("safe result = %+v", result)
	}
	if upstream := bodyForMockUpstream(body, result); string(upstream) != string(body) {
		t.Fatalf("safe upstream body = %q, want byte-for-byte %q", upstream, body)
	}
}

func TestOpenAIRequestProcessorMasksPIIForMockUpstreamWithConsistentContentLength(t *testing.T) {
	processor := compiledPolicyProcessor(t)
	body := []byte(`{"messages":[{"role":"user","content":"email alice@example.com"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-pii", Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 2, PolicySnapshot: compiledSnapshot("default", 2, policy.RequestPolicy{
			PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly,
			CompiledRules: policy.CompiledRequestRules{CustomPatterns: []policy.CompiledPattern{
				{ID: "email", Name: "EMAIL", Category: "PII", Regex: `[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}`, Action: policy.ActionMask},
			}},
		}),
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	upstream := bodyForMockUpstream(body, result)
	if result.Action != ActionMask || string(upstream) == string(body) || string(upstream) == "" {
		t.Fatalf("masked upstream body = %q, result = %+v", upstream, result)
	}
	if result.HeaderMutations["content-length"] != strconv.Itoa(len(upstream)) {
		t.Fatalf("content-length = %q, want %d", result.HeaderMutations["content-length"], len(upstream))
	}
}

func TestOpenAIRequestProcessorMasksBuiltinPIIWithoutCustomPolicyPattern(t *testing.T) {
	processor := compiledPolicyProcessor(t)
	body := []byte(`{"messages":[{"role":"user","content":"contact alice@example.com"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-builtin-pii", Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 2, PolicySnapshot: compiledSnapshot("default", 2, policy.RequestPolicy{
			PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly,
		}),
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionMask || result.DetectionCount != 1 || result.Body == nil || strings.Contains(string(result.Body), "alice@example.com") {
		t.Fatalf("built-in PII result = %+v body=%q", result, result.Body)
	}
}

func TestOpenAIRequestProcessorSecretBlockOverridesPIIAuditOnly(t *testing.T) {
	processor := compiledPolicyProcessor(t)
	body := []byte(`{"messages":[{"role":"user","content":"token secret-42"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-secret", Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 3, PolicySnapshot: compiledSnapshot("default", 3, policy.RequestPolicy{
			PII: policy.ActionAuditOnly, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly,
			CompiledRules: policy.CompiledRequestRules{CustomPatterns: []policy.CompiledPattern{
				{ID: "pii", Name: "PII", Category: "PII", Regex: `token`, Action: policy.ActionAuditOnly},
				{ID: "secret", Name: "SECRET", Category: "SECRET", Regex: `secret-[0-9]+`, Action: policy.ActionBlock},
			}},
		}),
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionBlock || result.DetectionCount != 2 || result.Body != nil {
		t.Fatalf("secret must block despite PII audit: %+v", result)
	}
}

func TestOpenAIRequestProcessorMutatesEveryMatchingRequestMessage(t *testing.T) {
	processor := compiledPolicyProcessor(t)
	body := []byte(`{"messages":[{"role":"system","content":"system@example.com"},{"role":"user","content":"first@example.com"},{"role":"assistant","content":"assistant@example.com"},{"role":"user","content":"second@example.com"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-multiple", Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 4, PolicySnapshot: compiledSnapshot("default", 4, policy.RequestPolicy{
			PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly,
			CompiledRules: policy.CompiledRequestRules{CustomPatterns: []policy.CompiledPattern{
				{ID: "email", Name: "EMAIL", Category: "PII", Regex: `[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}`, Action: policy.ActionMask},
			}},
		}),
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	mutated := string(result.Body)
	if result.Action != ActionMask || result.DetectionCount != 4 || containsAny(mutated, "system@example.com", "first@example.com", "assistant@example.com", "second@example.com") {
		t.Fatalf("multiple request mutation = %q, result = %+v", mutated, result)
	}
}

func TestOpenAIRequestProcessorMasksChatToolCallAndResult(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if strings.Contains(input.Text, "secret@example.com") {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: strings.ReplaceAll(input.Text, "secret@example.com", "[MASKED]"), DetectionCount: 1, Categories: []string{"PII"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{"messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"email\":\"secret@example.com\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"result secret@example.com"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionMask || result.DetectionCount != 2 || strings.Contains(string(result.Body), "secret@example.com") {
		t.Fatalf("tool request result = %+v body=%s", result, result.Body)
	}
}

func TestOpenAIRequestProcessorBlocksChatToolCallResponse(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if strings.Contains(input.Text, "secret") {
			return guardrails.InspectResult{Action: guardrails.RuleActionBlock, DetectionCount: 1, Categories: []string{"SECRET"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	result, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageResponse, ContentType: "application/json",
		Body:           []byte(`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"token\":\"secret\"}"}}]}}]}`),
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: responsePolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionBlock || result.ImmediateStatus != 403 || result.Body != nil {
		t.Fatalf("tool response result = %+v", result)
	}
}

func TestOpenAIRequestProcessorMasksAssistantResponseAndUpdatesLength(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if input.Text == "secret assistant value" {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "", DetectionCount: 1, Categories: []string{"PII"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{"id":"kept","choices":[{"message":{"role":"assistant","content":"secret assistant value"}},{"message":{"role":"assistant","content":"safe answer"}}],"usage":{"total_tokens":9}}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-response-mask", EnvoyReqID: "envoy-response-mask", Stage: StageResponse, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 4, PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 4, Definition: responsePolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionMask || result.DetectionCount != 1 || result.Metadata.Stage != StageResponse {
		t.Fatalf("result = %+v", result)
	}
	want := `{"id":"kept","choices":[{"message":{"role":"assistant","content":""}},{"message":{"role":"assistant","content":"safe answer"}}],"usage":{"total_tokens":9}}`
	if string(result.Body) != want || result.HeaderMutations["content-length"] != strconv.Itoa(len(result.Body)) {
		t.Fatalf("masked response = %q, headers = %+v", result.Body, result.HeaderMutations)
	}
}

func TestOpenAIRequestProcessorBlocksAssistantResponseWithForbiddenStatus(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		return guardrails.InspectResult{Action: guardrails.RuleActionBlock, SafeContent: "must not be used", DetectionCount: 1, Categories: []string{"SECRET"}}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-response-block", EnvoyReqID: "envoy-response-block", Stage: StageResponse, ContentType: "application/json",
		Body:     []byte(`{"choices":[{"message":{"role":"assistant","content":"secret response"}}]}`),
		PolicyID: "default", PolicyVersion: 5, PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 5, Definition: responsePolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionBlock || result.ImmediateStatus != 403 || result.Body != nil || result.HeaderMutations != nil || result.Metadata.Stage != StageResponse {
		t.Fatalf("blocked response result = %+v", result)
	}
}

func TestOpenAIRequestProcessorMasksResponsesAPIStringInput(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if input.Text == "contact alice@example.com" {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "contact [MASKED]", DetectionCount: 1, Categories: []string{"PII"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{"model":"gpt-test","input":"contact alice@example.com","unknown":{"keep":true}}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-responses", EnvoyReqID: "envoy-responses", Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 6, PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 6, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	want := `{"model":"gpt-test","input":"contact [MASKED]","unknown":{"keep":true}}`
	if result.Action != ActionMask || string(result.Body) != want || result.HeaderMutations["content-length"] != strconv.Itoa(len(result.Body)) {
		t.Fatalf("Responses API request result = %+v body=%s", result, result.Body)
	}
	if result.Metadata.Adapter != "openai_responses" || result.Metadata.PolicyVersion != 6 || result.Metadata.DetectionCount != 1 {
		t.Fatalf("Responses API metadata = %+v", result.Metadata)
	}
}

func TestOpenAIRequestProcessorMasksResponsesAPISystemAndAssistantContent(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if strings.Contains(input.Text, "secret") {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", DetectionCount: 1, Categories: []string{"SECRET"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{"instructions":"secret instructions","input":[{"role":"system","content":"secret system message"},{"role":"assistant","content":[{"type":"output_text","text":"secret assistant history"}]},{"role":"user","content":"safe"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	want := `{"instructions":"[MASKED]","input":[{"role":"system","content":"[MASKED]"},{"role":"assistant","content":[{"type":"output_text","text":"[MASKED]"}]},{"role":"user","content":"safe"}]}`
	if result.Action != ActionMask || result.DetectionCount != 3 || string(result.Body) != want {
		t.Fatalf("Responses API request history result = %+v body=%s", result, result.Body)
	}
}

func TestOpenAIRequestProcessorUsesStrongestActionAcrossResponsesAPIInputItems(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		switch input.Text {
		case "mask":
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", DetectionCount: 1, Categories: []string{"PII"}}, nil
		case "block":
			return guardrails.InspectResult{Action: guardrails.RuleActionBlock, DetectionCount: 1, Categories: []string{"SECRET"}}, nil
		default:
			return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
		}
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{"input":[{"role":"user","content":"mask"},{"role":"user","content":[{"type":"input_text","text":"block"}]}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionBlock || result.DetectionCount != 2 || result.Body != nil || result.HeaderMutations != nil {
		t.Fatalf("strongest Responses API request result = %+v", result)
	}
}

func TestOpenAIRequestProcessorScansResponsesFunctionCallAndOutput(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		switch {
		case strings.Contains(input.Text, "mask@example.com"):
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: strings.ReplaceAll(input.Text, "mask@example.com", "[MASKED]"), DetectionCount: 1, Categories: []string{"PII"}}, nil
		case strings.Contains(input.Text, "blocked-secret"):
			return guardrails.InspectResult{Action: guardrails.RuleActionBlock, DetectionCount: 1, Categories: []string{"SECRET"}}, nil
		default:
			return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
		}
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	requestResult, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageRequest, ContentType: "application/json",
		Body:           []byte(`{"input":[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"email\":\"mask@example.com\"}"},{"type":"function_call_output","call_id":"call_1","output":"safe result"}]}`),
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("request Process() error = %v", err)
	}
	if requestResult.Action != ActionMask || strings.Contains(string(requestResult.Body), "mask@example.com") {
		t.Fatalf("Responses tool request result = %+v body=%s", requestResult, requestResult.Body)
	}
	responseResult, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageResponse, ContentType: "application/json",
		Body:           []byte(`{"object":"response","output":[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"token\":\"blocked-secret\"}"}]}`),
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: responsePolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("response Process() error = %v", err)
	}
	if responseResult.Action != ActionBlock || responseResult.ImmediateStatus != 403 {
		t.Fatalf("Responses tool response result = %+v", responseResult)
	}
}

func TestOpenAIRequestProcessorMasksResponsesAPIOutputAndConvenienceText(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if input.Text == "secret@example.com" {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", DetectionCount: 1, Categories: []string{"PII"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{"id":"resp_1","object":"response","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"secret@example.com","annotations":[]}]}],"output_text":"secret@example.com","usage":{"total_tokens":3}}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-output", Stage: StageResponse, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 6, PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 6, Definition: responsePolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionMask || result.DetectionCount != 1 || result.Metadata.Adapter != "openai_responses" {
		t.Fatalf("Responses API response result = %+v", result)
	}
	if strings.Contains(string(result.Body), "secret@example.com") || strings.Count(string(result.Body), "[MASKED]") != 2 {
		t.Fatalf("masked Responses API response = %s", result.Body)
	}
	if result.HeaderMutations["content-length"] != strconv.Itoa(len(result.Body)) {
		t.Fatalf("content-length = %q, want %d", result.HeaderMutations["content-length"], len(result.Body))
	}
}

func TestOpenAIRequestProcessorBlocksResponsesAPIOutput(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error) {
		return guardrails.InspectResult{Action: guardrails.RuleActionBlock, DetectionCount: 1, Categories: []string{"SECRET"}}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	result, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageResponse, ContentType: "application/json",
		Body:           []byte(`{"object":"response","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"secret"}]}]}`),
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: responsePolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionBlock || result.ImmediateStatus != 403 || result.Body != nil || result.HeaderMutations != nil {
		t.Fatalf("blocked Responses API response = %+v", result)
	}
}

func TestRequestProcessorHandlesAnthropicMessages(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if strings.Contains(input.Text, "blocked") {
			return guardrails.InspectResult{Action: guardrails.RuleActionBlock, DetectionCount: 1, Categories: []string{"SECRET"}}, nil
		}
		if strings.Contains(input.Text, "secret") {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: strings.ReplaceAll(input.Text, "secret", "[MASKED]"), DetectionCount: 1, Categories: []string{"PII"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	requestResult, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageRequest, ContentType: "application/json", Headers: map[string][]string{"anthropic-version": {"2023-06-01"}},
		Body:           []byte(`{"system":"secret system","messages":[{"role":"user","content":[{"type":"text","text":"safe"},{"type":"image","source":{"type":"base64","data":"AAAA"}}]}]}`),
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("request Process() error = %v", err)
	}
	if requestResult.Action != ActionMask || requestResult.Metadata.Adapter != anthropicProvider || !strings.Contains(string(requestResult.Body), "[MASKED] system") || !strings.Contains(string(requestResult.Body), `"data":"AAAA"`) {
		t.Fatalf("Anthropic request result = %+v body=%s", requestResult, requestResult.Body)
	}
	responseResult, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageResponse, ContentType: "application/json",
		Body:           []byte(`{"type":"message","role":"assistant","content":[{"type":"text","text":"blocked output"}]}`),
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: responsePolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("response Process() error = %v", err)
	}
	if responseResult.Action != ActionBlock || responseResult.ImmediateStatus != 403 || responseResult.Metadata.Adapter != anthropicProvider {
		t.Fatalf("Anthropic response result = %+v", responseResult)
	}
}

func TestRequestProcessorHandlesGeminiGenerateContent(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if strings.Contains(input.Text, "secret") {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: strings.ReplaceAll(input.Text, "secret", "[MASKED]"), DetectionCount: 1, Categories: []string{"PII"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	requestResult, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageRequest, ContentType: "application/json",
		Body:           []byte(`{"contents":[{"role":"user","parts":[{"text":"secret prompt"},{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]}]}`),
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("request Process() error = %v", err)
	}
	if requestResult.Action != ActionMask || requestResult.Metadata.Adapter != geminiProvider || !strings.Contains(string(requestResult.Body), "[MASKED] prompt") || !strings.Contains(string(requestResult.Body), `"data":"AAAA"`) {
		t.Fatalf("Gemini request result = %+v body=%s", requestResult, requestResult.Body)
	}
	responseResult, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageResponse, ContentType: "application/json",
		Body:           []byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"secret output"}]}}]}`),
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: responsePolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("response Process() error = %v", err)
	}
	if responseResult.Action != ActionMask || responseResult.Metadata.Adapter != geminiProvider || !strings.Contains(string(responseResult.Body), "[MASKED] output") {
		t.Fatalf("Gemini response result = %+v body=%s", responseResult, responseResult.Body)
	}
}

func compiledPolicyProcessor(t *testing.T) *OpenAIRequestProcessor {
	t.Helper()
	service, err := guardrails.NewGuardrailService(&guardrails.Detector{})
	if err != nil {
		t.Fatalf("NewGuardrailService() error = %v", err)
	}
	processor, err := NewOpenAIRequestProcessor(service)
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	return processor
}

func compiledSnapshot(policyID string, version int, request policy.RequestPolicy) *policy.CompiledSnapshot {
	return &policy.CompiledSnapshot{PolicyID: policyID, Version: version, Definition: policy.PolicyDefinition{Request: request}}
}

// bodyForMockUpstream models Envoy's request-body forwarding decision: a nil
// mutation preserves the original bytes, otherwise the supplied mutation wins.
func bodyForMockUpstream(original []byte, result ProcessingResult) []byte {
	if result.Body == nil {
		return original
	}
	return result.Body
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if len(candidate) > 0 && len(value) >= len(candidate) {
			for index := 0; index+len(candidate) <= len(value); index++ {
				if value[index:index+len(candidate)] == candidate {
					return true
				}
			}
		}
	}
	return false
}

func requestPolicyDefinition() policy.PolicyDefinition {
	return policy.PolicyDefinition{Request: policy.RequestPolicy{
		PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly,
	}}
}

func responsePolicyDefinition() policy.PolicyDefinition {
	return policy.PolicyDefinition{Response: policy.ResponsePolicy{
		Enabled: true, PII: policy.ActionMask, Secret: policy.ActionBlock, UnsafeContent: policy.ActionAuditOnly,
	}}
}
