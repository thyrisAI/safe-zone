package extproc

import (
	"errors"
	"strings"
	"testing"
)

func TestAnthropicRequestExtractsAndMutatesSupportedContent(t *testing.T) {
	body := []byte(`{ "model" : "claude-test", "system" : [ { "type" : "text", "text" : "system secret", "cache_control" : { "type" : "ephemeral" } } ], "messages" : [ { "role" : "user", "content" : [ { "type" : "text", "text" : "user secret" }, { "type" : "image", "source" : { "type" : "base64", "media_type" : "image/png", "data" : "AAAA" } } ] }, { "role" : "assistant", "content" : [ { "type" : "text", "text" : "assistant secret" }, { "type" : "tool_use", "id" : "toolu_1", "name" : "lookup", "input" : { "email" : "secret@example.com" } } ] }, { "role" : "user", "content" : [ { "type" : "tool_result", "tool_use_id" : "toolu_1", "content" : [ { "type" : "text", "text" : "tool secret" }, { "type" : "image", "source" : { "type" : "base64", "data" : "BBBB" } } ] } ] } ], "max_tokens" : 100 }`)
	payload, err := ParseAnthropicRequest("application/json", body)
	if err != nil {
		t.Fatalf("ParseAnthropicRequest() error = %v", err)
	}
	wantPaths := []string{".system[0].text", ".messages[0].content[0].text", ".messages[1].content[0].text", ".messages[1].content[1].input", ".messages[2].content[0].content[0].text"}
	if len(payload.Contents) != len(wantPaths) {
		t.Fatalf("contents = %+v", payload.Contents)
	}
	mutations := make([]ProviderContentMutation, len(wantPaths))
	for index, path := range wantPaths {
		if payload.Contents[index].JSONPath != path {
			t.Fatalf("content[%d] = %+v, want %s", index, payload.Contents[index], path)
		}
		value := "[MASKED]"
		if payload.Contents[index].Role == "tool_call" {
			value = `{ "email" : "[MASKED]" }`
		}
		mutations[index] = ProviderContentMutation{ID: index, Content: value}
	}
	mutated, err := payload.Mutate(mutations)
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	if strings.Count(string(mutated), `"[MASKED]"`) != 5 || !strings.Contains(string(mutated), `"data" : "AAAA"`) || !strings.Contains(string(mutated), `"data" : "BBBB"`) || !strings.Contains(string(mutated), `"name" : "lookup"`) {
		t.Fatalf("mutated payload = %s", mutated)
	}
	unchanged, err := payload.Mutate(nil)
	if err != nil || string(unchanged) != string(body) {
		t.Fatalf("no-op mutation changed payload: %s, error=%v", unchanged, err)
	}
}

func TestAnthropicResponseExtractsTextAndToolInput(t *testing.T) {
	body := []byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"thinking","thinking":"opaque","signature":"sig"},{"type":"text","text":"answer secret"},{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"query":"secret"}}],"stop_reason":"tool_use"}`)
	payload, err := ParseAnthropicResponse("application/json", body)
	if err != nil {
		t.Fatalf("ParseAnthropicResponse() error = %v", err)
	}
	if len(payload.Contents) != 2 || payload.Contents[0].JSONPath != ".content[1].text" || payload.Contents[1].JSONPath != ".content[2].input" {
		t.Fatalf("contents = %+v", payload.Contents)
	}
}

func TestAnthropicAdapterFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "not Anthropic", body: `{"input":"hello"}`, want: ErrUnsupportedProviderPayload},
		{name: "unknown block", body: `{"messages":[{"role":"user","content":[{"type":"future","text":"bypass"}]}]}`, want: ErrUnsupportedProviderContent},
		{name: "spoofed image text", body: `{"messages":[{"role":"user","content":[{"type":"image","text":"bypass","source":{}}]}]}`, want: ErrUnsupportedProviderContent},
		{name: "invalid tool input", body: `{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"lookup","input":"secret"}]}]}`, want: ErrUnsupportedProviderContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseAnthropicRequest("application/json", []byte(test.body))
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestProviderPayloadRejectsInvalidJSONToolMutation(t *testing.T) {
	payload, err := ParseAnthropicRequest("application/json", []byte(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"value":"secret"}}]}]}`))
	if err != nil {
		t.Fatalf("ParseAnthropicRequest() error = %v", err)
	}
	_, err = payload.Mutate([]ProviderContentMutation{{ID: 0, Content: "[MASKED]"}})
	if !errors.Is(err, ErrInvalidProviderMutation) {
		t.Fatalf("Mutate() error = %v", err)
	}
}
