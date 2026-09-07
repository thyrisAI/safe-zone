package extproc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"sort"
	"strings"
)

// ChatRequestErrorKind classifies input errors without exposing request body
// content to callers or logs.
type ChatRequestErrorKind string

const (
	ChatRequestEmptyBody          ChatRequestErrorKind = "empty_body"
	ChatRequestUnsupportedType    ChatRequestErrorKind = "unsupported_content_type"
	ChatRequestInvalidJSON        ChatRequestErrorKind = "invalid_json"
	ChatRequestUnsupportedRequest ChatRequestErrorKind = "unsupported_request"
	ChatRequestUnsupportedContent ChatRequestErrorKind = "unsupported_content"
	ChatRequestInvalidMutation    ChatRequestErrorKind = "invalid_mutation"
)

var (
	ErrEmptyChatRequestBody       = errors.New("empty OpenAI chat request body")
	ErrUnsupportedChatContentType = errors.New("unsupported OpenAI chat request content type")
	ErrInvalidChatRequestJSON     = errors.New("invalid OpenAI chat request JSON")
	ErrUnsupportedChatRequest     = errors.New("unsupported OpenAI chat request")
	ErrUnsupportedChatContent     = errors.New("unsupported OpenAI chat message content")
	ErrInvalidChatMutation        = errors.New("invalid OpenAI chat request mutation")
)

// ChatRequestError is safe to return to a caller: it identifies the input
// class and location, but never includes raw request content.
type ChatRequestError struct {
	Kind         ChatRequestErrorKind
	MessageIndex int
	Path         string
	Err          error
}

func (e *ChatRequestError) Error() string {
	if e.MessageIndex >= 0 {
		return fmt.Sprintf("OpenAI chat request %s at messages[%d]%s: %v", e.Kind, e.MessageIndex, e.Path, e.Err)
	}
	return fmt.Sprintf("OpenAI chat request %s: %v", e.Kind, e.Err)
}

func (e *ChatRequestError) Unwrap() error { return e.Err }

type ChatResponseErrorKind string

const (
	ChatResponseEmptyBody           ChatResponseErrorKind = "empty_body"
	ChatResponseUnsupportedType     ChatResponseErrorKind = "unsupported_content_type"
	ChatResponseInvalidJSON         ChatResponseErrorKind = "invalid_json"
	ChatResponseUnsupportedResponse ChatResponseErrorKind = "unsupported_response"
	ChatResponseUnsupportedContent  ChatResponseErrorKind = "unsupported_content"
	ChatResponseInvalidMutation     ChatResponseErrorKind = "invalid_mutation"
)

var (
	ErrEmptyChatResponseBody          = errors.New("empty OpenAI chat response body")
	ErrUnsupportedChatResponseType    = errors.New("unsupported OpenAI chat response content type")
	ErrInvalidChatResponseJSON        = errors.New("invalid OpenAI chat response JSON")
	ErrUnsupportedChatResponse        = errors.New("unsupported OpenAI chat response")
	ErrUnsupportedChatResponseContent = errors.New("unsupported OpenAI chat response content")
	ErrInvalidChatResponseMutation    = errors.New("invalid OpenAI chat response mutation")
)

type ChatResponseError struct {
	Kind ChatResponseErrorKind
	Path string
	Err  error
}

func (e *ChatResponseError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("OpenAI chat response %s at %s: %v", e.Kind, e.Path, e.Err)
	}
	return fmt.Sprintf("OpenAI chat response %s: %v", e.Kind, e.Err)
}

func (e *ChatResponseError) Unwrap() error { return e.Err }

// ChatRequestContent identifies one supported mutable field in a Chat
// Completions request. JSONPath is stable for diagnostics and does not contain
// content.
type ChatRequestContent struct {
	ID           int
	MessageIndex int
	Role         string
	JSONPath     string
	Content      string
	valueStart   int
	valueEnd     int
}

