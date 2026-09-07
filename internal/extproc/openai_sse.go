package extproc

import (
	"encoding/json"
	"errors"
	"strings"
)

var (
	// ErrInvalidSSEEvent is returned when a complete SSE data event does not
	// contain valid JSON (excluding OpenAI's [DONE] sentinel).
	ErrInvalidSSEEvent = errors.New("invalid OpenAI SSE event")
	// ErrIncompleteSSEEvent is returned when Envoy signals end-of-stream while
	// an SSE line or event is still incomplete.
	ErrIncompleteSSEEvent = errors.New("incomplete OpenAI SSE event")
	// ErrSSEBufferLimit prevents malformed or unusually large SSE events from
	// retaining unbounded parser/window state for a stream.
	ErrSSEBufferLimit = errors.New("OpenAI SSE buffer limit exceeded")
)

// SSEParseError identifies a malformed SSE event without retaining its data.
// Event payloads may contain model output, so they must never be included in
// an error message or logged by callers.
type SSEParseError struct {
	Err error
}

func (e *SSEParseError) Error() string { return e.Err.Error() }
func (e *SSEParseError) Unwrap() error { return e.Err }

// OpenAISSEEvent is one complete OpenAI-compatible SSE event. DeltaContents
// contains the supported choices[].delta.content values in choice order. The
// parser deliberately does not apply guardrails, mutate bytes, or decide how
// an invalid event affects the stream.
type OpenAISSEEvent struct {
	Done          bool
	DeltaContents []string
	// Raw is the complete wire-format event, including its terminating blank
	// line. It is retained only in per-stream memory and is never logged.
	Raw       []byte
	dataLines []string
}

// OpenAISSEParser incrementally parses an SSE response delivered in arbitrary
// ext_proc response-body chunks. It is owned by one response stream and is not
// safe for concurrent use.
type OpenAISSEParser struct {
	pendingLine    string
	dataLines      []string
	eventOpen      bool
	rawEvent       strings.Builder
	maxBufferBytes int
}

// NewOpenAISSEParser creates a parser with a per-event buffer ceiling. A
// non-positive value keeps the parser useful for callers that do not need a
// limit; production ext-proc streams always set one.
func NewOpenAISSEParser(maxBufferBytes int) *OpenAISSEParser {
	return &OpenAISSEParser{maxBufferBytes: maxBufferBytes}
}

// Feed consumes a response-body chunk and returns each complete SSE event it
// contains. Chunks may split anywhere, including inside an SSE line or JSON
// string. A parse error is safe to surface because it contains no event data.
func (p *OpenAISSEParser) Feed(chunk []byte) ([]OpenAISSEEvent, error) {
	if p == nil {
		return nil, &SSEParseError{Err: ErrIncompleteSSEEvent}
	}
	p.pendingLine += string(chunk)
	var events []OpenAISSEEvent
	for {
		newline := strings.IndexByte(p.pendingLine, '\n')
		if newline < 0 {
			if p.maxBufferBytes > 0 && p.rawEvent.Len()+len(p.pendingLine) > p.maxBufferBytes {
				return nil, &SSEParseError{Err: ErrSSEBufferLimit}
			}
			return events, nil
		}
		rawLine := p.pendingLine[:newline+1]
		line := strings.TrimSuffix(p.pendingLine[:newline], "\r")
		p.pendingLine = p.pendingLine[newline+1:]
		event, complete, err := p.consumeLine(line, rawLine)
		if err != nil {
			return events, err
		}
		if complete {
			events = append(events, event)
		}
	}
}

// Finish must be called once Envoy marks the response body end-of-stream. A
// final empty line is required to terminate an SSE event; accepting a partial
// event here would hide truncated model output from the next streaming phase.
func (p *OpenAISSEParser) Finish() error {
	if p == nil || p.pendingLine != "" || p.eventOpen {
		return &SSEParseError{Err: ErrIncompleteSSEEvent}
	}
	return nil
}

