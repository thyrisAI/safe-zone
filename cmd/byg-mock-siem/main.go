// byg-mock-siem is a local-only BYG example sink. It stores the latest
// transport-safe audit event and never logs or returns a request payload.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"thyris-sz/internal/guardrails"
)

type sink struct {
	mu     sync.RWMutex
	latest *guardrails.AuditEvent
}

func (s *sink) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var event guardrails.AuditEvent
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		http.Error(w, "invalid audit event", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.latest = &event
	s.mu.Unlock()
	w.WriteHeader(http.StatusAccepted)
}

func (s *sink) inspect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	event := s.latest
	s.mu.RUnlock()
	if event == nil {
		http.Error(w, "no event received", http.StatusNotFound)
		return
	}
	// Return only fields permitted in a BYG audit event. This endpoint is a
	// test assertion surface, never a generic event archive.
	response := struct {
		RID           string   `json:"rid"`
		RequestID     string   `json:"request_id"`
		TraceID       string   `json:"trace_id,omitempty"`
		PolicyID      string   `json:"policy_id"`
		PolicyVersion int      `json:"policy_version"`
		Stage         string   `json:"stage"`
		Action        string   `json:"action"`
		Categories    []string `json:"categories,omitempty"`
		Count         int      `json:"detection_count"`
	}{
		RID: event.RID, RequestID: event.RequestID, TraceID: event.TraceID,
		PolicyID: event.PolicyID, PolicyVersion: event.PolicyVersion,
		Stage: string(event.Stage), Action: string(event.Action),
		Categories: event.Categories, Count: event.DetectionCount,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func main() {
	sink := &sink{}
	mux := http.NewServeMux()
	mux.HandleFunc("/events", sink.events)
	mux.HandleFunc("/inspect", sink.inspect)
	server := &http.Server{Addr: ":8080", Handler: mux}
	log.Printf("BYG example SIEM sink listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
