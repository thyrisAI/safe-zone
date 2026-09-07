package extproc

import (
	"errors"
	"strings"
	"testing"
)

func TestParseResponsesRequestExtractsMessagesAndToolResults(t *testing.T) {
	body := []byte(`{
  "model": "gpt-test",
  "instructions": "top-level system instructions",
	"input": [
		{"role":"system","content":"system message"},
		{"role":"developer","content":"developer message"},
		{"type":"message","role":"user","content":"plain user text"},
    {"type":"message","role":"user","content":[
      {"type":"input_text","text":"structured user text"},
      {"type":"input_image","image_url":"https://example.test/image.png"}
    ]},
    {"type":"message","role":"assistant","content":[{"type":"output_text","text":"previous assistant text"}]},
    {"type":"function_call_output","call_id":"call_1","output":"tool output"}
  ],
  "unknown": {"keep": true}
}`)
	request, err := ParseResponsesRequest("application/json; charset=utf-8", body)
	if err != nil {
		t.Fatalf("ParseResponsesRequest() error = %v", err)
	}
	if len(request.Contents) != 7 {
		t.Fatalf("request contents = %+v, want seven entries", request.Contents)
	}
	if got := request.Contents[0]; got.ID != 0 || got.Role != "system" || got.JSONPath != ".instructions" || got.Content != "top-level system instructions" {
		t.Fatalf("instructions content = %+v", got)
	}
	if got := request.Contents[1]; got.ID != 1 || got.Role != "system" || got.JSONPath != ".input[0].content" || got.Content != "system message" {
		t.Fatalf("system content = %+v", got)
	}
	if got := request.Contents[2]; got.ID != 2 || got.Role != "developer" || got.JSONPath != ".input[1].content" || got.Content != "developer message" {
		t.Fatalf("developer content = %+v", got)
	}
	if got := request.Contents[3]; got.ID != 3 || got.Role != "user" || got.JSONPath != ".input[2].content" || got.Content != "plain user text" {
		t.Fatalf("first user content = %+v", got)
	}
	if got := request.Contents[4]; got.ID != 4 || got.Role != "user" || got.JSONPath != ".input[3].content[0].text" || got.Content != "structured user text" {
		t.Fatalf("second user content = %+v", got)
	}
	if got := request.Contents[5]; got.ID != 5 || got.Role != "assistant" || got.JSONPath != ".input[4].content[0].text" || got.Content != "previous assistant text" {
		t.Fatalf("assistant content = %+v", got)
	}
	if got := request.Contents[6]; got.ID != 6 || got.Role != "tool_result" || got.JSONPath != ".input[5].output" || got.Content != "tool output" {
		t.Fatalf("tool result content = %+v", got)
	}
}

func TestParseResponsesRequestAcceptsStringInput(t *testing.T) {
	request, err := ParseResponsesRequest("application/json", []byte(`{"model":"gpt-test","input":"hello"}`))
	if err != nil {
		t.Fatalf("ParseResponsesRequest() error = %v", err)
	}
	if len(request.Contents) != 1 || request.Contents[0].JSONPath != ".input" || request.Contents[0].Content != "hello" {
		t.Fatalf("request contents = %+v", request.Contents)
	}
}