// ChatContentMutation replaces exactly one request content field identified by
// ID. Mutations can only target entries returned by ParseChatRequest.
type ChatContentMutation struct {
	ID      int
	Content string
}

// ChatRequest is a gateway-neutral representation of the supported subset of
// an OpenAI Chat Completions request. Its raw body remains private so callers
// can only produce mutations through the checked method below.
type ChatRequest struct {
	Contents []ChatRequestContent
	body     []byte
}

// ChatAssistantContent identifies one supported assistant content field in a
// non-streaming Chat Completions response. JSONPath is safe for diagnostics and
// the source offsets allow a later masking phase to rewrite only this value.
type ChatAssistantContent struct {
	ID          int
	ChoiceIndex int
	JSONPath    string
	Content     string
	valueStart  int
	valueEnd    int
}

// ChatResponseContentMutation replaces exactly one response content field
// identified by ID. Mutations can only target entries returned by
// ParseChatResponse.
type ChatResponseContentMutation struct {
	ID      int
	Content string
}

type ChatResponse struct {
	AssistantContents []ChatAssistantContent
	body              []byte
	root              *jsonNode
}

func chatResponseError(kind ChatResponseErrorKind, path string, err error) *ChatResponseError {
	return &ChatResponseError{
		Kind: kind,
		Path: path,
		Err:  err,
	}
}

func ParseChatResponse(contentType string, body []byte) (*ChatResponse, error) {
	if !isJSONContentType(contentType) {
		return nil, chatResponseError(
			ChatResponseUnsupportedType,
			"",
			ErrUnsupportedChatResponseType,
		)
	}

	if len(bytes.TrimSpace(body)) == 0 {
		return nil, chatResponseError(
			ChatResponseEmptyBody,
			"",
			ErrEmptyChatResponseBody,
		)
	}

	parser := jsonSourceParser{source: body}
	root, err := parser.parseDocument()
	if err != nil {
		return nil, chatResponseError(
			ChatResponseInvalidJSON,
			"",
			fmt.Errorf("%w: %v", ErrInvalidChatResponseJSON, err),
		)
	}
	if root.kind != jsonObject {
		return nil, chatResponseError(
			ChatResponseUnsupportedResponse,
			"",
			ErrUnsupportedChatResponse,
		)
	}
	choices := root.object["choices"]
	if choices == nil || choices.kind != jsonArray {
		return nil, chatResponseError(
			ChatResponseUnsupportedResponse,
			".choices",
			ErrUnsupportedChatResponse,
		)
	}

	response := &ChatResponse{
		body: append([]byte(nil), body...),
		root: root,
	}
	for index, choice := range choices.array {
		path := fmt.Sprintf(".choices[%d].message.content", index)
		if choice.kind != jsonObject {
			return nil, chatResponseError(ChatResponseUnsupportedResponse, fmt.Sprintf(".choices[%d]", index), ErrUnsupportedChatResponse)
		}
		message := choice.object["message"]
		if message == nil || message.kind != jsonObject {
			return nil, chatResponseError(ChatResponseUnsupportedResponse, fmt.Sprintf(".choices[%d].message", index), ErrUnsupportedChatResponse)
		}
		role := message.object["role"]
		if role == nil || role.kind != jsonString || role.stringValue != "assistant" {
			return nil, chatResponseError(ChatResponseUnsupportedResponse, fmt.Sprintf(".choices[%d].message.role", index), ErrUnsupportedChatResponse)
		}
		content := message.object["content"]
		toolCalls := message.object["tool_calls"]
		refusal := message.object["refusal"]
		hasRefusalContent := false
		if refusal != nil {
			refusalPath := fmt.Sprintf(".choices[%d].message.refusal", index)
			switch {
			case refusal.kind == jsonString:
				hasRefusalContent = true
				response.AssistantContents = append(response.AssistantContents, ChatAssistantContent{
					ID:          len(response.AssistantContents),
					ChoiceIndex: index,
					JSONPath:    refusalPath,
					Content:     refusal.stringValue,
					valueStart:  refusal.start,
					valueEnd:    refusal.end,
				})
			case !isJSONNull(refusal, body):
				return nil, chatResponseError(ChatResponseUnsupportedContent, refusalPath, ErrUnsupportedChatResponseContent)
			}
		}
		if content != nil && content.kind == jsonString {
			response.AssistantContents = append(response.AssistantContents, ChatAssistantContent{
				ID:          len(response.AssistantContents),
				ChoiceIndex: index,
				JSONPath:    path,
				Content:     content.stringValue,
				valueStart:  content.start,
				valueEnd:    content.end,
			})
		} else if content != nil && content.kind == jsonArray {
			if err := appendChatResponseContentParts(response, index, content); err != nil {
				return nil, err
			}
		} else if content != nil && !((toolCalls != nil || hasRefusalContent) && isJSONNull(content, body)) {
			return nil, chatResponseError(ChatResponseUnsupportedContent, path, ErrUnsupportedChatResponseContent)
		}
		if toolCalls != nil {
			if err := appendChatResponseToolCalls(response, index, toolCalls); err != nil {
				return nil, err
			}
		}
		if content == nil && toolCalls == nil && !hasRefusalContent {
			return nil, chatResponseError(ChatResponseUnsupportedContent, path, ErrUnsupportedChatResponseContent)
		}
	}

	return response, nil
}

