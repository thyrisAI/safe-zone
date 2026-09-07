package extproc

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

var (
	ErrUnsupportedProviderPayload = errors.New("unsupported provider payload")
	ErrUnsupportedProviderContent = errors.New("unsupported provider content")
	ErrInvalidProviderMutation    = errors.New("invalid provider payload mutation")
)

type ProviderPayloadError struct {
	Provider string
	Path     string
	Err      error
}

func (e *ProviderPayloadError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s payload at %s: %v", e.Provider, e.Path, e.Err)
	}
	return fmt.Sprintf("%s payload: %v", e.Provider, e.Err)
}

func (e *ProviderPayloadError) Unwrap() error { return e.Err }

func providerPayloadError(provider, path string, err error) *ProviderPayloadError {
	return &ProviderPayloadError{Provider: provider, Path: path, Err: err}
}

type ProviderTextContent struct {
	ID       int
	Role     string
	JSONPath string
	Content  string
	start    int
	end      int
	rawJSON  bool
}

type ProviderContentMutation struct {
	ID      int
	Content string
}

type ProviderPayload struct {
	Provider string
	Contents []ProviderTextContent
	body     []byte
}

func newProviderPayload(provider string, body []byte) *ProviderPayload {
	return &ProviderPayload{Provider: provider, body: append([]byte(nil), body...)}
}

func (p *ProviderPayload) addString(role, path string, node *jsonNode) {
	p.Contents = append(p.Contents, ProviderTextContent{
		ID: len(p.Contents), Role: role, JSONPath: path, Content: node.stringValue,
		start: node.start, end: node.end,
	})
}

func (p *ProviderPayload) addJSONObject(role, path string, node *jsonNode) {
	p.Contents = append(p.Contents, ProviderTextContent{
		ID: len(p.Contents), Role: role, JSONPath: path,
		Content: string(p.body[node.start:node.end]), start: node.start, end: node.end, rawJSON: true,
	})
}

func (p *ProviderPayload) Mutate(mutations []ProviderContentMutation) ([]byte, error) {
	if p == nil {
		return nil, providerPayloadError("provider", "", ErrInvalidProviderMutation)
	}
	if len(mutations) == 0 {
		return append([]byte(nil), p.body...), nil
	}
	targets := make(map[int]ProviderTextContent, len(p.Contents))
	for _, target := range p.Contents {
		targets[target.ID] = target
	}
	seen := make(map[int]struct{}, len(mutations))
	replacements := make([]sourceReplacement, 0, len(mutations))
	for _, mutation := range mutations {
		if _, duplicate := seen[mutation.ID]; duplicate {
			return nil, providerPayloadError(p.Provider, "", ErrInvalidProviderMutation)
		}
		target, ok := targets[mutation.ID]
		if !ok {
			return nil, providerPayloadError(p.Provider, "", ErrInvalidProviderMutation)
		}
		var encoded []byte
		if target.rawJSON {
			parser := jsonSourceParser{source: []byte(mutation.Content)}
			node, err := parser.parseDocument()
			if err != nil || node.kind != jsonObject {
				return nil, providerPayloadError(p.Provider, target.JSONPath, ErrInvalidProviderMutation)
			}
			encoded = []byte(mutation.Content)
		} else {
			encoded, _ = json.Marshal(mutation.Content)
		}
		seen[mutation.ID] = struct{}{}
		replacements = append(replacements, sourceReplacement{start: target.start, end: target.end, value: encoded})
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start < replacements[j].start })
	result := make([]byte, 0, len(p.body))
	cursor := 0
	for _, replacement := range replacements {
		if replacement.start < cursor || replacement.end < replacement.start || replacement.end > len(p.body) {
			return nil, providerPayloadError(p.Provider, "", ErrInvalidProviderMutation)
		}
		result = append(result, p.body[cursor:replacement.start]...)
		result = append(result, replacement.value...)
		cursor = replacement.end
	}
	return append(result, p.body[cursor:]...), nil
}

func parseProviderDocument(provider, contentType string, body []byte) (*jsonNode, error) {
	if !isJSONContentType(contentType) {
		return nil, providerPayloadError(provider, "", ErrUnsupportedProviderContent)
	}
	parser := jsonSourceParser{source: body}
	root, err := parser.parseDocument()
	if err != nil {
		return nil, providerPayloadError(provider, "", fmt.Errorf("%w: invalid JSON", ErrUnsupportedProviderContent))
	}
	if root.kind != jsonObject {
		return nil, providerPayloadError(provider, "", ErrUnsupportedProviderPayload)
	}
	return root, nil
}