func TestResponsesRequestMutatePreservesUnknownFieldsAndFormatting(t *testing.T) {
	body := []byte(`{ "unknown" : [ 1, 2 ], "input" : [ { "role" : "user", "content" : "replace one", "extra" : true }, { "role" : "user", "content" : [ { "type" : "input_text", "text" : "replace two" } ] } ] }`)
	request, err := ParseResponsesRequest("application/json", body)
	if err != nil {
		t.Fatalf("ParseResponsesRequest() error = %v", err)
	}
	mutated, err := request.Mutate([]ResponsesContentMutation{{ID: 0, Content: "[MASKED]"}, {ID: 1, Content: "safe\ntext"}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"replace one"`, `"[MASKED]"`, 1)
	want = strings.Replace(want, `"replace two"`, `"safe\ntext"`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated request changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
	unchanged, err := request.Mutate(nil)
	if err != nil || string(unchanged) != string(body) {
		t.Fatalf("no-op mutation = %q, error = %v", unchanged, err)
	}
}

func TestResponsesRequestMutatesFunctionCallsAndOutputs(t *testing.T) {
	body := []byte(`{ "input" : [ { "type" : "function_call", "name" : "lookup", "call_id" : "call_1", "arguments" : "{\"email\":\"secret@example.com\"}" }, { "type" : "function_call_output", "call_id" : "call_1", "output" : "result secret@example.com" } ] }`)
	request, err := ParseResponsesRequest("application/json", body)
	if err != nil {
		t.Fatalf("ParseResponsesRequest() error = %v", err)
	}
	if len(request.Contents) != 2 || request.Contents[0].Role != "tool_call" || request.Contents[1].Role != "tool_result" {
		t.Fatalf("request contents = %+v", request.Contents)
	}
	mutated, err := request.Mutate([]ResponsesContentMutation{{ID: 0, Content: `{"email":"[MASKED]"}`}, {ID: 1, Content: "result [MASKED]"}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"{\"email\":\"secret@example.com\"}"`, `"{\"email\":\"[MASKED]\"}"`, 1)
	want = strings.Replace(want, `"result secret@example.com"`, `"result [MASKED]"`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated tool payload changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
}

func TestResponsesRequestMutatesMultimodalToolOutputTextOnly(t *testing.T) {
	body := []byte(`{ "input" : [ { "type" : "function_call_output", "call_id" : "call_1", "output" : [ { "type" : "input_text", "text" : "secret result" }, { "type" : "input_image", "image_url" : "data:image/png;base64,AAAA" }, { "type" : "input_file", "file_id" : "file_1" } ] } ] }`)
	request, err := ParseResponsesRequest("application/json", body)
	if err != nil {
		t.Fatalf("ParseResponsesRequest() error = %v", err)
	}
	if len(request.Contents) != 1 || request.Contents[0].JSONPath != ".input[0].output[0].text" || request.Contents[0].Role != "tool_result" {
		t.Fatalf("request contents = %+v", request.Contents)
	}
	mutated, err := request.Mutate([]ResponsesContentMutation{{ID: request.Contents[0].ID, Content: "[MASKED]"}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"secret result"`, `"[MASKED]"`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated tool result changed non-text parts\ngot:  %s\nwant: %s", mutated, want)
	}
}

func TestParseResponsesRequestReturnsTypedErrors(t *testing.T) {
	tests := []struct {
		name, contentType, body, path string
		want                          error
		kind                          ResponsesErrorKind
	}{
		{name: "content type", contentType: "text/plain", body: `{}`, want: ErrUnsupportedResponsesType, kind: ResponsesUnsupportedType},
		{name: "empty", contentType: "application/json", body: " ", want: ErrEmptyResponsesBody, kind: ResponsesEmptyBody},
		{name: "invalid JSON", contentType: "application/json", body: `{"input":`, want: ErrInvalidResponsesJSON, kind: ResponsesInvalidJSON},
		{name: "ambiguous payload without input", contentType: "application/json", body: `{"model":"gpt-test"}`, path: ".input", want: ErrUnsupportedResponsesPayload, kind: ResponsesUnsupportedPayload},
		{name: "object input", contentType: "application/json", body: `{"input":{}}`, path: ".input", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "missing user content", contentType: "application/json", body: `{"input":[{"role":"user"}]}`, path: ".input[0].content", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "invalid input text", contentType: "application/json", body: `{"input":[{"role":"user","content":[{"type":"input_text","text":null}]}]}`, path: ".input[0].content[0].text", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "invalid instructions", contentType: "application/json", body: `{"instructions":[],"input":"hello"}`, path: ".instructions", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "invalid assistant output text", contentType: "application/json", body: `{"input":[{"role":"assistant","content":[{"type":"output_text","text":null}]}]}`, path: ".input[0].content[0].text", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "unknown multimodal part", contentType: "application/json", body: `{"input":[{"role":"user","content":[{"type":"future_type","text":"bypass"}]}]}`, path: ".input[0].content[0].type", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "spoofed image text", contentType: "application/json", body: `{"input":[{"role":"user","content":[{"type":"input_image","text":"bypass","image_url":"https://example.test/image.png"}]}]}`, path: ".input[0].content[0]", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "wrong text type for role", contentType: "application/json", body: `{"input":[{"role":"assistant","content":[{"type":"input_text","text":"bypass"}]}]}`, path: ".input[0].content[0].type", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "function call missing name", contentType: "application/json", body: `{"input":[{"type":"function_call","call_id":"call_1","arguments":"{}"}]}`, path: ".input[0].name", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "invalid function output", contentType: "application/json", body: `{"input":[{"type":"function_call_output","call_id":"call_1","output":{}}]}`, path: ".input[0].output", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "spoofed tool result type", contentType: "application/json", body: `{"input":[{"type":"message","call_id":"call_1","output":"secret"}]}`, path: ".input[0].type", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseResponsesRequest(test.contentType, []byte(test.body))
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.want)
			}
			var typed *ResponsesError
			if !errors.As(err, &typed) || typed.Kind != test.kind || typed.Path != test.path {
				t.Fatalf("typed error = %+v, want kind=%q path=%q", typed, test.kind, test.path)
			}
		})
	}
}

func TestParseResponsesRequestAllowsConversationContinuationWithoutNewInput(t *testing.T) {
	request, err := ParseResponsesRequest("application/json", []byte(`{"model":"gpt-test","previous_response_id":"resp_1"}`))
	if err != nil {
		t.Fatalf("ParseResponsesRequest() error = %v", err)
	}
	if len(request.Contents) != 0 {
		t.Fatalf("request contents = %+v, want none", request.Contents)
	}
}

func TestParseResponsesRequestAcceptsInstructionsWithoutInput(t *testing.T) {
	request, err := ParseResponsesRequest("application/json", []byte(`{"model":"gpt-test","instructions":"protect this context"}`))
	if err != nil {
		t.Fatalf("ParseResponsesRequest() error = %v", err)
	}
	if len(request.Contents) != 1 || request.Contents[0].Role != "system" || request.Contents[0].JSONPath != ".instructions" {
		t.Fatalf("request contents = %+v", request.Contents)
	}
}

func TestParseResponsesResponseExtractsAssistantOutputText(t *testing.T) {
	body := []byte(`{"id":"resp_1","object":"response","output":[{"type":"reasoning","summary":[]},{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"first","annotations":[]},{"type":"refusal","refusal":"cannot comply"}]},{"id":"call_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},{"id":"msg_2","type":"message","role":"assistant","content":[{"type":"output_text","text":"second","annotations":[]}]}],"output_text":"firstsecond","usage":{"total_tokens":5}}`)
	response, err := ParseResponsesResponse("application/json", body)
	if err != nil {
		t.Fatalf("ParseResponsesResponse() error = %v", err)
	}
	if len(response.Contents) != 4 {
		t.Fatalf("response contents = %+v, want four entries", response.Contents)
	}
	if got := response.Contents[0]; got.JSONPath != ".output[1].content[0].text" || got.Content != "first" {
		t.Fatalf("first assistant content = %+v", got)
	}
	if got := response.Contents[1]; got.JSONPath != ".output[1].content[1].refusal" || got.Content != "cannot comply" {
		t.Fatalf("refusal content = %+v", got)
	}
	if got := response.Contents[2]; got.JSONPath != ".output[2].arguments" || got.Content != "{}" {
		t.Fatalf("tool call content = %+v", got)
	}
	if got := response.Contents[3]; got.JSONPath != ".output[3].content[0].text" || got.Content != "second" {
		t.Fatalf("second assistant content = %+v", got)
	}
}

func TestResponsesResponseMutatesFunctionCallWithoutChangingOutputText(t *testing.T) {
	body := []byte(`{"object":"response","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"visible"}]},{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"email\":\"secret@example.com\"}"}],"output_text":"visible"}`)
	response, err := ParseResponsesResponse("application/json", body)
	if err != nil {
		t.Fatalf("ParseResponsesResponse() error = %v", err)
	}
	if len(response.Contents) != 2 || response.Contents[1].Role != "tool_call" {
		t.Fatalf("response contents = %+v", response.Contents)
	}
	mutated, err := response.Mutate([]ResponsesContentMutation{{ID: response.Contents[1].ID, Content: `{"email":"[MASKED]"}`}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"{\"email\":\"secret@example.com\"}"`, `"{\"email\":\"[MASKED]\"}"`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated function call changed output text\ngot:  %s\nwant: %s", mutated, want)
	}
}

func TestResponsesResponseMutateUpdatesNestedAndConvenienceOutputText(t *testing.T) {
	body := []byte(`{ "output_text" : "secret safe", "object" : "response", "unknown" : true, "output" : [ { "type" : "message", "role" : "assistant", "content" : [ { "type" : "output_text", "text" : "secret " }, { "type" : "output_text", "text" : "safe" } ] } ] }`)
	response, err := ParseResponsesResponse("application/json", body)
	if err != nil {
		t.Fatalf("ParseResponsesResponse() error = %v", err)
	}
	mutated, err := response.Mutate([]ResponsesContentMutation{{ID: 0, Content: "[MASKED] "}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"secret safe"`, `"[MASKED] safe"`, 1)
	want = strings.Replace(want, `"secret "`, `"[MASKED] "`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated response changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
}

func TestResponsesResponseUsesConvenienceOutputTextAsSafeFallback(t *testing.T) {
	response, err := ParseResponsesResponse("application/json", []byte(`{"object":"response","output":[],"output_text":"visible text"}`))
	if err != nil {
		t.Fatalf("ParseResponsesResponse() error = %v", err)
	}
	if len(response.Contents) != 1 || response.Contents[0].JSONPath != ".output_text" {
		t.Fatalf("response contents = %+v", response.Contents)
	}
	mutated, err := response.Mutate([]ResponsesContentMutation{{ID: 0, Content: "[MASKED]"}})
	if err != nil || string(mutated) != `{"object":"response","output":[],"output_text":"[MASKED]"}` {
		t.Fatalf("fallback mutation = %s, error = %v", mutated, err)
	}
}

func TestResponsesMutationRejectsUnknownAndDuplicateTargets(t *testing.T) {
	request, err := ParseResponsesRequest("application/json", []byte(`{"input":"one"}`))
	if err != nil {
		t.Fatalf("ParseResponsesRequest() error = %v", err)
	}
	for _, mutations := range [][]ResponsesContentMutation{
		{{ID: 4, Content: "unknown"}},
		{{ID: 0, Content: "first"}, {ID: 0, Content: "second"}},
	} {
		_, err := request.Mutate(mutations)
		if !errors.Is(err, ErrInvalidResponsesMutation) {
			t.Fatalf("Mutate(%+v) error = %v, want ErrInvalidResponsesMutation", mutations, err)
		}
	}
}

func TestParseResponsesResponseReturnsTypedErrors(t *testing.T) {
	tests := []struct {
		name, body, path string
		want             error
		kind             ResponsesErrorKind
	}{
		{name: "wrong object", body: `{"object":"chat.completion","output":[]}`, path: ".object", want: ErrUnsupportedResponsesPayload, kind: ResponsesUnsupportedPayload},
		{name: "missing output", body: `{"object":"response"}`, path: ".output", want: ErrUnsupportedResponsesPayload, kind: ResponsesUnsupportedPayload},
		{name: "invalid message content", body: `{"object":"response","output":[{"type":"message","role":"assistant","content":null}]}`, path: ".output[0].content", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "invalid output text", body: `{"object":"response","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":null}]}]}`, path: ".output[0].content[0].text", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "invalid refusal", body: `{"object":"response","output":[{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":null}]}]}`, path: ".output[0].content[0].refusal", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "unknown output part", body: `{"object":"response","output":[{"type":"message","role":"assistant","content":[{"type":"future_type","text":"bypass"}]}]}`, path: ".output[0].content[0].type", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "function call missing arguments", body: `{"object":"response","output":[{"type":"function_call","call_id":"call_1","name":"lookup"}]}`, path: ".output[0].arguments", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
		{name: "spoofed function call type", body: `{"object":"response","output":[{"type":"message","call_id":"call_1","name":"lookup","arguments":"secret"}]}`, path: ".output[0].type", want: ErrUnsupportedResponsesContent, kind: ResponsesUnsupportedContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseResponsesResponse("application/json", []byte(test.body))
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.want)
			}
			var typed *ResponsesError
			if !errors.As(err, &typed) || typed.Kind != test.kind || typed.Path != test.path {
				t.Fatalf("typed error = %+v, want kind=%q path=%q", typed, test.kind, test.path)
			}
		})
	}
}