func appendChatResponseContentParts(response *ChatResponse, choiceIndex int, content *jsonNode) error {
	if len(content.array) == 0 {
		return chatResponseError(ChatResponseUnsupportedContent, fmt.Sprintf(".choices[%d].message.content", choiceIndex), ErrUnsupportedChatResponseContent)
	}
	for contentIndex, part := range content.array {
		base := fmt.Sprintf(".choices[%d].message.content[%d]", choiceIndex, contentIndex)
		if part.kind != jsonObject {
			return chatResponseError(ChatResponseUnsupportedContent, base, ErrUnsupportedChatResponseContent)
		}
		partType := part.object["type"]
		if partType == nil || partType.kind != jsonString {
			return chatResponseError(ChatResponseUnsupportedContent, base+".type", ErrUnsupportedChatResponseContent)
		}
		var field string
		switch partType.stringValue {
		case "text":
			field = "text"
		case "refusal":
			field = "refusal"
		default:
			return chatResponseError(ChatResponseUnsupportedContent, base+".type", ErrUnsupportedChatResponseContent)
		}
		value := part.object[field]
		if value == nil || value.kind != jsonString {
			return chatResponseError(ChatResponseUnsupportedContent, base+"."+field, ErrUnsupportedChatResponseContent)
		}
		response.AssistantContents = append(response.AssistantContents, ChatAssistantContent{
			ID: len(response.AssistantContents), ChoiceIndex: choiceIndex,
			JSONPath: base + "." + field, Content: value.stringValue,
			valueStart: value.start, valueEnd: value.end,
		})
	}
	return nil
}

// Mutate serializes response-content replacements safely while retaining the
// exact original bytes for all unknown fields, choice ordering and untouched
// JSON values.
func (r *ChatResponse) Mutate(mutations []ChatResponseContentMutation) ([]byte, error) {
	if r == nil {
		return nil, chatResponseError(ChatResponseInvalidMutation, "", ErrInvalidChatResponseMutation)
	}
	if len(mutations) == 0 {
		return append([]byte(nil), r.body...), nil
	}
	targets := make(map[int]ChatAssistantContent, len(r.AssistantContents))
	for _, target := range r.AssistantContents {
		targets[target.ID] = target
	}
	replacements := make([]sourceReplacement, 0, len(mutations))
	seen := make(map[int]struct{}, len(mutations))
	for _, mutation := range mutations {
		if _, exists := seen[mutation.ID]; exists {
			return nil, chatResponseError(ChatResponseInvalidMutation, "", ErrInvalidChatResponseMutation)
		}
		target, found := targets[mutation.ID]
		if !found {
			return nil, chatResponseError(ChatResponseInvalidMutation, "", ErrInvalidChatResponseMutation)
		}
		encoded, err := json.Marshal(mutation.Content)
		if err != nil {
			return nil, chatResponseError(ChatResponseInvalidMutation, target.JSONPath, fmt.Errorf("%w: %v", ErrInvalidChatResponseMutation, err))
		}
		seen[mutation.ID] = struct{}{}
		replacements = append(replacements, sourceReplacement{start: target.valueStart, end: target.valueEnd, value: encoded})
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start < replacements[j].start })
	result := make([]byte, 0, len(r.body))
	cursor := 0
	for _, replacement := range replacements {
		result = append(result, r.body[cursor:replacement.start]...)
		result = append(result, replacement.value...)
		cursor = replacement.end
	}
	return append(result, r.body[cursor:]...), nil
}

