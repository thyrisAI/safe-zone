// Package adaptertest provides the reusable BYG data-plane adapter contract
// suite. Gateway packages supply a small native-protocol driver and invoke Run
// from their tests; the suite never imports a gateway implementation.
package adaptertest

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"thyris-sz/internal/controller/capabilities"
	"thyris-sz/internal/extproc"
)

// MessageKind is the gateway-neutral lifecycle event presented to a driver.
// The driver owns construction and inspection of native protocol messages.
type MessageKind string

const (
	RequestHeaders  MessageKind = "request_headers"
	RequestBody     MessageKind = "request_body"
	ResponseHeaders MessageKind = "response_headers"
	ResponseBody    MessageKind = "response_body"
)

// Input describes one native lifecycle event without exposing gateway types to
// the suite. Header values retain their order.
type Input struct {
	Kind        MessageKind
	Headers     map[string][]string
	Body        []byte
	Attributes  map[string]string
	EndOfStream bool
}

// Output is a semantic view of a native adapter response. Wire must contain the
// complete serialized native response so the suite can detect accidental data
// disclosure in immediate responses and metadata.
type Output struct {
	Continued          bool
	Immediate          bool
	StatusCode         int
	BodyMutation       []byte
	BodyMutationSet    bool
	HeaderMutations    map[string]string
	Metadata           extproc.SafeMetadata
	DynamicMetadataSet bool
	Wire               []byte
}

// Session owns native per-request state. Implementations must create fresh
// native messages from Input and return the ProcessingRequest produced by the
// adapter; they must not reproduce normalization logic in the driver.
type Session interface {
	AdaptInput(Input) (extproc.ProcessingRequest, error)
	AdaptOutput(MessageKind, extproc.ProcessingResult) (Output, error)
}

// Driver connects one gateway adapter to the reusable contract suite.
type Driver interface {
	Name() string
	Capabilities() capabilities.AdapterCapabilities
	NewSession() Session
}

