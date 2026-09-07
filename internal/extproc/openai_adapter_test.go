package extproc

import (
	"errors"
	"strings"
	"testing"
)

func TestParseChatResponseAcceptsChatCompletionsShape(t *testing.T) {
	body := []byte(`{"id":"chatcmpl-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"},{"index":1,"message":{"role":"assistant","content":"second answer"},"finish_reason":"length"}],"usage":{"total_tokens":7}}`)
	response, err := ParseChatResponse("application/json; charset=utf-8", body)
	if err != nil {
		t.Fatalf("ParseChatResponse() error = %v", err)
	}
	if response.root == nil || response.root.kind != jsonObject {
		t.Fatalf("response root = %+v, want JSON object", response.root)
	}
	if len(response.AssistantContents) != 2 {
		t.Fatalf("assistant contents = %+v, want two entries", response.AssistantContents)
	}
	first, second := response.AssistantContents[0], response.AssistantContents[1]
	if first.ChoiceIndex != 0 || first.JSONPath != ".choices[0].message.content" || first.Content != "hello" {
		t.Fatalf("first assistant content = %+v", first)
	}
	if second.ChoiceIndex != 1 || second.JSONPath != ".choices[1].message.content" || second.Content != "second answer" {
		t.Fatalf("second assistant content = %+v", second)
	}
	if string(response.body) != string(body) {
		t.Fatalf("response body = %q, want %q", response.body, body)
	}
	body[0] = '['
	if response.body[0] != '{' {
		t.Fatal("response must retain an independent copy of the original body")
	}
}

func TestParseChatResponseReturnsTypedErrors(t *testing.T) {
	tests := []struct {
		name  string
		ctype string
		body  string
		want  error
		kind  ChatResponseErrorKind
		path  string
	}{
		{name: "unsupported content type", ctype: "text/plain", body: `{}`, want: ErrUnsupportedChatResponseType, kind: ChatResponseUnsupportedType},
		{name: "empty body", ctype: "application/json", body: " \n\t", want: ErrEmptyChatResponseBody, kind: ChatResponseEmptyBody},
		{name: "invalid JSON", ctype: "application/json", body: `{"choices":[`, want: ErrInvalidChatResponseJSON, kind: ChatResponseInvalidJSON},
		{name: "array root", ctype: "application/json", body: `[]`, want: ErrUnsupportedChatResponse, kind: ChatResponseUnsupportedResponse},
		{name: "missing choices", ctype: "application/json", body: `{"object":"chat.completion"}`, want: ErrUnsupportedChatResponse, kind: ChatResponseUnsupportedResponse, path: ".choices"},
		{name: "non-array choices", ctype: "application/json", body: `{"choices":{}}`, want: ErrUnsupportedChatResponse, kind: ChatResponseUnsupportedResponse, path: ".choices"},
		{name: "non-object choice", ctype: "application/json", body: `{"choices":[null]}`, want: ErrUnsupportedChatResponse, kind: ChatResponseUnsupportedResponse, path: ".choices[0]"},
		{name: "missing message", ctype: "application/json", body: `{"choices":[{}]}`, want: ErrUnsupportedChatResponse, kind: ChatResponseUnsupportedResponse, path: ".choices[0].message"},
		{name: "non-assistant role", ctype: "application/json", body: `{"choices":[{"message":{"role":"tool","content":"no"}}]}`, want: ErrUnsupportedChatResponse, kind: ChatResponseUnsupportedResponse, path: ".choices[0].message.role"},
		{name: "null content", ctype: "application/json", body: `{"choices":[{"message":{"role":"assistant","content":null}}]}`, want: ErrUnsupportedChatResponseContent, kind: ChatResponseUnsupportedContent, path: ".choices[0].message.content"},
		{name: "array content", ctype: "application/json", body: `{"choices":[{"message":{"role":"assistant","content":[]}}]}`, want: ErrUnsupportedChatResponseContent, kind: ChatResponseUnsupportedContent, path: ".choices[0].message.content"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseChatResponse(test.ctype, []byte(test.body))
			if !errors.Is(err, test.want) {
				t.Fatalf("ParseChatResponse() error = %v, want errors.Is(_, %v)", err, test.want)
			}
			var typed *ChatResponseError
			if !errors.As(err, &typed) || typed.Kind != test.kind || typed.Path != test.path {
				t.Fatalf("ParseChatResponse() typed error = %+v, want kind %q and path %q", typed, test.kind, test.path)
			}
		})
	}
}