// ParseChatRequest accepts only an application/json OpenAI Chat Completions
// request. It extracts supported string and multimodal text content fields from
// scanned roles and preserves enough source offsets to safely rewrite only
// those JSON string values later.
func ParseChatRequest(contentType string, body []byte) (*ChatRequest, error) {
	if !isJSONContentType(contentType) {
		return nil, chatRequestError(ChatRequestUnsupportedType, -1, "", ErrUnsupportedChatContentType)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, chatRequestError(ChatRequestEmptyBody, -1, "", ErrEmptyChatRequestBody)
	}
	parser := jsonSourceParser{source: body}
	root, err := parser.parseDocument()
	if err != nil {
		return nil, chatRequestError(ChatRequestInvalidJSON, -1, "", fmt.Errorf("%w: %v", ErrInvalidChatRequestJSON, err))
	}
	if root.kind != jsonObject {
		return nil, chatRequestError(ChatRequestUnsupportedRequest, -1, "", ErrUnsupportedChatRequest)
	}
	// stream controls the upstream response representation. Request-side
	// extraction and mutation are identical for streamed and buffered Chat
	// Completions requests; response processing chooses the SSE path later.
	messages := root.object["messages"]
	if messages == nil || messages.kind != jsonArray {
		return nil, chatRequestError(ChatRequestUnsupportedRequest, -1, ".messages", ErrUnsupportedChatRequest)
	}

	request := &ChatRequest{body: append([]byte(nil), body...)}
	for index, message := range messages.array {
		path := fmt.Sprintf(".messages[%d].content", index)
		if message.kind != jsonObject {
			return nil, chatRequestError(ChatRequestUnsupportedRequest, index, "", ErrUnsupportedChatRequest)
		}
		role := message.object["role"]
		if role == nil || role.kind != jsonString || !isScannedChatRequestRole(role.stringValue) {
			if message.object["tool_calls"] != nil || message.object["tool_call_id"] != nil || message.object["refusal"] != nil {
				return nil, chatRequestError(ChatRequestUnsupportedContent, index, "", ErrUnsupportedChatContent)
			}
			continue
		}
		if role.stringValue == "tool" {
			toolCallID := message.object["tool_call_id"]
			if toolCallID == nil || toolCallID.kind != jsonString {
				return nil, chatRequestError(ChatRequestUnsupportedContent, index, ".tool_call_id", ErrUnsupportedChatContent)
			}
		}
		content := message.object["content"]
		toolCalls := message.object["tool_calls"]
		refusal := message.object["refusal"]
		if toolCalls != nil && role.stringValue != "assistant" {
			return nil, chatRequestError(ChatRequestUnsupportedContent, index, ".tool_calls", ErrUnsupportedChatContent)
		}
		hasRefusalContent := false
		if refusal != nil {
			if role.stringValue != "assistant" {
				return nil, chatRequestError(ChatRequestUnsupportedContent, index, ".refusal", ErrUnsupportedChatContent)
			}
			switch {
			case refusal.kind == jsonString:
				hasRefusalContent = true
				request.addContent(index, role.stringValue, fmt.Sprintf(".messages[%d].refusal", index), refusal)
			case !isJSONNull(refusal, body):
				return nil, chatRequestError(ChatRequestUnsupportedContent, index, ".refusal", ErrUnsupportedChatContent)
			}
		}
		if content != nil && content.kind == jsonString {
			request.addContent(index, role.stringValue, path, content)
		} else if content != nil && content.kind == jsonArray {
			if err := request.appendContentParts(index, role.stringValue, content); err != nil {
				return nil, err
			}
		} else if content != nil && !(role.stringValue == "assistant" && (toolCalls != nil || hasRefusalContent) && isJSONNull(content, body)) {
			return nil, chatRequestError(ChatRequestUnsupportedContent, index, ".content", ErrUnsupportedChatContent)
		}
		if role.stringValue == "assistant" && toolCalls != nil {
			if err := request.appendToolCalls(index, toolCalls); err != nil {
				return nil, err
			}
		}
		if content == nil && toolCalls == nil && !hasRefusalContent {
			return nil, chatRequestError(ChatRequestUnsupportedContent, index, ".content", ErrUnsupportedChatContent)
		}
	}
	return request, nil
}