// Run executes gateway-neutral transport-boundary tests. It checks only
// capabilities the adapter declares. End-to-end policy outcomes, failure-mode
// orchestration, and telemetry sinks belong to the separate conformance suite.
func Run(t *testing.T, driver Driver) {
	t.Helper()
	if driver == nil {
		t.Fatal("adapter contract driver is nil")
	}
	caps := driver.Capabilities()
	if err := capabilities.ValidateDeclaration(caps); err != nil {
		t.Fatalf("%s capability declaration: %v", driver.Name(), err)
	}
	if driver.Name() != caps.Name {
		t.Fatalf("driver name %q does not match capability name %q", driver.Name(), caps.Name)
	}

	t.Run("normalizes request transaction", func(t *testing.T) {
		session := requireSession(t, driver)
		headers := map[string][]string{
			"Content-Type": {"application/json"},
			":path":        {"/v1/chat/completions?contract=1"},
			"X-Repeat":     {"one", "two"},
		}
		request, err := session.AdaptInput(Input{Kind: RequestHeaders, Headers: headers})
		if err != nil {
			t.Fatalf("adapt request headers: %v", err)
		}
		if request.Stage != extproc.StageRequest || request.ContentType != "application/json" || request.RequestPath != "/v1/chat/completions?contract=1" {
			t.Fatalf("normalized request headers = %+v", request)
		}
		if got := request.Headers["x-repeat"]; !reflect.DeepEqual(got, []string{"one", "two"}) {
			t.Fatalf("repeated headers = %v", got)
		}
		request.Headers["x-repeat"][0] = "caller-mutated"

		body := []byte(`{"messages":[{"role":"user","content":"safe"}]}`)
		bodyRequest, err := session.AdaptInput(Input{Kind: RequestBody, Body: body, EndOfStream: true})
		if err != nil {
			t.Fatalf("adapt request body: %v", err)
		}
		body[0] = '!'
		if bodyRequest.Stage != extproc.StageRequest || !bodyRequest.EndOfStream || bodyRequest.ContentType != "application/json" {
			t.Fatalf("normalized request body = %+v", bodyRequest)
		}
		if got := bodyRequest.Headers["x-repeat"]; !reflect.DeepEqual(got, []string{"one", "two"}) {
			t.Fatalf("adapter state shared normalized header memory: %v", got)
		}
		if got := string(bodyRequest.Body); got != `{"messages":[{"role":"user","content":"safe"}]}` {
			t.Fatalf("normalized body changed with native input: %q", got)
		}

		emptySession := requireSession(t, driver)
		if _, err := emptySession.AdaptInput(Input{Kind: RequestHeaders}); err != nil {
			t.Fatalf("adapt headers before empty request body: %v", err)
		}
		emptyRequest, err := emptySession.AdaptInput(Input{Kind: RequestBody, Body: []byte{}, EndOfStream: true})
		if err != nil {
			t.Fatalf("adapt empty request body: %v", err)
		}
		if emptyRequest.Body == nil || len(emptyRequest.Body) != 0 {
			t.Fatalf("empty request body lost nil/empty distinction: %#v", emptyRequest.Body)
		}
	})

	t.Run("retains request identity for response", func(t *testing.T) {
		session := requireSession(t, driver)
		requestPath := "/v1/responses?contract=1"
		if _, err := session.AdaptInput(Input{Kind: RequestHeaders, Headers: map[string][]string{":path": {requestPath}}, EndOfStream: true}); err != nil {
			t.Fatalf("adapt request headers: %v", err)
		}
		responseHeaders, err := session.AdaptInput(Input{Kind: ResponseHeaders, Headers: map[string][]string{
			"Content-Type": {"application/json"},
			":path":        {"/must-not-replace-request-path"},
		}})
		if err != nil {
			t.Fatalf("adapt response headers: %v", err)
		}
		responseBody, err := session.AdaptInput(Input{Kind: ResponseBody, Body: []byte(`{"output":[]}`), EndOfStream: true})
		if err != nil {
			t.Fatalf("adapt response body: %v", err)
		}
		for _, request := range []extproc.ProcessingRequest{responseHeaders, responseBody} {
			if request.Stage != extproc.StageResponse || request.RequestPath != requestPath || request.ContentType != "application/json" {
				t.Fatalf("normalized response = %+v", request)
			}
		}
		if !responseBody.EndOfStream || string(responseBody.Body) != `{"output":[]}` {
			t.Fatalf("normalized response body = %+v", responseBody)
		}

		other := requireSession(t, driver)
		fresh, err := other.AdaptInput(Input{Kind: RequestHeaders, EndOfStream: true})
		if err != nil {
			t.Fatalf("adapt fresh request: %v", err)
		}
		if fresh.RequestPath != "" {
			t.Fatalf("request path leaked between sessions: %q", fresh.RequestPath)
		}
	})

	t.Run("rejects invalid native lifecycle sequences", func(t *testing.T) {
		requestBodyFirst := requireSession(t, driver)
		if _, err := requestBodyFirst.AdaptInput(Input{Kind: RequestBody, Body: []byte("out-of-order"), EndOfStream: true}); err == nil {
			t.Fatal("adapter accepted request body before request headers")
		}

		duplicateRequestHeaders := requireSession(t, driver)
		if _, err := duplicateRequestHeaders.AdaptInput(Input{Kind: RequestHeaders}); err != nil {
			t.Fatalf("adapt initial request headers: %v", err)
		}
		if _, err := duplicateRequestHeaders.AdaptInput(Input{Kind: RequestHeaders}); err == nil {
			t.Fatal("adapter accepted duplicate request headers")
		}

		responseBodyFirst := requireSession(t, driver)
		if _, err := responseBodyFirst.AdaptInput(Input{Kind: ResponseBody, Body: []byte("out-of-order"), EndOfStream: true}); err == nil {
			t.Fatal("adapter accepted response body before response headers")
		}

		bodyAfterEnd := requireSession(t, driver)
		if _, err := bodyAfterEnd.AdaptInput(Input{Kind: RequestHeaders, EndOfStream: true}); err != nil {
			t.Fatalf("adapt terminal request headers: %v", err)
		}
		if _, err := bodyAfterEnd.AdaptInput(Input{Kind: RequestBody, Body: []byte("after-end"), EndOfStream: true}); err == nil {
			t.Fatal("adapter accepted request body after end of stream")
		}
	})

	t.Run("maps continuation for non-blocking actions", func(t *testing.T) {
		for _, action := range []extproc.Action{extproc.ActionAllow, extproc.ActionAuditOnly} {
			output, err := requireSession(t, driver).AdaptOutput(RequestHeaders, extproc.ProcessingResult{Action: action})
			if err != nil {
				t.Fatalf("adapt %s: %v", action, err)
			}
			if !output.Continued || output.Immediate || output.BodyMutationSet {
				t.Fatalf("%s output = %+v", action, output)
			}
		}
	})

	if caps.RequestBodyMutation {
		t.Run("maps request body and header mutations", func(t *testing.T) {
			assertMutation(t, requireSession(t, driver), RequestBody)
		})
	}
	if caps.ResponseBodyMutation {
		t.Run("maps response body and header mutations", func(t *testing.T) {
			assertMutation(t, requireSession(t, driver), ResponseBody)
		})
	}

	if caps.ImmediateResponse {
		t.Run("maps block to a safe immediate response", func(t *testing.T) {
			const sensitive = "contract-secret-customer@example.com"
			result := extproc.ProcessingResult{
				Action:          extproc.ActionBlock,
				Body:            []byte(sensitive),
				HeaderMutations: map[string]string{"x-unsafe-finding": sensitive},
				ImmediateStatus: 403,
				Metadata: extproc.SafeMetadata{
					RequestID: "gateway-request-1", RID: "RID-contract-1",
					PolicyID: "contract-policy", PolicyVersion: 7,
					Adapter: driver.Name(), Stage: extproc.StageRequest, Action: extproc.ActionBlock,
				},
			}
			output, err := requireSession(t, driver).AdaptOutput(RequestBody, result)
			if err != nil {
				t.Fatalf("adapt BLOCK: %v", err)
			}
			if !output.Immediate || output.Continued || output.StatusCode == 0 {
				t.Fatalf("BLOCK output = %+v", output)
			}
			if bytes.Contains(output.Wire, []byte(sensitive)) {
				t.Fatalf("BLOCK response leaked processor content: %s", output.Wire)
			}
		})
	}

	if caps.DynamicMetadata {
		t.Run("maps safe dynamic metadata", func(t *testing.T) {
			metadata := extproc.SafeMetadata{
				RequestID: "gateway-request-2", RID: "RID-contract-2",
				PolicyID: "contract-policy", PolicyVersion: 8, Adapter: driver.Name(),
				Stage: extproc.StageResponse, Action: extproc.ActionMask,
				Categories: []string{"PII", "SECRET"}, DetectionCount: 2,
				ProcessorLatencyMS: 9, Degraded: true,
			}
			output, err := requireSession(t, driver).AdaptOutput(ResponseHeaders, extproc.ProcessingResult{Action: extproc.ActionAllow, Metadata: metadata})
			if err != nil {
				t.Fatalf("adapt metadata: %v", err)
			}
			if !output.DynamicMetadataSet || !reflect.DeepEqual(output.Metadata, metadata) {
				t.Fatalf("dynamic metadata = %+v, want %+v", output.Metadata, metadata)
			}
		})
	}

	t.Run("rejects unknown processing action", func(t *testing.T) {
		if _, err := requireSession(t, driver).AdaptOutput(RequestHeaders, extproc.ProcessingResult{Action: extproc.Action("UNKNOWN")}); err == nil {
			t.Fatal("adapter accepted unknown processing action")
		}
	})
}

