package extproc

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

func TestEmbeddingsExtractionAndMutation(t *testing.T) {
	for _, input := range []string{`"hello secret"`, `["hello secret", "safe", "İstanbul secret"]`} {
		body := `{ "model": "custom-model", "input": ` + input + `, "dimensions": 256, "encoding_format": "base64", "unknown": {"keep":1} }`
		payload, err := ParseEmbeddingsRequest("application/json; charset=utf-8", []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		unchanged, err := payload.Mutate(nil)
		if err != nil || string(unchanged) != body {
			t.Fatalf("unchanged=%s error=%v", unchanged, err)
		}
		var mutations []ProviderContentMutation
		for _, content := range payload.Contents {
			if strings.Contains(content.Content, "secret") {
				mutations = append(mutations, ProviderContentMutation{ID: content.ID, Content: strings.ReplaceAll(content.Content, "secret", `masked "value"`)})
			}
		}
		mutated, err := payload.Mutate(mutations)
		want := strings.ReplaceAll(body, "secret", `masked \"value\"`)
		if err != nil || string(mutated) != want || !json.Valid(mutated) {
			t.Fatalf("mutation=%s want=%s error=%v", mutated, want, err)
		}
	}
}

func TestEmbeddingsRejectUnsupportedInputs(t *testing.T) {
	for _, body := range []string{`{}`, `{"input":null}`, `{"input":""}`, `{"input":[]}`, `{"input":["safe",""]}`, `{"input":[1,2]}`, `{"input":[[1,2]]}`, `{"input":["safe",1]}`, `{"input":[{"text":"private-value"}]}`, `{"input":true}`, `{"input":"private-value","input":"safe"}`, `{"input":`, `[]`} {
		t.Run(body, func(t *testing.T) {
			_, err := ParseEmbeddingsRequest("application/json", []byte(body))
			if err == nil || strings.Contains(err.Error(), "private-value") {
				t.Fatalf("error=%v", err)
			}
		})
	}
	if _, err := ParseEmbeddingsRequest("text/plain", []byte(`{"input":"safe"}`)); !errors.Is(err, ErrUnsupportedProviderContent) {
		t.Fatalf("content type error=%v", err)
	}
}

func TestEmbeddingsUsesCompiledPolicyForEveryInput(t *testing.T) {
	processor := compiledPolicyProcessor(t)
	for _, tc := range []struct {
		name, input string
		action      Action
	}{
		{"safe", `"ordinary text"`, ActionAllow},
		{"pii", `["alice@example.com","safe","bob@example.com"]`, ActionMask},
		{"custom pattern", `"customer-123"`, ActionMask},
		{"secret overrides mask", `["alice@example.com","secret-42"]`, ActionBlock},
		{"audit", `"injection-marker"`, ActionAuditOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"custom","input":` + tc.input + `,"dimensions":32}`)
			snapshot := compiledSnapshot("embedding-policy", 7, policy.RequestPolicy{
				PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly,
				CompiledRules: policy.CompiledRequestRules{CustomPatterns: []policy.CompiledPattern{
					{ID: "customer", Name: "CUSTOMER", Category: "PII", Regex: `customer-[0-9]+`, Action: policy.ActionMask},
					{ID: "secret", Name: "SECRET", Category: "SECRET", Regex: `secret-[0-9]+`, Action: policy.ActionBlock},
					{ID: "injection", Name: "INJECTION", Category: "PROMPT_INJECTION", Regex: `injection-marker`, Action: policy.ActionAuditOnly},
				}},
			})
			result, err := processor.Process(context.Background(), ProcessingRequest{Stage: StageRequest, RequestPath: "/v1/embeddings?test=1", ContentType: "application/json", Body: body, PolicyID: "embedding-policy", PolicyVersion: 7, PolicySnapshot: snapshot})
			if err != nil || result.Action != tc.action {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if result.Metadata.Adapter != embeddingsProvider || result.Metadata.PolicyVersion != 7 {
				t.Fatalf("metadata=%+v", result.Metadata)
			}
			if tc.action == ActionMask {
				if result.Body == nil || !json.Valid(result.Body) || containsAny(string(result.Body), "alice@example.com", "bob@example.com", "customer-123") {
					t.Fatalf("masked=%s", result.Body)
				}
				if result.HeaderMutations["content-length"] != strconv.Itoa(len(result.Body)) {
					t.Fatal("incorrect content length")
				}
				if tc.name == "pii" && result.DetectionCount != 2 {
					t.Fatalf("detections=%d", result.DetectionCount)
				}
			} else if result.Body != nil || result.HeaderMutations != nil {
				t.Fatalf("unexpected mutation: %+v", result)
			}
			metadata, _ := json.Marshal(result.Metadata)
			if containsAny(string(metadata), "alice@example.com", "secret-42", "customer-123", "injection-marker") {
				t.Fatalf("unsafe metadata=%s", metadata)
			}
		})
	}
}

func TestEmbeddingsDispatchAndFailure(t *testing.T) {
	var inspected []string
	detectorErr := errors.New("detector unavailable")
	processor, _ := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, in guardrails.InspectInput) (guardrails.InspectResult, error) {
		inspected = append(inspected, in.Text)
		if in.Text == "fail" {
			return guardrails.InspectResult{}, detectorErr
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow}, nil
	}))
	snapshot := &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: requestPolicyDefinition()}
	for _, path := range []string{"/v1/embeddings", "/embeddings?x=1", "/responses"} {
		result, err := processor.Process(context.Background(), ProcessingRequest{Stage: StageRequest, Headers: map[string][]string{":path": {path}}, ContentType: "application/json", Body: []byte(`{"input":"safe"}`), PolicySnapshot: snapshot})
		want := embeddingsProvider
		if path == "/responses" {
			want = "openai_responses"
		}
		if err != nil || result.Metadata.Adapter != want {
			t.Fatalf("path=%s result=%+v err=%v", path, result, err)
		}
	}
	if !reflect.DeepEqual(inspected, []string{"safe", "safe", "safe"}) {
		t.Fatalf("inspected=%v", inspected)
	}
	for _, body := range []string{`{"input":["safe",123]}`, `{"messages":[],"input":[123]}`} {
		inspected = nil
		_, err := processor.Process(context.Background(), ProcessingRequest{Stage: StageRequest, RequestPath: "/v1/embeddings", ContentType: "application/json", Body: []byte(body), PolicySnapshot: snapshot})
		if err == nil || len(inspected) != 0 {
			t.Fatalf("invalid payload inspected=%v err=%v", inspected, err)
		}
	}
	_, err := processor.Process(context.Background(), ProcessingRequest{Stage: StageRequest, RequestPath: "/v1/embeddings", ContentType: "application/json", Body: []byte(`{"input":"fail"}`), PolicySnapshot: snapshot})
	if !errors.Is(err, detectorErr) {
		t.Fatalf("detector error=%v", err)
	}
}

func TestEmbeddingsResponseIsInputOnly(t *testing.T) {
	processor, _ := NewOpenAIRequestProcessor(inspectFunc(func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error) {
		t.Fatal("response must not be inspected")
		return guardrails.InspectResult{}, nil
	}))
	for _, body := range []string{`{"object":"list","data":[{"object":"embedding","embedding":[0.1,-0.2],"index":0}],"usage":{"total_tokens":2}}`, `{"object":"list","data":[{"embedding":"AAAAAA=="}]}`, `{"error":{"message":"provider error"}}`} {
		result, err := processor.Process(context.Background(), ProcessingRequest{Stage: StageResponse, RequestPath: "/v1/embeddings", ContentType: "application/json", Body: []byte(body), PolicySnapshot: &policy.CompiledSnapshot{Definition: responsePolicyDefinition()}})
		if err != nil || result.Action != ActionAllow || result.Body != nil || result.HeaderMutations != nil || result.Metadata.Adapter != embeddingsProvider {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
}