func (r *ChatRequest) appendContentParts(messageIndex int, role string, content *jsonNode) error {
	if len(content.array) == 0 {
		return chatRequestError(ChatRequestUnsupportedContent, messageIndex, ".content", ErrUnsupportedChatContent)
	}
	for contentIndex, part := range content.array {
		base := fmt.Sprintf(".messages[%d].content[%d]", messageIndex, contentIndex)
		if part.kind != jsonObject {
			return chatRequestError(ChatRequestUnsupportedContent, messageIndex, fmt.Sprintf(".content[%d]", contentIndex), ErrUnsupportedChatContent)
		}
		partType := part.object["type"]
		if partType == nil || partType.kind != jsonString {
			return chatRequestError(ChatRequestUnsupportedContent, messageIndex, fmt.Sprintf(".content[%d].type", contentIndex), ErrUnsupportedChatContent)
		}
		switch partType.stringValue {
		case "text":
			text := part.object["text"]
			if text == nil || text.kind != jsonString {
				return chatRequestError(ChatRequestUnsupportedContent, messageIndex, fmt.Sprintf(".content[%d].text", contentIndex), ErrUnsupportedChatContent)
			}
			r.addContent(messageIndex, role, base+".text", text)
		case "refusal":
			if role != "assistant" {
				return chatRequestError(ChatRequestUnsupportedContent, messageIndex, fmt.Sprintf(".content[%d].type", contentIndex), ErrUnsupportedChatContent)
			}
			refusal := part.object["refusal"]
			if refusal == nil || refusal.kind != jsonString {
				return chatRequestError(ChatRequestUnsupportedContent, messageIndex, fmt.Sprintf(".content[%d].refusal", contentIndex), ErrUnsupportedChatContent)
			}
			r.addContent(messageIndex, role, base+".refusal", refusal)
		case "image_url", "input_audio", "file":
			if role != "user" || part.object["text"] != nil || part.object["refusal"] != nil {
				return chatRequestError(ChatRequestUnsupportedContent, messageIndex, fmt.Sprintf(".content[%d]", contentIndex), ErrUnsupportedChatContent)
			}
		default:
			return chatRequestError(ChatRequestUnsupportedContent, messageIndex, fmt.Sprintf(".content[%d].type", contentIndex), ErrUnsupportedChatContent)
		}
	}
	return nil
}