func TestChatResponseMutatePreservesUnknownFieldsFormattingAndChoiceOrder(t *testing.T) {
	body := []byte(`{ "id" : "chatcmpl-test", "unknown" : { "array" : [ 1, 2 ] }, "choices" : [ { "message" : { "role" : "assistant", "content" : "replace first" }, "finish_reason" : "stop" }, { "message" : { "role" : "assistant", "content" : "replace second" }, "extra" : true } ], "usage" : { "total_tokens" : 5 } }`)
	response, err := ParseChatResponse("application/json", body)
	if err != nil {
		t.Fatalf("ParseChatResponse() error = %v", err)
	}
	mutated, err := response.Mutate([]ChatResponseContentMutation{
		{ID: 0, Content: "[MASKED]"},
		{ID: 1, Content: ""},
	})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"replace first"`, `"[MASKED]"`, 1)
	want = strings.Replace(want, `"replace second"`, `""`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated body changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
	unchanged, err := response.Mutate(nil)
	if err != nil {
		t.Fatalf("Mutate(nil) error = %v", err)
	}
	if string(unchanged) != string(body) {
		t.Fatalf("no-op mutation reformatted body\ngot:  %s\nwant: %s", unchanged, body)
	}
}

func TestChatResponseExtractsAndMutatesTextParts(t *testing.T) {
	body := []byte(`{ "choices" : [ { "message" : { "role" : "assistant", "content" : [ { "type" : "text", "text" : "replace text", "annotations" : [] }, { "type" : "refusal", "refusal" : "replace refusal" } ] }, "extra" : true } ] }`)
	response, err := ParseChatResponse("application/json", body)
	if err != nil {
		t.Fatalf("ParseChatResponse() error = %v", err)
	}
	if len(response.AssistantContents) != 2 || response.AssistantContents[0].JSONPath != ".choices[0].message.content[0].text" || response.AssistantContents[1].JSONPath != ".choices[0].message.content[1].refusal" {
		t.Fatalf("assistant contents = %+v", response.AssistantContents)
	}
	mutated, err := response.Mutate([]ChatResponseContentMutation{{ID: 0, Content: "[MASKED]"}, {ID: 1, Content: "safe refusal"}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"replace text"`, `"[MASKED]"`, 1)
	want = strings.Replace(want, `"replace refusal"`, `"safe refusal"`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated response changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
}

