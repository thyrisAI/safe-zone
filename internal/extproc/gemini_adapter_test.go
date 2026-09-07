package extproc

import (
	"errors"
	"strings"
	"testing"
)

func TestGeminiRequestExtractsAndMutatesSupportedParts(t *testing.T) {
	body := []byte(`{ "systemInstruction" : { "parts" : [ { "text" : "system secret" } ] }, "contents" : [ { "role" : "user", "parts" : [ { "text" : "user secret" }, { "inlineData" : { "mimeType" : "image/png", "data" : "AAAA" } } ] }, { "role" : "model", "parts" : [ { "functionCall" : { "name" : "lookup", "args" : { "email" : "secret@example.com" } } } ] }, { "role" : "function", "parts" : [ { "functionResponse" : { "name" : "lookup", "response" : { "result" : "tool secret" } } } ] } ] }`)
	payload, err := ParseGeminiRequest("application/json", body)
	if err != nil {
		t.Fatalf("ParseGeminiRequest() error = %v", err)
	}
	wantPaths := []string{".systemInstruction.parts[0].text", ".contents[0].parts[0].text", ".contents[1].parts[0].functionCall.args", ".contents[2].parts[0].functionResponse.response"}
	if len(payload.Contents) != len(wantPaths) {
		t.Fatalf("contents = %+v", payload.Contents)
	}
	mutations := make([]ProviderContentMutation, len(wantPaths))
	for index, path := range wantPaths {
		if payload.Contents[index].JSONPath != path {
			t.Fatalf("content[%d] = %+v, want %s", index, payload.Contents[index], path)
		}
		value := "[MASKED]"
		if payload.Contents[index].rawJSON {
			value = `{ "masked" : true }`
		}
		mutations[index] = ProviderContentMutation{ID: index, Content: value}
	}
	mutated, err := payload.Mutate(mutations)
	if err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	if strings.Count(string(mutated), `"[MASKED]"`) != 2 || strings.Count(string(mutated), `"masked" : true`) != 2 || !strings.Contains(string(mutated), `"data" : "AAAA"`) {
		t.Fatalf("mutated payload = %s", mutated)
	}
}

func TestGeminiResponseExtractsTextCodeAndToolPayloads(t *testing.T) {
	body := []byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"answer"},{"executableCode":{"language":"PYTHON","code":"print('secret')"}},{"codeExecutionResult":{"outcome":"OUTCOME_OK","output":"secret output"}},{"functionCall":{"name":"lookup","args":{"query":"secret"}}},{"toolCall":{"toolType":"GOOGLE_SEARCH_WEB","args":{"query":"secret web"}}},{"toolResponse":{"toolType":"GOOGLE_SEARCH_WEB","response":{"result":"secret result"}}}]}}],"usageMetadata":{"totalTokenCount":4}}`)
	payload, err := ParseGeminiResponse("application/json", body)
	if err != nil {
		t.Fatalf("ParseGeminiResponse() error = %v", err)
	}
	wantPaths := []string{".candidates[0].content.parts[0].text", ".candidates[0].content.parts[1].executableCode.code", ".candidates[0].content.parts[2].codeExecutionResult.output", ".candidates[0].content.parts[3].functionCall.args", ".candidates[0].content.parts[4].toolCall.args", ".candidates[0].content.parts[5].toolResponse.response"}
	if len(payload.Contents) != len(wantPaths) {
		t.Fatalf("contents = %+v", payload.Contents)
	}
	for index, path := range wantPaths {
		if payload.Contents[index].JSONPath != path {
			t.Fatalf("content[%d] = %+v, want %s", index, payload.Contents[index], path)
		}
	}
}

func TestGeminiAdapterFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "not Gemini", body: `{"messages":[]}`, want: ErrUnsupportedProviderPayload},
		{name: "empty contents", body: `{"contents":[]}`, want: ErrUnsupportedProviderContent},
		{name: "unknown part", body: `{"contents":[{"parts":[{"future":"bypass"}]}]}`, want: ErrUnsupportedProviderContent},
		{name: "ambiguous part", body: `{"contents":[{"parts":[{"text":"bypass","inlineData":{"data":"AAAA"}}]}]}`, want: ErrUnsupportedProviderContent},
		{name: "invalid function args", body: `{"contents":[{"role":"model","parts":[{"functionCall":{"name":"lookup","args":"secret"}}]}]}`, want: ErrUnsupportedProviderContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseGeminiRequest("application/json", []byte(test.body))
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestGeminiResponseAcceptsPromptFeedbackWithoutCandidates(t *testing.T) {
	payload, err := ParseGeminiResponse("application/json", []byte(`{"promptFeedback":{"blockReason":"SAFETY"}}`))
	if err != nil || len(payload.Contents) != 0 {
		t.Fatalf("payload = %+v, error = %v", payload, err)
	}
}
