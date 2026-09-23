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

func TestGenericJSONInspectsAndMutatesCompleteDocument(t *testing.T) {
	body := []byte(`{ "customer" : {"email":"person@example.test"}, "active":true, "count":3, "tags":["one","two"] }`)
	payload, err := ParseGenericJSON("application/json; charset=utf-8", body, "request")
	if err != nil {
		t.Fatalf("ParseGenericJSON() error = %v", err)
	}
	if payload.Provider != genericJSONProvider || len(payload.Contents) != 1 || payload.Contents[0].JSONPath != "$" || payload.Contents[0].Content != string(body) {
		t.Fatalf("payload = %+v", payload)
	}
	masked := strings.ReplaceAll(string(body), "person@example.test", "[MASKED]")
	mutated, err := payload.Mutate([]ProviderContentMutation{{ID: 0, Content: masked}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	if string(mutated) != masked || !strings.Contains(string(mutated), `"active":true`) || !strings.Contains(string(mutated), `"tags":["one","two"]`) {
		t.Fatalf("mutated payload = %s", mutated)
	}
}

func TestGenericJSONSupportsArraysAndStructuredJSONMediaTypes(t *testing.T) {
	body := []byte(`[{"message":"first"},{"message":"second"}]`)
	payload, err := ParseGenericJSON("application/problem+json", body, "response")
	if err != nil {
		t.Fatalf("ParseGenericJSON() error = %v", err)
	}
	mutated, err := payload.Mutate([]ProviderContentMutation{{ID: 0, Content: `[{"message":"[MASKED]"},{"message":"second"}]`}})
	if err != nil || string(mutated) != `[{"message":"[MASKED]"},{"message":"second"}]` {
		t.Fatalf("mutated = %s, error = %v", mutated, err)
	}
}

func TestGenericJSONRejectsUnsafeShapesAndMutations(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "non JSON content type", contentType: "text/plain", body: `{}`},
		{name: "malformed", contentType: "application/json", body: `{"value":`},
		{name: "duplicate nested key", contentType: "application/json", body: `{"nested":{"value":"first","value":"second"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseGenericJSON(test.contentType, []byte(test.body), "request")
			if !errors.Is(err, ErrUnsupportedProviderContent) || strings.Contains(err.Error(), "first") || strings.Contains(err.Error(), "second") {
				t.Fatalf("error = %v", err)
			}
		})
	}

	payload, err := ParseGenericJSON("application/json", []byte(`{"value":"safe"}`), "request")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{`["changed-kind"]`, `{"value":"one","value":"two"}`, `not-json`} {
		if _, err := payload.Mutate([]ProviderContentMutation{{ID: 0, Content: mutation}}); !errors.Is(err, ErrInvalidProviderMutation) {
			t.Fatalf("mutation %q error = %v, want %v", mutation, err, ErrInvalidProviderMutation)
		}
	}
}

func TestGenericJSONProcessorRequiresExplicitSelection(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if !strings.Contains(input.Text, `"email":"person@example.test"`) {
			t.Fatalf("Inspect text = %q, want complete JSON document", input.Text)
		}
		return guardrails.InspectResult{
			Action: guardrails.RuleActionMask, SafeContent: strings.ReplaceAll(input.Text, "person@example.test", "[MASKED]"),
			DetectionCount: 1, Categories: []string{"PII"},
		}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"messages":"ordinary generic field","email":"person@example.test","enabled":true}`)
	snapshot := &policy.CompiledSnapshot{PolicyID: "generic", Version: 6, Definition: requestPolicyDefinition()}
	if _, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "generic", PolicyVersion: 6, PolicySnapshot: snapshot,
	}); err == nil {
		t.Fatal("generic JSON was accepted without an explicit content-adapter selection")
	}
	result, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageRequest, ContentAdapter: GenericJSONContentAdapter,
		ContentType: "application/json", Body: body, PolicyID: "generic", PolicyVersion: 6, PolicySnapshot: snapshot,
	})
	if err != nil || result.Action != ActionMask || result.Metadata.Adapter != genericJSONProvider || strings.Contains(string(result.Body), "person@example.test") || result.HeaderMutations["content-length"] != strconv.Itoa(len(result.Body)) {
		t.Fatalf("result = %+v body=%s error=%v", result, result.Body, err)
	}
}

func TestGenericJSONResponseBlockUsesProviderPipeline(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		return guardrails.InspectResult{Action: guardrails.RuleActionBlock, DetectionCount: 1, Categories: []string{"SECRET"}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageResponse, ContentAdapter: GenericJSONContentAdapter,
		ContentType: "application/problem+json", Body: []byte(`{"detail":"blocked-secret"}`),
		PolicySnapshot: &policy.CompiledSnapshot{Definition: responsePolicyDefinition()},
	})
	if err != nil || result.Action != ActionBlock || result.ImmediateStatus != 403 || result.Metadata.Adapter != genericJSONProvider {
		t.Fatalf("result = %+v error=%v", result, err)
	}
}