func appendChatResponseToolCalls(response *ChatResponse, choiceIndex int, toolCalls *jsonNode) error {
	if toolCalls.kind != jsonArray {
		return chatResponseError(ChatResponseUnsupportedContent, fmt.Sprintf(".choices[%d].message.tool_calls", choiceIndex), ErrUnsupportedChatResponseContent)
	}
	for toolIndex, toolCall := range toolCalls.array {
		base := fmt.Sprintf(".choices[%d].message.tool_calls[%d]", choiceIndex, toolIndex)
		if toolCall.kind != jsonObject {
			return chatResponseError(ChatResponseUnsupportedContent, base, ErrUnsupportedChatResponseContent)
		}
		id, callType := toolCall.object["id"], toolCall.object["type"]
		if id == nil || id.kind != jsonString {
			return chatResponseError(ChatResponseUnsupportedContent, base+".id", ErrUnsupportedChatResponseContent)
		}
		if callType == nil || callType.kind != jsonString || callType.stringValue != "function" {
			return chatResponseError(ChatResponseUnsupportedContent, base+".type", ErrUnsupportedChatResponseContent)
		}
		function := toolCall.object["function"]
		if function == nil || function.kind != jsonObject {
			return chatResponseError(ChatResponseUnsupportedContent, base+".function", ErrUnsupportedChatResponseContent)
		}
		name, arguments := function.object["name"], function.object["arguments"]
		if name == nil || name.kind != jsonString {
			return chatResponseError(ChatResponseUnsupportedContent, base+".function.name", ErrUnsupportedChatResponseContent)
		}
		if arguments == nil || arguments.kind != jsonString {
			return chatResponseError(ChatResponseUnsupportedContent, base+".function.arguments", ErrUnsupportedChatResponseContent)
		}
		response.AssistantContents = append(response.AssistantContents, ChatAssistantContent{
			ID: len(response.AssistantContents), ChoiceIndex: choiceIndex,
			JSONPath: base + ".function.arguments", Content: arguments.stringValue,
			valueStart: arguments.start, valueEnd: arguments.end,
		})
	}
	return nil
}

func isScannedChatRequestRole(role string) bool {
	return role == "developer" || role == "system" || role == "user" || role == "assistant" || role == "tool"
}

func (r *ChatRequest) addContent(messageIndex int, role, path string, node *jsonNode) {
	r.Contents = append(r.Contents, ChatRequestContent{
		ID: len(r.Contents), MessageIndex: messageIndex, Role: role, JSONPath: path,
		Content: node.stringValue, valueStart: node.start, valueEnd: node.end,
	})
}

func (r *ChatRequest) appendToolCalls(messageIndex int, toolCalls *jsonNode) error {
	if toolCalls.kind != jsonArray {
		return chatRequestError(ChatRequestUnsupportedContent, messageIndex, ".tool_calls", ErrUnsupportedChatContent)
	}
	for toolIndex, toolCall := range toolCalls.array {
		base := fmt.Sprintf(".messages[%d].tool_calls[%d]", messageIndex, toolIndex)
		if toolCall.kind != jsonObject {
			return chatRequestError(ChatRequestUnsupportedContent, messageIndex, fmt.Sprintf(".tool_calls[%d]", toolIndex), ErrUnsupportedChatContent)
		}
		id, callType := toolCall.object["id"], toolCall.object["type"]
		if id == nil || id.kind != jsonString {
			return chatRequestError(ChatRequestUnsupportedContent, messageIndex, fmt.Sprintf(".tool_calls[%d].id", toolIndex), ErrUnsupportedChatContent)
		}
		if callType == nil || callType.kind != jsonString || callType.stringValue != "function" {
			return chatRequestError(ChatRequestUnsupportedContent, messageIndex, fmt.Sprintf(".tool_calls[%d].type", toolIndex), ErrUnsupportedChatContent)
		}
		function := toolCall.object["function"]
		if function == nil || function.kind != jsonObject {
			return chatRequestError(ChatRequestUnsupportedContent, messageIndex, fmt.Sprintf(".tool_calls[%d].function", toolIndex), ErrUnsupportedChatContent)
		}
		name, arguments := function.object["name"], function.object["arguments"]
		if name == nil || name.kind != jsonString {
			return chatRequestError(ChatRequestUnsupportedContent, messageIndex, fmt.Sprintf(".tool_calls[%d].function.name", toolIndex), ErrUnsupportedChatContent)
		}
		if arguments == nil || arguments.kind != jsonString {
			return chatRequestError(ChatRequestUnsupportedContent, messageIndex, fmt.Sprintf(".tool_calls[%d].function.arguments", toolIndex), ErrUnsupportedChatContent)
		}
		r.addContent(messageIndex, "tool_call", base+".function.arguments", arguments)
	}
	return nil
}