func (p *OpenAISSEParser) consumeLine(line, rawLine string) (OpenAISSEEvent, bool, error) {
	if line == "" {
		if !p.eventOpen {
			return OpenAISSEEvent{}, false, nil
		}
		p.rawEvent.WriteString(rawLine)
		if p.maxBufferBytes > 0 && p.rawEvent.Len() > p.maxBufferBytes {
			return OpenAISSEEvent{}, false, &SSEParseError{Err: ErrSSEBufferLimit}
		}
		if len(p.dataLines) == 0 {
			event := OpenAISSEEvent{Raw: []byte(p.rawEvent.String())}
			p.rawEvent.Reset()
			p.eventOpen = false
			return event, true, nil
		}
		event, err := openAISSEEvent(p.dataLines, []byte(p.rawEvent.String()))
		p.dataLines = nil
		p.rawEvent.Reset()
		p.eventOpen = false
		return event, true, err
	}

	p.eventOpen = true
	p.rawEvent.WriteString(rawLine)
	if p.maxBufferBytes > 0 && p.rawEvent.Len() > p.maxBufferBytes {
		return OpenAISSEEvent{}, false, &SSEParseError{Err: ErrSSEBufferLimit}
	}
	if strings.HasPrefix(line, ":") { // SSE comment / keepalive
		return OpenAISSEEvent{}, false, nil
	}
	field, value, found := strings.Cut(line, ":")
	if !found {
		return OpenAISSEEvent{}, false, nil
	}
	if strings.HasPrefix(value, " ") {
		value = value[1:]
	}
	if field == "data" {
		p.dataLines = append(p.dataLines, value)
	}
	return OpenAISSEEvent{}, false, nil
}

func openAISSEEvent(dataLines []string, raw []byte) (OpenAISSEEvent, error) {
	payload := strings.Join(dataLines, "\n")
	if payload == "[DONE]" {
		return OpenAISSEEvent{Done: true, Raw: raw, dataLines: append([]string(nil), dataLines...)}, nil
	}
	var response struct {
		Choices []struct {
			Delta struct {
				Content *string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		return OpenAISSEEvent{}, &SSEParseError{Err: ErrInvalidSSEEvent}
	}
	event := OpenAISSEEvent{Raw: raw, dataLines: append([]string(nil), dataLines...)}
	for _, choice := range response.Choices {
		if choice.Delta.Content != nil {
			event.DeltaContents = append(event.DeltaContents, *choice.Delta.Content)
		}
	}
	return event, nil
}

// WithDeltaContents returns a semantically equivalent OpenAI SSE event with
// the supported delta content values replaced. OpenAI providers conventionally
// use one JSON data line per event; multi-line JSON is rejected rather than
// risk producing a malformed stream.
func (e OpenAISSEEvent) WithDeltaContents(contents []string) (OpenAISSEEvent, error) {
	if e.Done || len(e.DeltaContents) == 0 {
		return e, nil
	}
	if len(e.dataLines) != 1 || len(contents) != len(e.DeltaContents) {
		return OpenAISSEEvent{}, &SSEParseError{Err: ErrInvalidSSEEvent}
	}
	var response struct {
		Choices []struct {
			Delta struct {
				Content *string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(e.dataLines[0]), &response); err != nil {
		return OpenAISSEEvent{}, &SSEParseError{Err: ErrInvalidSSEEvent}
	}
	index := 0
	for choice := range response.Choices {
		if response.Choices[choice].Delta.Content != nil {
			value := contents[index]
			response.Choices[choice].Delta.Content = &value
			index++
		}
	}
	payload, err := json.Marshal(response)
	if err != nil {
		return OpenAISSEEvent{}, &SSEParseError{Err: ErrInvalidSSEEvent}
	}
	e.Raw = append([]byte("data: "), payload...)
	e.Raw = append(e.Raw, '\n', '\n')
	e.dataLines = []string{string(payload)}
	e.DeltaContents = append([]string(nil), contents...)
	return e, nil
}