func requireSession(t *testing.T, driver Driver) Session {
	t.Helper()
	session := driver.NewSession()
	if session == nil {
		t.Fatalf("%s returned a nil adapter contract session", driver.Name())
	}
	return session
}

func assertMutation(t *testing.T, session Session, kind MessageKind) {
	t.Helper()
	body := []byte("masked-contract-body")
	wantBody := append([]byte(nil), body...)
	headers := map[string]string{"content-length": fmt.Sprintf("%d", len(body)), "x-tsz-action": "MASK"}
	result := extproc.ProcessingResult{Action: extproc.ActionMask, Body: body, HeaderMutations: headers}
	output, err := session.AdaptOutput(kind, result)
	if err != nil {
		t.Fatalf("adapt MASK: %v", err)
	}
	body[0] = '!'
	headers["x-tsz-action"] = "caller-mutated"
	if !output.Continued || output.Immediate || !output.BodyMutationSet || !bytes.Equal(output.BodyMutation, wantBody) {
		t.Fatalf("MASK output = %+v", output)
	}
	if output.HeaderMutations["content-length"] != fmt.Sprintf("%d", len(wantBody)) || output.HeaderMutations["x-tsz-action"] != "MASK" {
		t.Fatalf("MASK header mutations = %v", output.HeaderMutations)
	}

	empty, err := session.AdaptOutput(kind, extproc.ProcessingResult{Action: extproc.ActionMask, Body: []byte{}})
	if err != nil {
		t.Fatalf("adapt empty MASK: %v", err)
	}
	if !empty.BodyMutationSet || len(empty.BodyMutation) != 0 {
		t.Fatalf("empty body mutation lost nil/empty distinction: %+v", empty)
	}
}

// Stage returns the normalized stage for a lifecycle event.
func (kind MessageKind) Stage() (extproc.ProcessingStage, error) {
	switch kind {
	case RequestHeaders, RequestBody:
		return extproc.StageRequest, nil
	case ResponseHeaders, ResponseBody:
		return extproc.StageResponse, nil
	default:
		return "", fmt.Errorf("unknown adapter contract message kind %q", kind)
	}
}