// Mutate serializes replacements safely while retaining the exact original
// bytes for all unknown fields, message ordering and untouched JSON values.
func (r *ChatRequest) Mutate(mutations []ChatContentMutation) ([]byte, error) {
	if r == nil {
		return nil, chatRequestError(ChatRequestInvalidMutation, -1, "", ErrInvalidChatMutation)
	}
	if len(mutations) == 0 {
		return append([]byte(nil), r.body...), nil
	}
	targets := make(map[int]ChatRequestContent, len(r.Contents))
	for _, target := range r.Contents {
		targets[target.ID] = target
	}
	replacements := make([]sourceReplacement, 0, len(mutations))
	seen := make(map[int]struct{}, len(mutations))
	for _, mutation := range mutations {
		if _, exists := seen[mutation.ID]; exists {
			return nil, chatRequestError(ChatRequestInvalidMutation, -1, "", ErrInvalidChatMutation)
		}
		target, found := targets[mutation.ID]
		if !found {
			return nil, chatRequestError(ChatRequestInvalidMutation, -1, "", ErrInvalidChatMutation)
		}
		encoded, err := json.Marshal(mutation.Content)
		if err != nil {
			return nil, chatRequestError(ChatRequestInvalidMutation, target.MessageIndex, target.JSONPath, fmt.Errorf("%w: %v", ErrInvalidChatMutation, err))
		}
		seen[mutation.ID] = struct{}{}
		replacements = append(replacements, sourceReplacement{start: target.valueStart, end: target.valueEnd, value: encoded})
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start < replacements[j].start })
	result := make([]byte, 0, len(r.body))
	cursor := 0
	for _, replacement := range replacements {
		result = append(result, r.body[cursor:replacement.start]...)
		result = append(result, replacement.value...)
		cursor = replacement.end
	}
	return append(result, r.body[cursor:]...), nil
}

func isJSONNull(node *jsonNode, body []byte) bool {
	return node != nil && node.start >= 0 && node.end <= len(body) && string(body[node.start:node.end]) == "null"
}

func isJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func chatRequestError(kind ChatRequestErrorKind, messageIndex int, path string, err error) *ChatRequestError {
	return &ChatRequestError{Kind: kind, MessageIndex: messageIndex, Path: path, Err: err}
}

type jsonNodeKind uint8

const (
	jsonObject jsonNodeKind = iota
	jsonArray
	jsonString
	jsonBoolean
	jsonOther
)

type jsonNode struct {
	kind          jsonNodeKind
	start, end    int
	stringValue   string
	boolean       bool
	object        map[string]*jsonNode
	duplicateKeys bool
	array         []*jsonNode
}

// jsonSourceParser validates JSON while retaining byte offsets. It is limited
// to the JSON grammar; it has no OpenAI- or gateway-specific dependency.
type jsonSourceParser struct {
	source []byte
	pos    int
}

func (p *jsonSourceParser) parseDocument() (*jsonNode, error) {
	p.skipSpace()
	node, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos != len(p.source) {
		return nil, fmt.Errorf("unexpected trailing data")
	}
	return node, nil
}

func (p *jsonSourceParser) parseValue() (*jsonNode, error) {
	p.skipSpace()
	if p.pos >= len(p.source) {
		return nil, fmt.Errorf("unexpected end of input")
	}
	switch p.source[p.pos] {
	case '{':
		return p.parseObject()
	case '[':
		return p.parseArray()
	case '"':
		return p.parseStringNode()
	case 't', 'f':
		return p.parseBoolean()
	case 'n':
		return p.parseLiteral("null", jsonOther)
	default:
		return p.parseNumber()
	}
}

