package extproc

import (
	"fmt"
	"net/url"
)

const embeddingsProvider = "openai_embeddings"

// ParseEmbeddingsRequest extracts text inputs without interpreting model names
// or changing unrelated JSON. Token IDs require a model-specific tokenizer and
// are rejected rather than treated as inspected text.
func ParseEmbeddingsRequest(contentType string, body []byte) (*ProviderPayload, error) {
	root, err := parseProviderDocument(embeddingsProvider, contentType, body)
	if err != nil {
		return nil, err
	}
	if root.duplicateKeys {
		return nil, providerPayloadError(embeddingsProvider, "", ErrUnsupportedProviderContent)
	}
	input := root.object["input"]
	if input == nil {
		return nil, providerPayloadError(embeddingsProvider, ".input", ErrUnsupportedProviderPayload)
	}
	payload := newProviderPayload(embeddingsProvider, body)
	appendText := func(path string, node *jsonNode) error {
		if node.kind != jsonString || node.stringValue == "" {
			return providerPayloadError(embeddingsProvider, path, ErrUnsupportedProviderContent)
		}
		payload.addString("user", path, node)
		return nil
	}
	if input.kind == jsonArray {
		if len(input.array) == 0 {
			return nil, providerPayloadError(embeddingsProvider, ".input", ErrUnsupportedProviderContent)
		}
		for index, item := range input.array {
			if err := appendText(fmt.Sprintf(".input[%d]", index), item); err != nil {
				return nil, err
			}
		}
	} else if err := appendText(".input", input); err != nil {
		return nil, err
	}
	return payload, nil
}

func isEmbeddingsRequest(request ProcessingRequest) bool {
	path := request.RequestPath
	if path == "" && request.Stage == StageRequest {
		path = FirstHeader(request.Headers, ":path")
	}
	parsed, err := url.ParseRequestURI(path)
	return err == nil && (parsed.Path == "/v1/embeddings" || parsed.Path == "/embeddings")
}
