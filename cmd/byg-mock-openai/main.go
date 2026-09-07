// byg-mock-openai is a test-only upstream for the BYG examples. It retains
// no prompt content: inspection exposes only a request counter, byte count,
// SHA-256 digest, and whether a TSZ mask placeholder was observed.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

type observation struct {
	Sequence               int    `json:"sequence"`
	Bytes                  int    `json:"bytes"`
	SHA256                 string `json:"sha256"`
	Masked                 bool   `json:"masked"`
	ContainsSyntheticEmail bool   `json:"contains_synthetic_email"`
}

type server struct {
	mu   sync.RWMutex
	last observation
}

func main() {
	s := &server{}
	http.HandleFunc("/v1/chat/completions", s.chat)
	http.HandleFunc("/v1/responses", s.responses)
	http.HandleFunc("/inspect", s.inspect)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func (s *server) chat(w http.ResponseWriter, r *http.Request) {
	if !s.recordRequest(w, r) {
		return
	}
	if os.Getenv("BYG_MOCK_RESPONSE_MODE") == "sse" {
		s.writeSSEFixture(w)
		return
	}
	content := mockResponseContent()
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "chatcmpl-kind-mock", "object": "chat.completion", "choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": content}}}})
}

func (s *server) responses(w http.ResponseWriter, r *http.Request) {
	if !s.recordRequest(w, r) {
		return
	}
	// Responses SSE events have a different event schema and are intentionally
	// not simulated until the Responses streaming Phase 6 item is implemented.
	content := mockResponseContent()
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "resp-kind-mock", "object": "response",
		"output": []any{map[string]any{
			"id": "msg-kind-mock", "type": "message", "role": "assistant",
			"content": []any{map[string]string{"type": "output_text", "text": content}},
		}},
	})
}

func (s *server) recordRequest(w http.ResponseWriter, r *http.Request) bool {
	// Kubernetes readiness probes use GET. They must not be indistinguishable
	// from a client request in the test-only upstream observation.
	if r.Method != http.MethodPost {
		return true
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		http.Error(w, `{"error":"request too large"}`, http.StatusRequestEntityTooLarge)
		return false
	}
	digest := sha256.Sum256(body)
	s.mu.Lock()
	s.last = observation{
		Sequence: s.last.Sequence + 1,
		Bytes:    len(body),
		SHA256:   hex.EncodeToString(digest[:]),
		Masked:   strings.Contains(string(body), "EMAIL_"),
		// This boolean is safe to expose: it identifies only the checked-in
		// synthetic example fixture, never a value received from a user.
		ContainsSyntheticEmail: strings.Contains(string(body), "demo.user@example.test"),
	}
	s.mu.Unlock()
	return true
}

func mockResponseContent() string {
	content := os.Getenv("BYG_MOCK_RESPONSE_CONTENT")
	if content == "" {
		return "safe mock response"
	}
	return content
}

// writeSSEFixture serves a checked-in OpenAI-compatible SSE fixture. The
// fixture, rather than the mock binary, owns event contents so every streaming
// example can exercise its own event boundaries and completion sequence.
func (s *server) writeSSEFixture(w http.ResponseWriter) {
	path := os.Getenv("BYG_MOCK_SSE_FIXTURE")
	fixture, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, `{"error":"SSE fixture unavailable"}`, http.StatusInternalServerError)
		return
	}
	if len(fixture) == 0 {
		http.Error(w, `{"error":"SSE fixture is empty"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-cache")
	flusher, _ := w.(http.Flusher)
	for _, event := range sseFixtureEvents(fixture) {
		_, _ = w.Write(event)
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func sseFixtureEvents(fixture []byte) [][]byte {
	var events [][]byte
	for len(fixture) > 0 {
		lfEnd := bytes.Index(fixture, []byte("\n\n"))
		crlfEnd := bytes.Index(fixture, []byte("\r\n\r\n"))
		if lfEnd < 0 && crlfEnd < 0 {
			events = append(events, fixture)
			break
		}
		end, separatorLength := lfEnd, len("\n\n")
		if crlfEnd >= 0 && (lfEnd < 0 || crlfEnd < lfEnd) {
			end, separatorLength = crlfEnd, len("\r\n\r\n")
		}
		end += separatorLength
		events = append(events, fixture[:end])
		fixture = fixture[end:]
	}
	return events
}

func (s *server) inspect(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	result := s.last
	s.mu.RUnlock()
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
