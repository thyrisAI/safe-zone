package extproc

import (
	"errors"
	"strings"
	"testing"
)

func TestOpenAISSEParserExtractsDeltasAcrossExtProcBodyChunks(t *testing.T) {
	parser := &OpenAISSEParser{}
	chunks := [][]byte{
		[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hel"),
		[]byte("lo\"}},{\"delta\":{\"content\":\" world\"}}]}\n\n"),
		[]byte(": keepalive\n\ndata: [DONE]\n\n"),
	}
	var events []OpenAISSEEvent
	for _, chunk := range chunks {
		parsed, err := parser.Feed(chunk)
		if err != nil {
			t.Fatalf("Feed(%q) error = %v", chunk, err)
		}
		events = append(events, parsed...)
	}
	if err := parser.Finish(); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %+v, want content event, keepalive, and DONE", events)
	}
	if got, want := events[0].DeltaContents, []string{"hello", " world"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("delta contents = %q, want %q", got, want)
	}
	if len(events[1].Raw) == 0 || events[1].Done || len(events[1].DeltaContents) != 0 {
		t.Fatalf("keepalive event = %+v", events[1])
	}
	if !events[2].Done || len(events[2].DeltaContents) != 0 {
		t.Fatalf("DONE event = %+v", events[2])
	}
}

func TestOpenAISSEParserPreservesNonContentEvents(t *testing.T) {
	parser := &OpenAISSEParser{}
	events, err := parser.Feed([]byte("event: ping\ndata: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"))
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if len(events) != 1 || events[0].Done || len(events[0].DeltaContents) != 0 {
		t.Fatalf("events = %+v, want one non-content event", events)
	}
}

func TestOpenAISSEParserReturnsSafeErrors(t *testing.T) {
	t.Run("invalid JSON", func(t *testing.T) {
		parser := &OpenAISSEParser{}
		_, err := parser.Feed([]byte("data: {not json}\n\n"))
		if !errors.Is(err, ErrInvalidSSEEvent) {
			t.Fatalf("Feed() error = %v, want ErrInvalidSSEEvent", err)
		}
		if err.Error() != ErrInvalidSSEEvent.Error() {
			t.Fatalf("error = %q, must not include event data", err)
		}
	})
	t.Run("end of stream with partial event", func(t *testing.T) {
		parser := &OpenAISSEParser{}
		if _, err := parser.Feed([]byte("data: {\"choices\":[]}")); err != nil {
			t.Fatalf("Feed() error = %v", err)
		}
		if err := parser.Finish(); !errors.Is(err, ErrIncompleteSSEEvent) {
			t.Fatalf("Finish() error = %v, want ErrIncompleteSSEEvent", err)
		}
	})
}

func TestOpenAISSEParserEnforcesConfiguredBufferLimit(t *testing.T) {
	parser := NewOpenAISSEParser(16)
	_, err := parser.Feed([]byte("data: this line is too long"))
	if !errors.Is(err, ErrSSEBufferLimit) {
		t.Fatalf("Feed() error = %v, want ErrSSEBufferLimit", err)
	}
	if err.Error() != ErrSSEBufferLimit.Error() {
		t.Fatalf("Feed() error = %q, must not contain SSE data", err)
	}
}

func TestOpenAISSEEventWithDeltaContentsRewritesOnlyDelta(t *testing.T) {
	parser := &OpenAISSEParser{}
	events, err := parser.Feed([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"secret\"},\"index\":0}]}\n\n"))
	if err != nil || len(events) != 1 {
		t.Fatalf("Feed() = (%+v, %v)", events, err)
	}
	rewritten, err := events[0].WithDeltaContents([]string{"[MASKED]"})
	if err != nil {
		t.Fatalf("WithDeltaContents() error = %v", err)
	}
	if got := string(rewritten.Raw); !strings.Contains(got, "[MASKED]") || !strings.HasPrefix(got, "data: ") || !strings.HasSuffix(got, "\n\n") {
		t.Fatalf("rewritten SSE = %q", got)
	}
}