func (p *jsonSourceParser) parseObject() (*jsonNode, error) {
	start := p.pos
	p.pos++
	node := &jsonNode{kind: jsonObject, start: start, object: make(map[string]*jsonNode)}
	p.skipSpace()
	if p.consume('}') {
		node.end = p.pos
		return node, nil
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.source) || p.source[p.pos] != '"' {
			return nil, fmt.Errorf("object key must be a string")
		}
		key, err := p.parseStringNode()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if !p.consume(':') {
			return nil, fmt.Errorf("object key is missing colon")
		}
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		if _, exists := node.object[key.stringValue]; exists {
			node.duplicateKeys = true
		}
		node.object[key.stringValue] = value
		p.skipSpace()
		if p.consume('}') {
			node.end = p.pos
			return node, nil
		}
		if !p.consume(',') {
			return nil, fmt.Errorf("object is missing comma")
		}
	}
}

func (p *jsonSourceParser) parseArray() (*jsonNode, error) {
	start := p.pos
	p.pos++
	node := &jsonNode{kind: jsonArray, start: start}
	p.skipSpace()
	if p.consume(']') {
		node.end = p.pos
		return node, nil
	}
	for {
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		node.array = append(node.array, value)
		p.skipSpace()
		if p.consume(']') {
			node.end = p.pos
			return node, nil
		}
		if !p.consume(',') {
			return nil, fmt.Errorf("array is missing comma")
		}
	}
}

func (p *jsonSourceParser) parseStringNode() (*jsonNode, error) {
	start := p.pos
	p.pos++
	escaped := false
	for p.pos < len(p.source) {
		character := p.source[p.pos]
		p.pos++
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			raw := p.source[start:p.pos]
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
			return &jsonNode{kind: jsonString, start: start, end: p.pos, stringValue: value}, nil
		}
		if character < 0x20 {
			return nil, fmt.Errorf("unescaped control character in string")
		}
	}
	return nil, fmt.Errorf("unterminated string")
}

func (p *jsonSourceParser) parseBoolean() (*jsonNode, error) {
	if bytes.HasPrefix(p.source[p.pos:], []byte("true")) {
		return p.parseLiteral("true", jsonBoolean)
	}
	if bytes.HasPrefix(p.source[p.pos:], []byte("false")) {
		node, err := p.parseLiteral("false", jsonBoolean)
		if node != nil {
			node.boolean = false
		}
		return node, err
	}
	return nil, fmt.Errorf("invalid boolean")
}

func (p *jsonSourceParser) parseLiteral(literal string, kind jsonNodeKind) (*jsonNode, error) {
	if !bytes.HasPrefix(p.source[p.pos:], []byte(literal)) {
		return nil, fmt.Errorf("invalid literal")
	}
	start := p.pos
	p.pos += len(literal)
	node := &jsonNode{kind: kind, start: start, end: p.pos}
	if literal == "true" {
		node.boolean = true
	}
	return node, nil
}

func (p *jsonSourceParser) parseNumber() (*jsonNode, error) {
	start := p.pos
	for p.pos < len(p.source) && !isJSONDelimiter(p.source[p.pos]) {
		p.pos++
	}
	if start == p.pos {
		return nil, fmt.Errorf("invalid value")
	}
	if !json.Valid(p.source[start:p.pos]) {
		return nil, fmt.Errorf("invalid number")
	}
	return &jsonNode{kind: jsonOther, start: start, end: p.pos}, nil
}

func (p *jsonSourceParser) skipSpace() {
	for p.pos < len(p.source) && (p.source[p.pos] == ' ' || p.source[p.pos] == '\n' || p.source[p.pos] == '\r' || p.source[p.pos] == '\t') {
		p.pos++
	}
}

func (p *jsonSourceParser) consume(want byte) bool {
	if p.pos < len(p.source) && p.source[p.pos] == want {
		p.pos++
		return true
	}
	return false
}

func isJSONDelimiter(value byte) bool {
	return value == ' ' || value == '\n' || value == '\r' || value == '\t' || value == ',' || value == ']' || value == '}'
}