func TestChatResponseExtractsAndMutatesTopLevelRefusal(t *testing.T) {
	body := []byte(`{ "choices" : [ { "message" : { "role" : "assistant", "content" : null, "refusal" : "replace refusal" } } ] }`)
	response, err := ParseChatResponse("application/json", body)
	if err != nil {
		t.Fatalf("ParseChatResponse() error = %v", err)
	}
	if len(response.AssistantContents) != 1 || response.AssistantContents[0].JSONPath != ".choices[0].message.refusal" || response.AssistantContents[0].Content != "replace refusal" {
		t.Fatalf("assistant contents = %+v", response.AssistantContents)
	}
	mutated, err := response.Mutate([]ChatResponseContentMutation{{ID: response.AssistantContents[0].ID, Content: "[MASKED]"}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"replace refusal"`, `"[MASKED]"`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated response changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
}

func TestChatResponseMutateRejectsUnknownOrDuplicateTargets(t *testing.T) {
	response, err := ParseChatResponse("application/json", []byte(`{"choices":[{"message":{"role":"assistant","content":"one"}}]}`))
	if err != nil {
		t.Fatalf("ParseChatResponse() error = %v", err)
	}
	for _, mutations := range [][]ChatResponseContentMutation{
		{{ID: 2, Content: "unknown"}},
		{{ID: 0, Content: "first"}, {ID: 0, Content: "second"}},
	} {
		_, err := response.Mutate(mutations)
		if !errors.Is(err, ErrInvalidChatResponseMutation) {
			t.Fatalf("Mutate(%+v) error = %v, want ErrInvalidChatResponseMutation", mutations, err)
		}
	}
}

func TestChatResponseExtractsAndMutatesToolCallArguments(t *testing.T) {
	body := []byte(`{ "choices" : [ { "message" : { "role" : "assistant", "content" : null, "tool_calls" : [ { "id" : "call_1", "type" : "function", "function" : { "name" : "lookup", "arguments" : "{\"email\":\"secret@example.com\"}" } } ] } } ] }`)
	response, err := ParseChatResponse("application/json", body)
	if err != nil {
		t.Fatalf("ParseChatResponse() error = %v", err)
	}
	if len(response.AssistantContents) != 1 || response.AssistantContents[0].JSONPath != ".choices[0].message.tool_calls[0].function.arguments" {
		t.Fatalf("response contents = %+v", response.AssistantContents)
	}
	mutated, err := response.Mutate([]ChatResponseContentMutation{{ID: response.AssistantContents[0].ID, Content: `{"email":"[MASKED]"}`}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"{\"email\":\"secret@example.com\"}"`, `"{\"email\":\"[MASKED]\"}"`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated tool call changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
}

func TestParseChatRequestExtractsMessageAndToolResultContent(t *testing.T) {
	body := []byte(`{
  "model": "gpt-test",
  "messages": [
    {"role":"system","content":"system instructions"},
    {"role":"developer","content":"developer instructions"},
    {"role":"assistant","content":"assistant reply"},
    {"role":"tool","tool_call_id":"call_0","content":"tool output"},
    {"role":"user","content":"first user message"},
    {"role":"user","content":"second user message"}
  ],
  "unknown_top_level": {"keep": true}
}`)
	request, err := ParseChatRequest("application/json; charset=utf-8", body)
	if err != nil {
		t.Fatalf("ParseChatRequest() error = %v", err)
	}
	if len(request.Contents) != 6 {
		t.Fatalf("request contents = %+v, want six entries", request.Contents)
	}
	system, developer, assistant, toolResult, first, second := request.Contents[0], request.Contents[1], request.Contents[2], request.Contents[3], request.Contents[4], request.Contents[5]
	if system.MessageIndex != 0 || system.Role != "system" || system.JSONPath != ".messages[0].content" || system.Content != "system instructions" {
		t.Fatalf("system content = %+v", system)
	}
	if developer.MessageIndex != 1 || developer.Role != "developer" || developer.JSONPath != ".messages[1].content" || developer.Content != "developer instructions" {
		t.Fatalf("developer content = %+v", developer)
	}
	if assistant.MessageIndex != 2 || assistant.Role != "assistant" || assistant.JSONPath != ".messages[2].content" || assistant.Content != "assistant reply" {
		t.Fatalf("assistant content = %+v", assistant)
	}
	if toolResult.MessageIndex != 3 || toolResult.Role != "tool" || toolResult.JSONPath != ".messages[3].content" || toolResult.Content != "tool output" {
		t.Fatalf("tool result content = %+v", toolResult)
	}
	if first.MessageIndex != 4 || first.Role != "user" || first.JSONPath != ".messages[4].content" || first.Content != "first user message" {
		t.Fatalf("first user content = %+v", first)
	}
	if second.MessageIndex != 5 || second.Role != "user" || second.JSONPath != ".messages[5].content" || second.Content != "second user message" {
		t.Fatalf("second user content = %+v", second)
	}
}

func TestChatRequestExtractsAndMutatesTopLevelRefusal(t *testing.T) {
	body := []byte(`{ "messages" : [ { "role" : "assistant", "content" : null, "refusal" : "previous refusal" } ] }`)
	request, err := ParseChatRequest("application/json", body)
	if err != nil {
		t.Fatalf("ParseChatRequest() error = %v", err)
	}
	if len(request.Contents) != 1 || request.Contents[0].JSONPath != ".messages[0].refusal" || request.Contents[0].Content != "previous refusal" {
		t.Fatalf("request contents = %+v", request.Contents)
	}
	mutated, err := request.Mutate([]ChatContentMutation{{ID: request.Contents[0].ID, Content: "[MASKED]"}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"previous refusal"`, `"[MASKED]"`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated request changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
}

func TestChatRequestMutatePreservesUnknownFieldsFormattingAndMessageOrder(t *testing.T) {
	body := []byte(`{ "model" : "gpt-test", "unknown" : { "array" : [ 1, 2 ] }, "messages" : [ { "role" : "system", "content" : "replace system" }, { "role" : "user", "content" : "replace me", "extra" : { "x" : true } }, { "role" : "assistant", "content" : "leave assistant" } ], "stream" : false }`)
	request, err := ParseChatRequest("application/json", body)
	if err != nil {
		t.Fatalf("ParseChatRequest() error = %v", err)
	}
	if len(request.Contents) != 3 || request.Contents[0].MessageIndex != 0 || request.Contents[1].MessageIndex != 1 || request.Contents[2].MessageIndex != 2 {
		t.Fatalf("request contents = %+v", request.Contents)
	}
	mutated, err := request.Mutate([]ChatContentMutation{{ID: 0, Content: "safe system"}, {ID: 1, Content: "masked\ncontent"}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"replace system"`, `"safe system"`, 1)
	want = strings.Replace(want, `"replace me"`, `"masked\ncontent"`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated body changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
	unchanged, err := request.Mutate(nil)
	if err != nil {
		t.Fatalf("Mutate(nil) error = %v", err)
	}
	if string(unchanged) != string(body) {
		t.Fatalf("no-op mutation reformatted body\ngot:  %s\nwant: %s", unchanged, body)
	}
}

func TestParseChatRequestReturnsTypedErrors(t *testing.T) {
	tests := []struct {
		name  string
		ctype string
		body  string
		want  error
		kind  ChatRequestErrorKind
	}{
		{name: "unsupported content type", ctype: "text/plain", body: `{}`, want: ErrUnsupportedChatContentType, kind: ChatRequestUnsupportedType},
		{name: "empty body", ctype: "application/json", body: " \n\t", want: ErrEmptyChatRequestBody, kind: ChatRequestEmptyBody},
		{name: "invalid JSON", ctype: "application/json", body: `{"messages":[`, want: ErrInvalidChatRequestJSON, kind: ChatRequestInvalidJSON},
		{name: "Responses API shape", ctype: "application/json", body: `{"input":"not chat completions"}`, want: ErrUnsupportedChatRequest, kind: ChatRequestUnsupportedRequest},
		{name: "user object content", ctype: "application/json", body: `{"messages":[{"role":"user","content":{"text":"no"}}]}`, want: ErrUnsupportedChatContent, kind: ChatRequestUnsupportedContent},
		{name: "user missing content", ctype: "application/json", body: `{"messages":[{"role":"user"}]}`, want: ErrUnsupportedChatContent, kind: ChatRequestUnsupportedContent},
		{name: "empty content array", ctype: "application/json", body: `{"messages":[{"role":"user","content":[]}]}`, want: ErrUnsupportedChatContent, kind: ChatRequestUnsupportedContent},
		{name: "unknown content part", ctype: "application/json", body: `{"messages":[{"role":"user","content":[{"type":"unknown","text":"no"}]}]}`, want: ErrUnsupportedChatContent, kind: ChatRequestUnsupportedContent},
		{name: "image on system role", ctype: "application/json", body: `{"messages":[{"role":"system","content":[{"type":"image_url","image_url":{"url":"https://example.test/image.png"}}]}]}`, want: ErrUnsupportedChatContent, kind: ChatRequestUnsupportedContent},
		{name: "spoofed image text", ctype: "application/json", body: `{"messages":[{"role":"user","content":[{"type":"image_url","text":"bypass","image_url":{"url":"https://example.test/image.png"}}]}]}`, want: ErrUnsupportedChatContent, kind: ChatRequestUnsupportedContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseChatRequest(test.ctype, []byte(test.body))
			if !errors.Is(err, test.want) {
				t.Fatalf("ParseChatRequest() error = %v, want errors.Is(_, %v)", err, test.want)
			}
			var typed *ChatRequestError
			if !errors.As(err, &typed) || typed.Kind != test.kind {
				t.Fatalf("ParseChatRequest() typed error = %+v, want kind %q", typed, test.kind)
			}
		})
	}
}

func TestChatRequestExtractsAndMutatesMultimodalTextParts(t *testing.T) {
	body := []byte(`{ "messages" : [ { "role" : "system", "content" : [ { "type" : "text", "text" : "system secret" } ] }, { "role" : "user", "content" : [ { "type" : "text", "text" : "first secret" }, { "type" : "image_url", "image_url" : { "url" : "data:image/png;base64,AAAA", "detail" : "low" } }, { "type" : "text", "text" : "second secret", "extra" : true } ] }, { "role" : "assistant", "content" : [ { "type" : "refusal", "refusal" : "assistant secret" } ] }, { "role" : "tool", "tool_call_id" : "call_1", "content" : [ { "type" : "text", "text" : "tool secret" } ] } ] }`)
	request, err := ParseChatRequest("application/json", body)
	if err != nil {
		t.Fatalf("ParseChatRequest() error = %v", err)
	}
	wantPaths := []string{
		".messages[0].content[0].text",
		".messages[1].content[0].text",
		".messages[1].content[2].text",
		".messages[2].content[0].refusal",
		".messages[3].content[0].text",
	}
	if len(request.Contents) != len(wantPaths) {
		t.Fatalf("request contents = %+v", request.Contents)
	}
	mutations := make([]ChatContentMutation, len(wantPaths))
	for index, path := range wantPaths {
		if request.Contents[index].JSONPath != path {
			t.Fatalf("content[%d] = %+v, want path %s", index, request.Contents[index], path)
		}
		mutations[index] = ChatContentMutation{ID: request.Contents[index].ID, Content: "[MASKED]"}
	}
	mutated, err := request.Mutate(mutations)
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	if strings.Count(string(mutated), `"[MASKED]"`) != len(wantPaths) {
		t.Fatalf("mutated request = %s", mutated)
	}
	if !strings.Contains(string(mutated), `"url" : "data:image/png;base64,AAAA", "detail" : "low"`) {
		t.Fatalf("non-text image part changed: %s", mutated)
	}
}

func TestParseChatRequestAcceptsStreamingRequests(t *testing.T) {
	request, err := ParseChatRequest("application/json", []byte(`{"stream":true,"messages":[{"role":"user","content":"safe"}]}`))
	if err != nil {
		t.Fatalf("ParseChatRequest() error = %v", err)
	}
	if len(request.Contents) != 1 || request.Contents[0].Content != "safe" {
		t.Fatalf("request contents = %+v", request.Contents)
	}
}

func TestChatRequestMutateRejectsUnknownOrDuplicateTargets(t *testing.T) {
	request, err := ParseChatRequest("application/json", []byte(`{"messages":[{"role":"user","content":"one"}]}`))
	if err != nil {
		t.Fatalf("ParseChatRequest() error = %v", err)
	}
	for _, mutations := range [][]ChatContentMutation{
		{{ID: 2, Content: "unknown"}},
		{{ID: 0, Content: "first"}, {ID: 0, Content: "second"}},
	} {
		_, err := request.Mutate(mutations)
		if !errors.Is(err, ErrInvalidChatMutation) {
			t.Fatalf("Mutate(%+v) error = %v, want ErrInvalidChatMutation", mutations, err)
		}
	}
}

func TestChatRequestExtractsAndMutatesToolCallsAndResults(t *testing.T) {
	body := []byte(`{ "messages" : [ { "role" : "assistant", "content" : null, "tool_calls" : [ { "id" : "call_1", "type" : "function", "function" : { "name" : "lookup", "arguments" : "{\"email\":\"secret@example.com\"}" } } ] }, { "role" : "tool", "tool_call_id" : "call_1", "content" : "result secret@example.com" } ] }`)
	request, err := ParseChatRequest("application/json", body)
	if err != nil {
		t.Fatalf("ParseChatRequest() error = %v", err)
	}
	if len(request.Contents) != 2 || request.Contents[0].Role != "tool_call" || request.Contents[1].Role != "tool" {
		t.Fatalf("request contents = %+v", request.Contents)
	}
	mutated, err := request.Mutate([]ChatContentMutation{{ID: 0, Content: `{"email":"[MASKED]"}`}, {ID: 1, Content: "result [MASKED]"}})
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	want := strings.Replace(string(body), `"{\"email\":\"secret@example.com\"}"`, `"{\"email\":\"[MASKED]\"}"`, 1)
	want = strings.Replace(want, `"result secret@example.com"`, `"result [MASKED]"`, 1)
	if string(mutated) != want {
		t.Fatalf("mutated tool payload changed unrelated JSON\ngot:  %s\nwant: %s", mutated, want)
	}
}

func TestChatToolPayloadsReturnTypedErrors(t *testing.T) {
	_, requestErr := ParseChatRequest("application/json", []byte(`{"messages":[{"role":"assistant","content":null,"tool_calls":[{"function":{"name":"lookup","arguments":{}}}]}]}`))
	if !errors.Is(requestErr, ErrUnsupportedChatContent) {
		t.Fatalf("request error = %v, want unsupported content", requestErr)
	}
	_, responseErr := ParseChatResponse("application/json", []byte(`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"arguments":"{}"}}]}}]}`))
	if !errors.Is(responseErr, ErrUnsupportedChatResponseContent) {
		t.Fatalf("response error = %v, want unsupported response content", responseErr)
	}
	_, roleErr := ParseChatRequest("application/json", []byte(`{"messages":[{"role":"user","content":"safe","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"secret"}}]}]}`))
	if !errors.Is(roleErr, ErrUnsupportedChatContent) {
		t.Fatalf("role spoof error = %v, want unsupported content", roleErr)
	}
}
