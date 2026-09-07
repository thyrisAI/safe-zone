package envoy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	. "thyris-sz/internal/extproc"
	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

type recordingProcessor struct {
	mu          sync.Mutex
	requests    []ProcessingRequest
	mutateFirst bool
}

type versionedPolicyCache struct {
	version atomic.Int64
	gets    atomic.Int64
}

type keyedPolicyCache struct {
	mu       sync.Mutex
	versions map[string]int
	gets     map[string]int
}

type cancellationProcessor struct {
	started chan struct{}
	exited  chan struct{}
}

// cancellationWindowProcessor blocks only while processing an SSE window. It
// lets the request and response headers complete so tests can cancel precisely
// while the windowed streaming path is in flight.
type cancellationWindowProcessor struct{ *cancellationProcessor }

type failingWindowProcessor struct{}

type blockingWindowProcessor struct{}

type asyncAuditWindowProcessor struct {
	processed chan []OpenAISSEEvent
}

func (failingWindowProcessor) Process(context.Context, ProcessingRequest) (ProcessingResult, error) {
	return ProcessingResult{Action: ActionAllow}, nil
}

func (failingWindowProcessor) ProcessSSEWindow(context.Context, ProcessingRequest, []OpenAISSEEvent) (ProcessingResult, []OpenAISSEEvent, error) {
	return ProcessingResult{}, nil, errors.New("injected SSE window failure")
}

func (blockingWindowProcessor) Process(context.Context, ProcessingRequest) (ProcessingResult, error) {
	return ProcessingResult{Action: ActionAllow}, nil
}

func (blockingWindowProcessor) ProcessSSEWindow(_ context.Context, request ProcessingRequest, _ []OpenAISSEEvent) (ProcessingResult, []OpenAISSEEvent, error) {
	return ProcessingResult{
		Action:          ActionBlock,
		ImmediateStatus: 403,
		DetectionCount:  1,
		Metadata: SafeMetadata{
			RequestID: request.EnvoyReqID,
			RID:       request.RID, PolicyID: request.PolicyID, PolicyVersion: request.PolicyVersion,
			Adapter: "openai_chat_completions", Stage: StageResponse, Action: ActionBlock,
			Categories: []string{"SECRET"}, DetectionCount: 1,
		},
	}, nil, nil
}

func (processor asyncAuditWindowProcessor) Process(context.Context, ProcessingRequest) (ProcessingResult, error) {
	return ProcessingResult{Action: ActionAllow}, nil
}

func (processor asyncAuditWindowProcessor) ProcessSSEWindow(_ context.Context, _ ProcessingRequest, events []OpenAISSEEvent) (ProcessingResult, []OpenAISSEEvent, error) {
	processor.processed <- append([]OpenAISSEEvent(nil), events...)
	return ProcessingResult{Action: ActionAuditOnly, DetectionCount: 1, Metadata: SafeMetadata{Categories: []string{"PII"}}}, nil, nil
}

type mockUpstream struct{ requests atomic.Int64 }

func (upstream *mockUpstream) forward(_ []byte) { upstream.requests.Add(1) }

type snapshotPolicyCache struct{ snapshot policy.CompiledSnapshot }

func (cache snapshotPolicyCache) Ready() bool { return true }

func (cache snapshotPolicyCache) Get(policyID string) (policy.CompiledSnapshot, bool) {
	if policyID != cache.snapshot.PolicyID {
		return policy.CompiledSnapshot{}, false
	}
	return cache.snapshot.Clone(), true
}

type recordingAuditor struct {
	mu     sync.Mutex
	events []guardrails.AuditEvent
}

type recordingResponseStateObserver struct {
	mu       sync.Mutex
	outcomes []string
}

func (observer *recordingResponseStateObserver) ObserveResponseWithoutRequestState(outcome string) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.outcomes = append(observer.outcomes, outcome)
}

func (observer *recordingResponseStateObserver) snapshot() []string {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return append([]string(nil), observer.outcomes...)
}

func (auditor *recordingAuditor) Audit(_ context.Context, event guardrails.AuditEvent) error {
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	auditor.events = append(auditor.events, event)
	return nil
}

func (auditor *recordingAuditor) eventsSnapshot() []guardrails.AuditEvent {
	auditor.mu.Lock()
	defer auditor.mu.Unlock()
	return append([]guardrails.AuditEvent(nil), auditor.events...)
}

type failingAuditor struct{}

func (failingAuditor) Audit(context.Context, guardrails.AuditEvent) error {
	return errors.New("audit sink unavailable")
}

type blockingOperationalAuditor struct {
	started chan struct{}
	release chan struct{}
}

func (auditor blockingOperationalAuditor) Audit(_ context.Context, event guardrails.AuditEvent) error {
	if eventType := event.EventType; eventType != "operational" {
		return nil
	}
	select {
	case auditor.started <- struct{}{}:
	default:
	}
	<-auditor.release
	return nil
}

type processorFunc func(context.Context, ProcessingRequest) (ProcessingResult, error)

func (fn processorFunc) Process(ctx context.Context, request ProcessingRequest) (ProcessingResult, error) {
	return fn(ctx, request)
}

func newCancellationProcessor(capacity int) *cancellationProcessor {
	return &cancellationProcessor{
		started: make(chan struct{}, capacity),
		exited:  make(chan struct{}, capacity),
	}
}

func (processor *cancellationProcessor) Process(ctx context.Context, _ ProcessingRequest) (ProcessingResult, error) {
	processor.started <- struct{}{}
	<-ctx.Done()
	processor.exited <- struct{}{}
	return ProcessingResult{}, ctx.Err()
}

func (processor cancellationWindowProcessor) Process(context.Context, ProcessingRequest) (ProcessingResult, error) {
	return ProcessingResult{Action: ActionAllow}, nil
}

func (processor cancellationWindowProcessor) ProcessSSEWindow(ctx context.Context, _ ProcessingRequest, _ []OpenAISSEEvent) (ProcessingResult, []OpenAISSEEvent, error) {
	processor.started <- struct{}{}
	<-ctx.Done()
	processor.exited <- struct{}{}
	return ProcessingResult{}, nil, ctx.Err()
}

func newKeyedPolicyCache(versions map[string]int) *keyedPolicyCache {
	cloned := make(map[string]int, len(versions))
	for policyID, version := range versions {
		cloned[policyID] = version
	}
	return &keyedPolicyCache{versions: cloned, gets: make(map[string]int)}
}

func (cache *keyedPolicyCache) Ready() bool { return true }

func (cache *keyedPolicyCache) Get(policyID string) (policy.CompiledSnapshot, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	version, ok := cache.versions[policyID]
	if !ok {
		return policy.CompiledSnapshot{}, false
	}
	cache.gets[policyID]++
	return policy.CompiledSnapshot{PolicyID: policyID, Version: version}, true
}

func (cache *keyedPolicyCache) setVersion(policyID string, version int) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.versions[policyID] = version
}

func (cache *keyedPolicyCache) getCount(policyID string) int {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.gets[policyID]
}

func (cache *versionedPolicyCache) Ready() bool { return true }

func (cache *versionedPolicyCache) Get(policyID string) (policy.CompiledSnapshot, bool) {
	cache.gets.Add(1)
	version := int(cache.version.Load())
	return policy.CompiledSnapshot{
		PolicyID: policyID,
		Version:  version,
		Definition: policy.PolicyDefinition{Request: policy.RequestPolicy{
			CustomPatternIDs: []string{"pattern-v1"},
		}},
	}, true
}

func (processor *recordingProcessor) Process(_ context.Context, request ProcessingRequest) (ProcessingResult, error) {
	processor.mu.Lock()
	stored := request
	if request.PolicySnapshot != nil {
		clone := request.PolicySnapshot.Clone()
		stored.PolicySnapshot = &clone
	}
	processor.requests = append(processor.requests, stored)
	if processor.mutateFirst && len(processor.requests) == 1 && request.PolicySnapshot != nil {
		request.PolicySnapshot.Version = 999
		request.PolicySnapshot.Definition.Request.CustomPatternIDs[0] = "processor-mutated"
	}
	processor.mu.Unlock()
	return ProcessingResult{Action: ActionAllow}, nil
}

func (processor *recordingProcessor) requestsByRID() map[string][]ProcessingRequest {
	processor.mu.Lock()
	defer processor.mu.Unlock()
	grouped := make(map[string][]ProcessingRequest)
	for _, request := range processor.requests {
		stored := request
		if request.PolicySnapshot != nil {
			clone := request.PolicySnapshot.Clone()
			stored.PolicySnapshot = &clone
		}
		grouped[request.RID] = append(grouped[request.RID], stored)
	}
	return grouped
}

func newExternalProcessorTestClient(t *testing.T, processor Processor, cache PolicyCache, maxConcurrentStreams ...uint32) extprocv3.ExternalProcessorClient {
	t.Helper()
	// Generic protocol tests intentionally omit a policy snapshot. Model that
	// fixture explicitly as fail-open; policy-missing closed behavior has its
	// own focused tests using ServerSettings.
	server, err := NewServerWithSettings(processor, cache, nil, ServerSettings{FailMode: policy.FailureModeOpen, MaxBodyBytes: 1024 * 1024, ProcessingTimeout: 2 * time.Second}, maxConcurrentStreams...)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	return newExternalProcessorTestClientForServer(t, server)
}

func newExternalProcessorTestClientForServer(t *testing.T, server *Server) extprocv3.ExternalProcessorClient {
	t.Helper()
	t.Cleanup(server.Close)
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	extprocv3.RegisterExternalProcessorServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})

	connection, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return extprocv3.NewExternalProcessorClient(connection)
}

func requestHeadersMessage(rid, envoyReqID, policyID string) *extprocv3.ProcessingRequest {
	return requestHeadersMessageWithEndOfStream(rid, envoyReqID, policyID, false)
}

func requestHeadersMessageWithEndOfStream(rid, envoyReqID, policyID string, endOfStream bool) *extprocv3.ProcessingRequest {
	return &extprocv3.ProcessingRequest{
		Request: &extprocv3.ProcessingRequest_RequestHeaders{RequestHeaders: &extprocv3.HttpHeaders{Headers: testHeaderMap(
			[2]string{"x-request-id", envoyReqID}, [2]string{"x-tsz-rid", rid}, [2]string{"x-tsz-policy", policyID},
		), EndOfStream: endOfStream}},
	}
}

func remainingStreamMessages() []*extprocv3.ProcessingRequest {
	return []*extprocv3.ProcessingRequest{
		requestBodyForAdapterTest([]byte("request"), true),
		responseHeadersForAdapterTest(false),
		responseBodyForAdapterTest([]byte("response"), true),
	}
}

func exchangeMessages(stream extprocv3.ExternalProcessor_ProcessClient, messages []*extprocv3.ProcessingRequest, responseOffset int) error {
	for index, message := range messages {
		if err := stream.Send(message); err != nil {
			return fmt.Errorf("send message %d: %w", index+responseOffset, err)
		}
		response, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("receive response %d: %w", index+responseOffset, err)
		}
		switch index + responseOffset {
		case 0:
			if response.GetRequestHeaders() == nil {
				return fmt.Errorf("response 0 is not request headers")
			}
		case 1:
			if response.GetRequestBody() == nil {
				return fmt.Errorf("response 1 is not request body")
			}
		case 2:
			if response.GetResponseHeaders() == nil {
				return fmt.Errorf("response 2 is not response headers")
			}
		case 3:
			if response.GetResponseBody() == nil {
				return fmt.Errorf("response 3 is not response body")
			}
		}
	}
	return nil
}

func runAllowStream(ctx context.Context, client extprocv3.ExternalProcessorClient, rid, envoyReqID, policyID string) error {
	stream, err := client.Process(ctx)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	messages := append([]*extprocv3.ProcessingRequest{requestHeadersMessage(rid, envoyReqID, policyID)}, remainingStreamMessages()...)
	if err := exchangeMessages(stream, messages, 0); err != nil {
		return err
	}
	if err := stream.CloseSend(); err != nil {
		return fmt.Errorf("close stream: %w", err)
	}
	return nil
}

func TestServerProcessesHeadersAndBufferedBodiesBidirectionally(t *testing.T) {
	processor := &recordingProcessor{mutateFirst: true}
	policyCache := &versionedPolicyCache{}
	policyCache.version.Store(1)
	client := newExternalProcessorTestClient(t, processor, policyCache)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.Process(ctx)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	messages := []*extprocv3.ProcessingRequest{
		requestHeadersMessage("rid-1", "envoy-1", "default"),
		requestBodyForAdapterTest([]byte("request"), true),
		responseHeadersForAdapterTest(false),
		responseBodyForAdapterTest([]byte("response"), true),
	}
	for index, message := range messages {
		if err := stream.Send(message); err != nil {
			t.Fatalf("Send(%d) error = %v", index, err)
		}
		response, err := stream.Recv()
		if err != nil {
			t.Fatalf("Recv(%d) error = %v", index, err)
		}
		if index == 0 {
			// A concurrent activation changes the cache, but this stream must
			// keep the snapshot pinned from request headers.
			policyCache.version.Store(2)
		}
		switch index {
		case 0:
			if response.GetRequestHeaders() == nil {
				t.Fatalf("response %d is not request headers: %+v", index, response)
			}
		case 1:
			if response.GetRequestBody() == nil {
				t.Fatalf("response %d is not request body: %+v", index, response)
			}
		case 2:
			if response.GetResponseHeaders() == nil {
				t.Fatalf("response %d is not response headers: %+v", index, response)
			}
		case 3:
			if response.GetResponseBody() == nil {
				t.Fatalf("response %d is not response body: %+v", index, response)
			}
		}
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("CloseSend() error = %v", err)
	}

	processor.mu.Lock()
	defer processor.mu.Unlock()
	if len(processor.requests) != 4 {
		t.Fatalf("processor received %d requests, want 4", len(processor.requests))
	}
	if processor.requests[0].Stage != StageRequest || processor.requests[1].Stage != StageRequest || processor.requests[2].Stage != StageResponse || processor.requests[3].Stage != StageResponse {
		t.Fatalf("processor stages = %+v", processor.requests)
	}
	if string(processor.requests[1].Body) != "request" || string(processor.requests[3].Body) != "response" {
		t.Fatalf("processor bodies = %+v", processor.requests)
	}
	streamRID := processor.requests[0].RID
	if !strings.HasPrefix(streamRID, "RID-") {
		t.Fatalf("BYG stream RID = %q", streamRID)
	}
	for index, request := range processor.requests {
		if request.RID != streamRID || request.EnvoyReqID != "envoy-1" || request.PolicyID != "default" || request.PolicyVersion != 1 {
			t.Fatalf("processor request %d identity/version = %+v", index, request)
		}
		if request.PolicySnapshot == nil || request.PolicySnapshot.Version != 1 {
			t.Fatalf("processor request %d snapshot = %+v", index, request.PolicySnapshot)
		}
		if request.PolicySnapshot.Definition.Request.CustomPatternIDs[0] != "pattern-v1" {
			t.Fatalf("processor request %d observed mutated definition: %+v", index, request.PolicySnapshot.Definition)
		}
	}
	if got := policyCache.gets.Load(); got != 1 {
		t.Fatalf("policy cache Get calls = %d, want 1", got)
	}
}

func TestServerParsesStreamedSSEBodiesWithoutInvokingResponseEnforcement(t *testing.T) {
	processor := &recordingProcessor{}
	policyCache := &versionedPolicyCache{}
	policyCache.version.Store(1)
	stream, err := newExternalProcessorTestClient(t, processor, policyCache).Process(context.Background())
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	responseHeaders := responseHeadersForAdapterTest(false)
	responseHeaders.GetResponseHeaders().Headers.Headers[0].RawValue = []byte("text/event-stream")
	messages := []*extprocv3.ProcessingRequest{
		requestHeadersMessage("rid-stream", "envoy-stream", "default"),
		requestBodyForAdapterTest(nil, true),
		responseHeaders,
		responseBodyForAdapterTest([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hel"), false),
		responseBodyForAdapterTest([]byte("lo\"}}]}\n\ndata: [DONE]\n\n"), true),
	}
	if err := exchangeMessages(stream, messages, 0); err != nil {
		t.Fatalf("exchangeMessages() error = %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("CloseSend() error = %v", err)
	}

	processor.mu.Lock()
	defer processor.mu.Unlock()
	if len(processor.requests) != 3 {
		t.Fatalf("processor received %d requests, want headers, request body and response headers only", len(processor.requests))
	}
	for _, request := range processor.requests {
		if request.Stage == StageResponse && request.Body != nil {
			t.Fatalf("streamed response body reached non-streaming processor: %+v", request)
		}
	}
}

func TestServerForwardsAsyncAuditSSEWithoutMutationAndAuditsAfterCompletion(t *testing.T) {
	processor := asyncAuditWindowProcessor{processed: make(chan []OpenAISSEEvent, 1)}
	auditor := &recordingAuditor{}
	cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{PolicyID: "default", Version: 3, Definition: policy.PolicyDefinition{
		Response:  policy.ResponsePolicy{Enabled: true, PII: policy.ActionAuditOnly, Secret: policy.ActionAuditOnly, UnsafeContent: policy.ActionAuditOnly},
		Streaming: policy.StreamingSettings{Mode: policy.StreamingModeAsyncAudit},
	}}}
	server, err := NewServerWithSettings(processor, cache, auditor, ServerSettings{FailMode: policy.FailureModeClosed, MaxBodyBytes: 1024, MaxStreamBufferBytes: 1024, ProcessingTimeout: time.Second})
	if err != nil {
		t.Fatalf("NewServerWithSettings: %v", err)
	}
	stream, err := newExternalProcessorTestClientForServer(t, server).Process(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	for _, message := range []*extprocv3.ProcessingRequest{requestHeadersMessageWithEndOfStream("ignored", "envoy-async", "default", true), responseHeadersForAdapterTest(false)} {
		if message.GetResponseHeaders() != nil {
			message.GetResponseHeaders().Headers.Headers[0].RawValue = []byte("text/event-stream")
		}
		if err := stream.Send(message); err != nil {
			t.Fatalf("send setup: %v", err)
		}
		if _, err := stream.Recv(); err != nil {
			t.Fatalf("receive setup: %v", err)
		}
	}
	raw := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"alice@example.com\"}}]}\n\ndata: [DONE]\n\n")
	if err := stream.Send(responseBodyForAdapterTest(raw, true)); err != nil {
		t.Fatalf("send SSE: %v", err)
	}
	response, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive SSE: %v", err)
	}
	if response.GetImmediateResponse() != nil || response.GetResponseBody().GetResponse().GetBodyMutation() != nil {
		t.Fatalf("AsyncAudit altered the delivered stream: %+v", response)
	}
	select {
	case events := <-processor.processed:
		if len(events) != 2 || !strings.Contains(string(events[0].Raw), "alice@example.com") {
			t.Fatalf("async events = %+v", events)
		}
	case <-time.After(time.Second):
		t.Fatal("async stream inspection did not complete")
	}
	deadline := time.Now().Add(time.Second)
	for {
		events := auditor.eventsSnapshot()
		if len(events) >= 3 {
			event := events[len(events)-1]
			if event.Stage != guardrails.AuditStageResponse || event.Action != guardrails.RuleActionAuditOnly || event.DetectionCount != 1 || strings.Join(event.Categories, ",") != "PII" {
				t.Fatalf("async audit event = %+v", event)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("async audit events = %+v, want completed response decision", events)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestServerResponseWithoutRequestStateUsesFailModeAndAudits(t *testing.T) {
	for _, test := range []struct {
		name        string
		mode        policy.FailureMode
		wantBlock   bool
		wantOutcome string
	}{
		{name: "closed by default", mode: policy.FailureModeClosed, wantBlock: true, wantOutcome: "fail_closed"},
		{name: "explicit open", mode: policy.FailureModeOpen, wantBlock: false, wantOutcome: "fail_open"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var processorCalls atomic.Int64
			auditor := &recordingAuditor{}
			observer := &recordingResponseStateObserver{}
			server, err := NewServerWithSettings(processorFunc(func(context.Context, ProcessingRequest) (ProcessingResult, error) {
				processorCalls.Add(1)
				return ProcessingResult{Action: ActionAllow}, nil
			}), newKeyedPolicyCache(map[string]int{"default": 1}), auditor, ServerSettings{FailMode: test.mode, ResponseStateObserver: observer})
			if err != nil {
				t.Fatalf("NewServerWithSettings() error = %v", err)
			}
			stream, err := newExternalProcessorTestClientForServer(t, server).Process(context.Background())
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			if err := stream.Send(responseHeadersForAdapterTest(false)); err != nil {
				t.Fatalf("send unmarked response headers: %v", err)
			}
			response, err := stream.Recv()
			if err != nil {
				t.Fatalf("receive unmarked response headers: %v", err)
			}
			if (response.GetImmediateResponse() != nil) != test.wantBlock {
				t.Fatalf("unmarked response = %+v, wantBlock=%t", response, test.wantBlock)
			}
			if test.wantBlock && response.GetImmediateResponse().GetStatus().GetCode() != typev3.StatusCode_Forbidden {
				t.Fatalf("closed response status = %+v", response.GetImmediateResponse())
			}
			if !test.wantBlock && response.GetResponseHeaders().GetResponse().GetStatus() != extprocv3.CommonResponse_CONTINUE {
				t.Fatalf("open response = %+v", response)
			}
			if processorCalls.Load() != 0 {
				t.Fatalf("unmarked response invoked processor %d times", processorCalls.Load())
			}
			if outcomes := observer.snapshot(); len(outcomes) != 1 || outcomes[0] != test.wantOutcome {
				t.Fatalf("outcomes = %v", outcomes)
			}
			events := auditor.eventsSnapshot()
			if len(events) != 1 || events[0].Reason != "response_without_request_state" || !events[0].Degraded || events[0].Action != guardrails.RuleAction(responseAction(test.wantBlock)) {
				t.Fatalf("audit events = %+v", events)
			}
		})
	}
}

func TestServerIgnoresResponseOnlyLookingRequestHeaderAndStillGuardsResponse(t *testing.T) {
	const rawPII = "assistant@example.test"
	const masked = "[REDACTED:EMAIL]"
	var responseCalls atomic.Int64
	client := newExternalProcessorTestClient(t, processorFunc(func(_ context.Context, request ProcessingRequest) (ProcessingResult, error) {
		if request.Stage == StageResponse && request.Body != nil {
			responseCalls.Add(1)
			if got := string(request.Body); got != rawPII {
				t.Fatalf("response body = %q, want synthetic PII", got)
			}
			return ProcessingResult{Action: ActionMask, Body: []byte(masked)}, nil
		}
		return ProcessingResult{Action: ActionAllow}, nil
	}), newKeyedPolicyCache(map[string]int{"default": 1}))
	stream, err := client.Process(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	headers := requestHeadersMessage("rid-spoof", "envoy-spoof", "default")
	headers.GetRequestHeaders().GetHeaders().Headers = append(headers.GetRequestHeaders().GetHeaders().Headers,
		&corev3.HeaderValue{Key: "X-TSZ-Envoy-Local-Reply", RawValue: []byte("spoofed")})
	for _, message := range []*extprocv3.ProcessingRequest{
		headers,
		requestBodyForAdapterTest([]byte(`{"model":"test"}`), true),
		responseHeadersForAdapterTest(false),
		responseBodyForAdapterTest([]byte(rawPII), true),
	} {
		if err := stream.Send(message); err != nil {
			t.Fatalf("send: %v", err)
		}
		response, err := stream.Recv()
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		if message.GetResponseBody() != nil {
			if got := string(response.GetResponseBody().GetResponse().GetBodyMutation().GetBody()); got != masked {
				t.Fatalf("spoofed marker bypassed response masking: got %q", got)
			}
		}
	}
	if responseCalls.Load() != 1 {
		t.Fatalf("response processor calls = %d, want 1", responseCalls.Load())
	}
}

func responseAction(block bool) Action {
	if block {
		return ActionBlock
	}
	return ActionAllow
}

func TestServerMapsImmediateResponseAndMutationsFromFakeProcessor(t *testing.T) {
	t.Run("immediate response", func(t *testing.T) {
		client := newExternalProcessorTestClient(t, processorFunc(func(context.Context, ProcessingRequest) (ProcessingResult, error) {
			return ProcessingResult{Action: ActionBlock, Body: []byte("blocked by test")}, nil
		}), newKeyedPolicyCache(nil))
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("open stream: %v", err)
		}
		if err := stream.Send(requestHeadersMessageWithEndOfStream("rid-block", "envoy-block", "default", true)); err != nil {
			t.Fatalf("send headers: %v", err)
		}
		response, err := stream.Recv()
		if err != nil {
			t.Fatalf("receive immediate response: %v", err)
		}
		if response.GetImmediateResponse().GetStatus().GetCode() != typev3.StatusCode_BadRequest {
			t.Fatalf("immediate response = %+v", response.GetImmediateResponse())
		}
		var payload blockErrorResponse
		if err := json.Unmarshal(response.GetImmediateResponse().GetBody(), &payload); err != nil || payload.Error.Code != blockErrorCode {
			t.Fatalf("immediate response must use safe block schema: body=%q error=%v", response.GetImmediateResponse().GetBody(), err)
		}
	})

	t.Run("header and body mutation", func(t *testing.T) {
		client := newExternalProcessorTestClient(t, processorFunc(func(_ context.Context, request ProcessingRequest) (ProcessingResult, error) {
			if request.Body == nil {
				return ProcessingResult{Action: ActionMask, HeaderMutations: map[string]string{"x-tsz-action": "MASK"}}, nil
			}
			return ProcessingResult{Action: ActionMask, Body: []byte("masked body")}, nil
		}), newKeyedPolicyCache(nil))
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("open stream: %v", err)
		}
		if err := stream.Send(requestHeadersMessage("rid-mask", "envoy-mask", "default")); err != nil {
			t.Fatalf("send headers: %v", err)
		}
		headerResponse, err := stream.Recv()
		if err != nil {
			t.Fatalf("receive header response: %v", err)
		}
		if got := headerResponse.GetRequestHeaders().GetResponse().GetHeaderMutation().GetSetHeaders(); len(got) != 3 {
			t.Fatalf("header mutation = %+v", headerResponse)
		}
		identityHeaders := make(map[string]string)
		for _, header := range headerResponse.GetRequestHeaders().GetResponse().GetHeaderMutation().GetSetHeaders() {
			identityHeaders[header.GetHeader().GetKey()] = string(header.GetHeader().GetRawValue())
		}
		if !strings.HasPrefix(identityHeaders["x-tsz-rid"], "RID-") || identityHeaders["x-tsz-rid"] == "rid-mask" || identityHeaders["x-tsz-envoy-request-id"] != "envoy-mask" {
			t.Fatalf("BYG identity headers = %v", identityHeaders)
		}
		if err := stream.Send(requestBodyForAdapterTest([]byte("original"), true)); err != nil {
			t.Fatalf("send body: %v", err)
		}
		bodyResponse, err := stream.Recv()
		if err != nil {
			t.Fatalf("receive body response: %v", err)
		}
		if got := string(bodyResponse.GetRequestBody().GetResponse().GetBodyMutation().GetBody()); got != "masked body" {
			t.Fatalf("body mutation = %q, want masked body", got)
		}
	})
}

func TestServerBlockImmediateResponseIsSafeAndDoesNotForwardUpstream(t *testing.T) {
	const rawSecret = "secret-do-not-leak"
	upstream := &mockUpstream{}
	client := newExternalProcessorTestClient(t, processorFunc(func(context.Context, ProcessingRequest) (ProcessingResult, error) {
		return ProcessingResult{
			Action: ActionBlock,
			// This models an accidental unsafe processor body. The adapter must
			// discard it before it reaches either Envoy or an upstream service.
			Body:     []byte(rawSecret),
			Metadata: SafeMetadata{RID: "rid-block", RequestID: "envoy-block", PolicyID: "default", PolicyVersion: 1},
		}, nil
	}), newKeyedPolicyCache(map[string]int{"default": 1}))

	stream, err := client.Process(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := stream.Send(requestHeadersMessageWithEndOfStream("rid-block", "envoy-block", "default", true)); err != nil {
		t.Fatalf("send headers: %v", err)
	}
	response, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive immediate response: %v", err)
	}
	immediate := response.GetImmediateResponse()
	if immediate == nil || immediate.GetStatus().GetCode() != typev3.StatusCode_BadRequest {
		t.Fatalf("expected HTTP 400 immediate response, got %+v", response)
	}
	var payload blockErrorResponse
	if err := json.Unmarshal(immediate.GetBody(), &payload); err != nil {
		t.Fatalf("immediate response is not valid JSON: %v; body=%q", err, immediate.GetBody())
	}
	if payload.Error.Code != blockErrorCode || !strings.HasPrefix(payload.TSZMeta.RID, "RID-") || payload.TSZMeta.EnvoyRequestID != "envoy-block" {
		t.Fatalf("unexpected safe immediate payload: %+v", payload)
	}
	// Envoy terminates the request on an ImmediateResponse. The mock's
	// forwarding branch therefore never runs, which guards this integration
	// boundary without requiring a live Envoy process in unit tests.
	if immediate == nil {
		upstream.forward([]byte("request body"))
	}
	if got := upstream.requests.Load(); got != 0 {
		t.Fatalf("blocked request reached mock upstream %d time(s)", got)
	}
	if strings.Contains(string(immediate.GetBody()), rawSecret) || strings.Contains(response.String(), rawSecret) {
		t.Fatalf("immediate response leaked raw sensitive value: %s", response.String())
	}
}

func TestServerAuditsSafeRequestMetadataAndPublishesDynamicMetadata(t *testing.T) {
	auditor := &recordingAuditor{}
	server, err := NewServerWithAuditor(processorFunc(func(context.Context, ProcessingRequest) (ProcessingResult, error) {
		return ProcessingResult{Action: ActionAuditOnly, DetectionCount: 2, Metadata: SafeMetadata{Categories: []string{"PII", "PROMPT_INJECTION"}}}, nil
	}), newKeyedPolicyCache(map[string]int{"policy-a": 5}), auditor)
	if err != nil {
		t.Fatalf("NewServerWithAuditor() error = %v", err)
	}
	client := newExternalProcessorTestClientForServer(t, server)
	stream, err := client.Process(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	message := requestHeadersMessageWithEndOfStream("client-rid-must-not-be-used", "envoy-42", "policy-a", true)
	message.GetRequestHeaders().GetHeaders().Headers = append(message.GetRequestHeaders().GetHeaders().Headers,
		&corev3.HeaderValue{Key: "x-tsz-gateway", RawValue: []byte("gateway-a")},
		&corev3.HeaderValue{Key: "x-tsz-route", RawValue: []byte("route-a")},
		&corev3.HeaderValue{Key: "x-tsz-tenant", RawValue: []byte("tenant-a")},
	)
	if err := stream.Send(message); err != nil {
		t.Fatalf("send headers: %v", err)
	}
	response, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive response: %v", err)
	}
	events := auditor.eventsSnapshot()
	if len(events) != 1 {
		t.Fatalf("audit event count = %d, want 1", len(events))
	}
	event := events[0]
	if event.Timestamp.IsZero() || !strings.HasPrefix(event.RID, "RID-") || event.RID == "client-rid-must-not-be-used" ||
		event.RequestID != "envoy-42" || event.TraceID != "" || event.Adapter != "envoy-gateway" ||
		event.Target != "gateway=gateway-a;tenant=tenant-a;route=route-a" ||
		event.Gateway != "gateway-a" || event.Route != "route-a" || event.Tenant != "tenant-a" ||
		event.PolicyID != "policy-a" || event.PolicyVersion != 5 || event.Stage != guardrails.AuditStageRequest ||
		event.Action != guardrails.RuleActionAuditOnly || event.DetectionCount != 2 || event.ProcessorLatencyMS < 0 {
		t.Fatalf("audit event = %+v", event)
	}
	if got := strings.Join(event.Categories, ","); got != "PII,PROMPT_INJECTION" {
		t.Fatalf("audit categories = %q", got)
	}
	metadata := response.GetDynamicMetadata().GetFields()[safeMetadataNamespace].GetStructValue().GetFields()
	for _, field := range []string{"request_id", "rid", "policy_id", "policy_version", "adapter", "stage", "action", "categories", "detection_count", "processor_latency_ms"} {
		if _, found := metadata[field]; !found {
			t.Fatalf("dynamic metadata lacks %q: %+v", field, metadata)
		}
	}
	if metadata["rid"].GetStringValue() != event.RID || metadata["request_id"].GetStringValue() != "envoy-42" || metadata["adapter"].GetStringValue() != "envoy-gateway" {
		t.Fatalf("dynamic metadata identity = %+v, event = %+v", metadata, event)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal audit event: %v", err)
	}
	if strings.Contains(string(encoded), "client-rid-must-not-be-used") {
		t.Fatalf("audit event contains client-controlled RID: %s", encoded)
	}
}

func TestServerAuditsResponseDecisionsWithResponseStage(t *testing.T) {
	auditor := &recordingAuditor{}
	server, err := NewServerWithAuditor(processorFunc(func(_ context.Context, request ProcessingRequest) (ProcessingResult, error) {
		result := ProcessingResult{Action: ActionAllow}
		if request.Stage == StageResponse && request.Body != nil {
			result.Action = ActionAuditOnly
			result.DetectionCount = 1
			result.Metadata.Categories = []string{"UNSAFE_CONTENT"}
		}
		return result, nil
	}), newKeyedPolicyCache(map[string]int{"policy-a": 5}), auditor)
	if err != nil {
		t.Fatalf("NewServerWithAuditor() error = %v", err)
	}
	stream, err := newExternalProcessorTestClientForServer(t, server).Process(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	for _, message := range []*extprocv3.ProcessingRequest{
		requestHeadersMessageWithEndOfStream("ignored", "envoy-response-audit", "policy-a", true),
		responseHeadersForAdapterTest(false),
		responseBodyForAdapterTest([]byte(`{"choices":[]}`), true),
	} {
		if err := stream.Send(message); err != nil {
			t.Fatalf("send message: %v", err)
		}
		if _, err := stream.Recv(); err != nil {
			t.Fatalf("receive response: %v", err)
		}
	}
	events := auditor.eventsSnapshot()
	if len(events) != 3 {
		t.Fatalf("audit event count = %d, want 3: %+v", len(events), events)
	}
	responseEvent := events[2]
	if responseEvent.Stage != guardrails.AuditStageResponse || responseEvent.Action != guardrails.RuleActionAuditOnly ||
		responseEvent.PolicyID != "policy-a" || responseEvent.PolicyVersion != 5 || responseEvent.RequestID != "envoy-response-audit" ||
		responseEvent.DetectionCount != 1 || strings.Join(responseEvent.Categories, ",") != "UNSAFE_CONTENT" {
		t.Fatalf("response audit event = %+v", responseEvent)
	}
}

func TestServerAuditsPinnedPolicyVersionAfterCacheUpdate(t *testing.T) {
	cache := &versionedPolicyCache{}
	cache.version.Store(1)
	auditor := &recordingAuditor{}
	server, err := NewServerWithAuditor(NewAllowProcessor(), cache, auditor)
	if err != nil {
		t.Fatalf("NewServerWithAuditor() error = %v", err)
	}
	client := newExternalProcessorTestClientForServer(t, server)
	stream, err := client.Process(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := stream.Send(requestHeadersMessageWithEndOfStream("ignored", "envoy-pinned", "default", false)); err != nil {
		t.Fatalf("send headers: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("receive header response: %v", err)
	}

	// Simulate a successful activation while this stream remains open.
	cache.version.Store(2)
	if err := stream.Send(requestBodyForAdapterTest([]byte(`{"messages":[]}`), true)); err != nil {
		t.Fatalf("send body: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("receive body response: %v", err)
	}
	events := auditor.eventsSnapshot()
	if len(events) != 2 {
		t.Fatalf("audit event count = %d, want 2", len(events))
	}
	for index, event := range events {
		if event.PolicyID != "default" || event.PolicyVersion != 1 {
			t.Fatalf("event %d policy identity = %s/%d, want default/1", index, event.PolicyID, event.PolicyVersion)
		}
	}
	if got := cache.gets.Load(); got != 1 {
		t.Fatalf("policy cache Get calls = %d, want 1", got)
	}
}

func TestServerAuditsEveryActionAndNeverPublishesSensitiveFixture(t *testing.T) {
	const sensitiveFixture = "secret-fixture-do-not-publish"
	for _, test := range []struct {
		name   string
		action Action
		count  int
	}{
		{name: "allow", action: ActionAllow, count: 0},
		{name: "mask", action: ActionMask, count: 1},
		{name: "block", action: ActionBlock, count: 2},
		{name: "audit only", action: ActionAuditOnly, count: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			auditor := &recordingAuditor{}
			server, err := NewServerWithAuditor(processorFunc(func(context.Context, ProcessingRequest) (ProcessingResult, error) {
				return ProcessingResult{Action: test.action, DetectionCount: test.count, Body: []byte(sensitiveFixture), Metadata: SafeMetadata{Categories: []string{"PII"}}}, nil
			}), newKeyedPolicyCache(map[string]int{"default": 9}), auditor)
			if err != nil {
				t.Fatalf("NewServerWithAuditor() error = %v", err)
			}
			client := newExternalProcessorTestClientForServer(t, server)
			stream, err := client.Process(context.Background())
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			if err := stream.Send(requestHeadersMessageWithEndOfStream("untrusted-rid", "envoy-actions", "default", true)); err != nil {
				t.Fatalf("send headers: %v", err)
			}
			response, err := stream.Recv()
			if err != nil {
				t.Fatalf("receive response: %v", err)
			}
			events := auditor.eventsSnapshot()
			if len(events) != 1 || events[0].Action != guardrails.RuleAction(test.action) || events[0].DetectionCount != test.count {
				t.Fatalf("audit events = %+v", events)
			}
			encodedEvent, err := json.Marshal(events[0])
			if err != nil {
				t.Fatalf("marshal audit event: %v", err)
			}
			metadata := response.GetDynamicMetadata().String()
			if strings.Contains(string(encodedEvent), sensitiveFixture) || strings.Contains(metadata, sensitiveFixture) {
				t.Fatalf("sensitive fixture leaked: event=%s metadata=%s", encodedEvent, metadata)
			}
			if test.action == ActionBlock && response.GetImmediateResponse() == nil {
				t.Fatal("BLOCK did not use an immediate response")
			}
		})
	}
}

func TestServerAppliesRequestFailurePolicyWhenAuditSinkFails(t *testing.T) {
	for _, test := range []struct {
		name      string
		failure   policy.FailureMode
		wantBlock bool
	}{
		{name: "closed", failure: policy.FailureModeClosed, wantBlock: true},
		{name: "open", failure: policy.FailureModeOpen, wantBlock: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{
				PolicyID: "default", Version: 1,
				Definition: policy.PolicyDefinition{FailurePolicy: policy.FailurePolicy{Request: test.failure}},
			}}
			server, err := NewServerWithAuditor(processorFunc(func(context.Context, ProcessingRequest) (ProcessingResult, error) {
				return ProcessingResult{Action: ActionAllow}, nil
			}), cache, failingAuditor{})
			if err != nil {
				t.Fatalf("NewServerWithAuditor() error = %v", err)
			}
			client := newExternalProcessorTestClientForServer(t, server)
			stream, err := client.Process(context.Background())
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			if err := stream.Send(requestHeadersMessageWithEndOfStream("ignored", "envoy-audit-failure", "default", true)); err != nil {
				t.Fatalf("send headers: %v", err)
			}
			response, err := stream.Recv()
			if err != nil {
				t.Fatalf("receive response: %v", err)
			}
			if (response.GetImmediateResponse() != nil) != test.wantBlock {
				t.Fatalf("failure policy %q response = %+v", test.failure, response)
			}
			metadata := response.GetDynamicMetadata().GetFields()[safeMetadataNamespace].GetStructValue().GetFields()
			if got := metadata["action"].GetStringValue(); test.wantBlock && got != string(ActionBlock) {
				t.Fatalf("closed audit failure metadata action = %q, want BLOCK", got)
			}
		})
	}
}

func TestServerAppliesResponseFailurePolicyWhenAuditSinkFails(t *testing.T) {
	for _, test := range []struct {
		name      string
		failure   policy.FailureMode
		wantBlock bool
	}{
		{name: "closed", failure: policy.FailureModeClosed, wantBlock: true},
		{name: "open", failure: policy.FailureModeOpen, wantBlock: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{
				PolicyID: "default", Version: 1,
				Definition: policy.PolicyDefinition{FailurePolicy: policy.FailurePolicy{
					Request: policy.FailureModeOpen, Response: test.failure,
				}},
			}}
			server, err := NewServerWithAuditor(NewAllowProcessor(), cache, failingAuditor{})
			if err != nil {
				t.Fatalf("NewServerWithAuditor() error = %v", err)
			}
			stream, err := newExternalProcessorTestClientForServer(t, server).Process(context.Background())
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			if err := stream.Send(requestHeadersMessageWithEndOfStream("ignored", "envoy-response-audit-failure", "default", true)); err != nil {
				t.Fatalf("send request headers: %v", err)
			}
			if _, err := stream.Recv(); err != nil {
				t.Fatalf("receive request response: %v", err)
			}
			if err := stream.Send(responseHeadersForAdapterTest(true)); err != nil {
				t.Fatalf("send response headers: %v", err)
			}
			response, err := stream.Recv()
			if err != nil {
				t.Fatalf("receive response result: %v", err)
			}
			if (response.GetImmediateResponse() != nil) != test.wantBlock {
				t.Fatalf("response failure policy %q result = %+v", test.failure, response)
			}
			metadata := response.GetDynamicMetadata().GetFields()[safeMetadataNamespace].GetStructValue().GetFields()
			if !metadata["degraded"].GetBoolValue() {
				t.Fatalf("response audit failure metadata = %v, want degraded=true", metadata)
			}
		})
	}
}

func TestServerFailurePolicyOverridesDeploymentDefaultAndAuditsDegradedResult(t *testing.T) {
	for _, test := range []struct {
		name           string
		policyMode     policy.FailureMode
		deploymentMode policy.FailureMode
		wantBlock      bool
	}{
		{name: "policy closed overrides deployment open", policyMode: policy.FailureModeClosed, deploymentMode: policy.FailureModeOpen, wantBlock: true},
		{name: "policy open overrides deployment closed", policyMode: policy.FailureModeOpen, deploymentMode: policy.FailureModeClosed, wantBlock: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			auditor := &recordingAuditor{}
			cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: policy.PolicyDefinition{FailurePolicy: policy.FailurePolicy{Request: test.policyMode}}}}
			server, err := NewServerWithSettings(processorFunc(func(context.Context, ProcessingRequest) (ProcessingResult, error) {
				return ProcessingResult{}, errors.New("injected detector failure")
			}), cache, auditor, ServerSettings{FailMode: test.deploymentMode, MaxBodyBytes: 1024, ProcessingTimeout: time.Second})
			if err != nil {
				t.Fatalf("NewServerWithSettings() error = %v", err)
			}
			client := newExternalProcessorTestClientForServer(t, server)
			stream, err := client.Process(context.Background())
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			if err := stream.Send(requestHeadersMessageWithEndOfStream("ignored", "envoy-failure", "default", true)); err != nil {
				t.Fatalf("send: %v", err)
			}
			response, err := stream.Recv()
			if err != nil {
				t.Fatalf("receive: %v", err)
			}
			if (response.GetImmediateResponse() != nil) != test.wantBlock {
				t.Fatalf("response=%+v wantBlock=%v", response, test.wantBlock)
			}
			events := auditor.eventsSnapshot()
			wantAction := guardrails.RuleActionAllow
			if test.wantBlock {
				wantAction = guardrails.RuleActionBlock
			}
			if len(events) != 1 || !events[0].Degraded || events[0].Action != wantAction {
				t.Fatalf("degraded audit events=%+v", events)
			}
		})
	}
}

// A request processor error is never an implicit allow.  In the absence of a
// per-policy override, the deployment-level TSZ_FAIL_MODE is the authority.
func TestServerRequestProcessorErrorUsesDeploymentFailMode(t *testing.T) {
	for _, test := range []struct {
		name      string
		failMode  policy.FailureMode
		wantBlock bool
	}{
		{name: "closed blocks", failMode: policy.FailureModeClosed, wantBlock: true},
		{name: "open continues only when explicit", failMode: policy.FailureModeOpen, wantBlock: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			auditor := &recordingAuditor{}
			cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{PolicyID: "default", Version: 1}}
			server, err := NewServerWithSettings(processorFunc(func(context.Context, ProcessingRequest) (ProcessingResult, error) {
				return ProcessingResult{}, errors.New("injected detector failure")
			}), cache, auditor, ServerSettings{FailMode: test.failMode, MaxBodyBytes: 1024, ProcessingTimeout: time.Second})
			if err != nil {
				t.Fatalf("NewServerWithSettings() error = %v", err)
			}
			client := newExternalProcessorTestClientForServer(t, server)
			stream, err := client.Process(context.Background())
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			if err := stream.Send(requestHeadersMessageWithEndOfStream("ignored", "envoy-deployment-failure", "default", true)); err != nil {
				t.Fatalf("send: %v", err)
			}
			response, err := stream.Recv()
			if err != nil {
				t.Fatalf("receive: %v", err)
			}
			if (response.GetImmediateResponse() != nil) != test.wantBlock {
				t.Fatalf("response=%+v wantBlock=%v", response, test.wantBlock)
			}
			if !test.wantBlock && response.GetRequestHeaders() == nil {
				t.Fatalf("fail-open must continue request headers, got %+v", response)
			}
			events := auditor.eventsSnapshot()
			wantAction := guardrails.RuleActionAllow
			if test.wantBlock {
				wantAction = guardrails.RuleActionBlock
			}
			if len(events) != 1 || !events[0].Degraded || events[0].Action != wantAction {
				t.Fatalf("degraded audit events=%+v", events)
			}
		})
	}
}

func TestServerUsesResponseFailurePolicyForResponseProcessorFailures(t *testing.T) {
	for _, test := range []struct {
		name            string
		requestFailure  policy.FailureMode
		responseFailure policy.FailureMode
		wantBlock       bool
	}{
		{name: "response closed overrides request open", requestFailure: policy.FailureModeOpen, responseFailure: policy.FailureModeClosed, wantBlock: true},
		{name: "response open overrides request closed", requestFailure: policy.FailureModeClosed, responseFailure: policy.FailureModeOpen, wantBlock: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{
				PolicyID: "default", Version: 1,
				Definition: policy.PolicyDefinition{FailurePolicy: policy.FailurePolicy{
					Request: test.requestFailure, Response: test.responseFailure,
				}},
			}}
			server, err := NewServerWithSettings(processorFunc(func(_ context.Context, request ProcessingRequest) (ProcessingResult, error) {
				if request.Stage == StageResponse && request.Body != nil {
					return ProcessingResult{}, errors.New("injected response processor failure")
				}
				return ProcessingResult{Action: ActionAllow}, nil
			}), cache, nil, ServerSettings{FailMode: policy.FailureModeClosed, MaxBodyBytes: 1024, ProcessingTimeout: time.Second})
			if err != nil {
				t.Fatalf("NewServerWithSettings() error = %v", err)
			}
			client := newExternalProcessorTestClientForServer(t, server)
			stream, err := client.Process(context.Background())
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			for _, message := range []*extprocv3.ProcessingRequest{
				requestHeadersMessageWithEndOfStream("ignored", "envoy-response-failure", "default", true),
				responseHeadersForAdapterTest(false),
			} {
				if err := stream.Send(message); err != nil {
					t.Fatalf("send headers: %v", err)
				}
				if _, err := stream.Recv(); err != nil {
					t.Fatalf("receive headers response: %v", err)
				}
			}
			if err := stream.Send(responseBodyForAdapterTest([]byte("response"), true)); err != nil {
				t.Fatalf("send response body: %v", err)
			}
			response, err := stream.Recv()
			if err != nil {
				t.Fatalf("receive response body result: %v", err)
			}
			if (response.GetImmediateResponse() != nil) != test.wantBlock {
				t.Fatalf("request=%q response=%q result=%+v wantBlock=%v", test.requestFailure, test.responseFailure, response, test.wantBlock)
			}
			if !test.wantBlock && response.GetResponseBody() == nil {
				t.Fatalf("fail-open response must continue, got %+v", response)
			}
		})
	}
}

func TestServerRejectsOversizeBodyWith413BeforeUpstream(t *testing.T) {
	upstream := &mockUpstream{}
	cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{PolicyID: "default", Version: 1}}
	server, err := NewServerWithSettings(processorFunc(func(_ context.Context, request ProcessingRequest) (ProcessingResult, error) {
		if request.Body != nil {
			upstream.forward(nil)
		}
		return ProcessingResult{Action: ActionAllow}, nil
	}), cache, nil, ServerSettings{FailMode: policy.FailureModeOpen, MaxBodyBytes: 4, ProcessingTimeout: time.Second})
	if err != nil {
		t.Fatalf("NewServerWithSettings() error = %v", err)
	}
	client := newExternalProcessorTestClientForServer(t, server)
	stream, err := client.Process(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := stream.Send(requestHeadersMessage("ignored", "envoy-limit", "default")); err != nil {
		t.Fatalf("send headers: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("receive headers: %v", err)
	}
	if err := stream.Send(requestBodyForAdapterTest([]byte("12345"), true)); err != nil {
		t.Fatalf("send body: %v", err)
	}
	response, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive body: %v", err)
	}
	if response.GetImmediateResponse().GetStatus().GetCode() != typev3.StatusCode_PayloadTooLarge || upstream.requests.Load() != 0 {
		t.Fatalf("413 response=%+v upstream=%d", response, upstream.requests.Load())
	}
}

func TestServerRejectsOversizeResponseBeforeProcessorOrClient(t *testing.T) {
	for _, test := range []struct {
		name     string
		declared bool
	}{
		{name: "declared content length", declared: true},
		{name: "buffered body without content length"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var processedResponseBody atomic.Int64
			server, err := NewServerWithSettings(processorFunc(func(_ context.Context, request ProcessingRequest) (ProcessingResult, error) {
				if request.Stage == StageResponse && request.Body != nil {
					processedResponseBody.Add(1)
				}
				return ProcessingResult{Action: ActionAllow}, nil
			}), snapshotPolicyCache{snapshot: policy.CompiledSnapshot{PolicyID: "default", Version: 1}}, nil, ServerSettings{FailMode: policy.FailureModeOpen, MaxBodyBytes: 4, ProcessingTimeout: time.Second})
			if err != nil {
				t.Fatalf("NewServerWithSettings() error = %v", err)
			}
			client := newExternalProcessorTestClientForServer(t, server)
			stream, err := client.Process(context.Background())
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			if err := stream.Send(requestHeadersMessageWithEndOfStream("ignored", "envoy-response-limit", "default", true)); err != nil {
				t.Fatalf("send request headers: %v", err)
			}
			if _, err := stream.Recv(); err != nil {
				t.Fatalf("receive request headers: %v", err)
			}
			responseHeaders := responseHeadersForAdapterTest(false)
			if test.declared {
				responseHeaders.GetResponseHeaders().GetHeaders().Headers = append(responseHeaders.GetResponseHeaders().GetHeaders().Headers,
					&corev3.HeaderValue{Key: "content-length", RawValue: []byte("5")},
				)
			}
			if err := stream.Send(responseHeaders); err != nil {
				t.Fatalf("send response headers: %v", err)
			}
			response, err := stream.Recv()
			if err != nil {
				t.Fatalf("receive response headers result: %v", err)
			}
			if test.declared {
				assertOversizedResponse(t, response, "response body must be rejected from content-length")
				if processedResponseBody.Load() != 0 {
					t.Fatalf("processor saw oversized declared response body %d times", processedResponseBody.Load())
				}
				return
			}
			if response.GetResponseHeaders() == nil {
				t.Fatalf("response headers were not allowed: %+v", response)
			}
			if err := stream.Send(responseBodyForAdapterTest([]byte("12345"), true)); err != nil {
				t.Fatalf("send response body: %v", err)
			}
			response, err = stream.Recv()
			if err != nil {
				t.Fatalf("receive response body result: %v", err)
			}
			assertOversizedResponse(t, response, "buffered response body must be rejected")
			if processedResponseBody.Load() != 0 {
				t.Fatalf("processor saw oversized buffered response body %d times", processedResponseBody.Load())
			}
		})
	}
}

func assertOversizedResponse(t *testing.T, response *extprocv3.ProcessingResponse, context string) {
	t.Helper()
	immediate := response.GetImmediateResponse()
	if immediate == nil || immediate.GetStatus().GetCode() != typev3.StatusCode_BadGateway {
		t.Fatalf("%s: response=%+v", context, response)
	}
	var payload blockErrorResponse
	if err := json.Unmarshal(immediate.GetBody(), &payload); err != nil {
		t.Fatalf("%s: decode body: %v; body=%q", context, err, immediate.GetBody())
	}
	if payload.Error.Code != "TSZ_RESPONSE_BODY_TOO_LARGE" {
		t.Fatalf("%s: payload=%+v", context, payload)
	}
}

func TestServerProcessingTimeoutCancelsProcessorDeterministically(t *testing.T) {
	exited := make(chan struct{}, 1)
	cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: policy.PolicyDefinition{FailurePolicy: policy.FailurePolicy{Request: policy.FailureModeClosed}}}}
	server, err := NewServerWithSettings(processorFunc(func(ctx context.Context, _ ProcessingRequest) (ProcessingResult, error) {
		<-ctx.Done()
		exited <- struct{}{}
		return ProcessingResult{}, ctx.Err()
	}), cache, nil, ServerSettings{FailMode: policy.FailureModeOpen, MaxBodyBytes: 1024, ProcessingTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewServerWithSettings() error = %v", err)
	}
	client := newExternalProcessorTestClientForServer(t, server)
	stream, err := client.Process(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := stream.Send(requestHeadersMessageWithEndOfStream("ignored", "envoy-timeout", "default", true)); err != nil {
		t.Fatalf("send: %v", err)
	}
	response, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if response.GetImmediateResponse() == nil {
		t.Fatalf("timeout did not apply closed policy: %+v", response)
	}
	awaitSignal(t, exited, "processor timeout cancellation")
}

func TestServerHandlesEmptyBodyEndOfStreamAndInvalidMessages(t *testing.T) {
	t.Run("empty body and end of stream", func(t *testing.T) {
		processor := &recordingProcessor{}
		client := newExternalProcessorTestClient(t, processor, newKeyedPolicyCache(nil))
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("open stream: %v", err)
		}
		if err := exchangeMessages(stream, []*extprocv3.ProcessingRequest{
			requestHeadersMessage("rid-empty", "envoy-empty", "default"),
			requestBodyForAdapterTest(nil, true),
		}, 0); err != nil {
			t.Fatalf("empty body exchange: %v", err)
		}
		if err := stream.CloseSend(); err != nil {
			t.Fatalf("close stream: %v", err)
		}
		if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
			t.Fatalf("receive after client end of stream = %v, want EOF", err)
		}
		processor.mu.Lock()
		defer processor.mu.Unlock()
		if len(processor.requests) != 2 || len(processor.requests[1].Body) != 0 {
			t.Fatalf("empty body processor requests = %+v", processor.requests)
		}
	})

	for _, test := range []struct {
		name    string
		message *extprocv3.ProcessingRequest
	}{
		{name: "empty message", message: &extprocv3.ProcessingRequest{}},
		{name: "body before request headers", message: requestBodyForAdapterTest([]byte("unexpected"), true)},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newExternalProcessorTestClient(t, NewAllowProcessor(), newKeyedPolicyCache(nil))
			stream, err := client.Process(context.Background())
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			if err := stream.Send(test.message); err != nil {
				t.Fatalf("send invalid message: %v", err)
			}
			if _, err := stream.Recv(); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("invalid message error = %v (%s), want INVALID_ARGUMENT", err, status.Code(err))
			}
		})
	}
}

func TestServerPropagatesClientTimeoutToProcessor(t *testing.T) {
	processor := newCancellationProcessor(1)
	client := newExternalProcessorTestClient(t, processor, newKeyedPolicyCache(nil))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	stream, err := client.Process(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := stream.Send(requestHeadersMessageWithEndOfStream("rid-timeout", "envoy-timeout", "default", true)); err != nil {
		t.Fatalf("send headers: %v", err)
	}
	awaitSignal(t, processor.started, "processor start before client timeout")
	if _, err := stream.Recv(); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("timeout error = %v (%s), want DEADLINE_EXCEEDED", err, status.Code(err))
	}
	awaitSignal(t, processor.exited, "processor exit after client timeout")
}

func TestServerCancelsWindowedSSEProcessingWhenClientDisconnects(t *testing.T) {
	processor := cancellationWindowProcessor{newCancellationProcessor(1)}
	cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{
		PolicyID: "default",
		Version:  1,
		Definition: policy.PolicyDefinition{
			Response:  policy.ResponsePolicy{Enabled: true},
			Streaming: policy.StreamingSettings{Mode: policy.StreamingModeWindowed, WindowBytes: 1},
		},
	}}
	client := newExternalProcessorTestClient(t, processor, cache)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.Process(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := stream.Send(requestHeadersMessageWithEndOfStream("ignored", "envoy-sse-cancel", "default", true)); err != nil {
		t.Fatalf("send request headers: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("receive request headers response: %v", err)
	}
	responseHeaders := responseHeadersForAdapterTest(false)
	responseHeaders.GetResponseHeaders().Headers.Headers[0].RawValue = []byte("text/event-stream")
	if err := stream.Send(responseHeaders); err != nil {
		t.Fatalf("send response headers: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("receive response headers response: %v", err)
	}
	if err := stream.Send(responseBodyForAdapterTest([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"), true)); err != nil {
		t.Fatalf("send SSE response body: %v", err)
	}
	awaitSignal(t, processor.started, "windowed SSE processor start before client disconnect")

	// Envoy cancelling its ext_proc RPC is how a downstream disconnect reaches
	// the processor. The in-flight window must inherit that cancellation.
	cancel()
	if _, err := stream.Recv(); status.Code(err) != codes.Canceled {
		t.Fatalf("disconnect error = %v (%s), want CANCELED", err, status.Code(err))
	}
	awaitSignal(t, processor.exited, "windowed SSE processor exit after client disconnect")
}

func TestWindowedSSECancellationReleasesPermitBeforeOperationalAuditCompletes(t *testing.T) {
	processor := cancellationWindowProcessor{newCancellationProcessor(1)}
	auditor := blockingOperationalAuditor{started: make(chan struct{}, 1), release: make(chan struct{})}
	defer close(auditor.release)
	cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: policy.PolicyDefinition{
		Response:  policy.ResponsePolicy{Enabled: true},
		Streaming: policy.StreamingSettings{Mode: policy.StreamingModeWindowed, WindowBytes: 1},
	}}}
	server, err := NewServerWithSettings(processor, cache, auditor, ServerSettings{FailMode: policy.FailureModeOpen, MaxBodyBytes: 1024, ProcessingTimeout: time.Second}, 1)
	if err != nil {
		t.Fatalf("NewServerWithSettings: %v", err)
	}
	client := newExternalProcessorTestClientForServer(t, server)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.Process(ctx)
	if err != nil {
		t.Fatalf("open first stream: %v", err)
	}
	if err := stream.Send(requestHeadersMessageWithEndOfStream("ignored", "envoy-permit-cancel", "default", true)); err != nil {
		t.Fatalf("send first request headers: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("receive first request headers response: %v", err)
	}
	responseHeaders := responseHeadersForAdapterTest(false)
	responseHeaders.GetResponseHeaders().Headers.Headers[0].RawValue = []byte("text/event-stream")
	if err := stream.Send(responseHeaders); err != nil {
		t.Fatalf("send first response headers: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("receive first response headers response: %v", err)
	}
	if err := stream.Send(responseBodyForAdapterTest([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"), true)); err != nil {
		t.Fatalf("send first SSE body: %v", err)
	}
	awaitSignal(t, processor.started, "first windowed SSE processor start")
	cancel()
	if _, err := stream.Recv(); status.Code(err) != codes.Canceled {
		t.Fatalf("first stream cancellation = %v (%s), want CANCELED", err, status.Code(err))
	}
	awaitSignal(t, processor.exited, "first windowed SSE processor exit")
	awaitSignal(t, auditor.started, "operational audit dispatch")

	// The audit worker is intentionally blocked. A second stream proves the
	// canceled stream's permit was released before audit delivery completed.
	secondCtx, cancelSecond := context.WithTimeout(context.Background(), time.Second)
	defer cancelSecond()
	second, err := client.Process(secondCtx)
	if err != nil {
		t.Fatalf("open second stream: %v", err)
	}
	if err := second.Send(requestHeadersMessageWithEndOfStream("ignored", "envoy-permit-next", "default", true)); err != nil {
		t.Fatalf("send second request headers: %v", err)
	}
	if _, err := second.Recv(); err != nil {
		t.Fatalf("second stream did not acquire released permit while audit was blocked: %v", err)
	}
}

func TestServerFailsOpenForWindowedSSEProcessorError(t *testing.T) {
	cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: policy.PolicyDefinition{
		Response: policy.ResponsePolicy{Enabled: true}, Streaming: policy.StreamingSettings{Mode: policy.StreamingModeWindowed, WindowBytes: 1},
	}}}
	server, err := NewServerWithSettings(failingWindowProcessor{}, cache, nil, ServerSettings{FailMode: policy.FailureModeOpen, MaxBodyBytes: 1024, ProcessingTimeout: time.Second})
	if err != nil {
		t.Fatalf("NewServerWithSettings: %v", err)
	}
	stream, err := newExternalProcessorTestClientForServer(t, server).Process(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	for _, message := range []*extprocv3.ProcessingRequest{requestHeadersMessageWithEndOfStream("ignored", "envoy-open", "default", true), responseHeadersForAdapterTest(false)} {
		if message.GetResponseHeaders() != nil {
			message.GetResponseHeaders().Headers.Headers[0].RawValue = []byte("text/event-stream")
		}
		if err := stream.Send(message); err != nil {
			t.Fatalf("send setup: %v", err)
		}
		if _, err := stream.Recv(); err != nil {
			t.Fatalf("receive setup: %v", err)
		}
	}
	raw := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"unchanged\"}}]}\n\n")
	if err := stream.Send(responseBodyForAdapterTest(raw, true)); err != nil {
		t.Fatalf("send SSE: %v", err)
	}
	response, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive SSE: %v", err)
	}
	if got := response.GetResponseBody().GetResponse().GetBodyMutation().GetBody(); string(got) != string(raw) {
		t.Fatalf("fail-open body = %q, want %q", got, raw)
	}
	metadata := response.GetDynamicMetadata().GetFields()[safeMetadataNamespace].GetStructValue().GetFields()
	if !metadata["degraded"].GetBoolValue() {
		t.Fatalf("fail-open response metadata = %v, want degraded=true", metadata)
	}
}

func TestServerHaltsWindowedSSEWithSafeImmediateResponse(t *testing.T) {
	cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{PolicyID: "default", Version: 7, Definition: policy.PolicyDefinition{
		Response:  policy.ResponsePolicy{Enabled: true, Secret: policy.ActionBlock},
		Streaming: policy.StreamingSettings{Mode: policy.StreamingModeWindowed, WindowBytes: 1},
	}}}
	auditor := &recordingAuditor{}
	server, err := NewServerWithSettings(blockingWindowProcessor{}, cache, auditor, ServerSettings{FailMode: policy.FailureModeClosed, MaxBodyBytes: 1024, ProcessingTimeout: time.Second})
	if err != nil {
		t.Fatalf("NewServerWithSettings: %v", err)
	}
	stream, err := newExternalProcessorTestClientForServer(t, server).Process(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	for _, message := range []*extprocv3.ProcessingRequest{requestHeadersMessageWithEndOfStream("ignored", "envoy-halt", "default", true), responseHeadersForAdapterTest(false)} {
		if message.GetResponseHeaders() != nil {
			message.GetResponseHeaders().Headers.Headers[0].RawValue = []byte("text/event-stream")
		}
		if err := stream.Send(message); err != nil {
			t.Fatalf("send setup: %v", err)
		}
		if _, err := stream.Recv(); err != nil {
			t.Fatalf("receive setup: %v", err)
		}
	}
	unsafe := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"unsafe streamed value\"}}]}\n\ndata: [DONE]\n\n")
	if err := stream.Send(responseBodyForAdapterTest(unsafe, true)); err != nil {
		t.Fatalf("send SSE: %v", err)
	}
	response, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive SSE halt: %v", err)
	}
	immediate := response.GetImmediateResponse()
	if immediate == nil || immediate.GetStatus().GetCode() != typev3.StatusCode_Forbidden {
		t.Fatalf("halt response = %+v, want safe 403", response)
	}
	if strings.Contains(string(immediate.GetBody()), "unsafe streamed value") {
		t.Fatalf("halt response leaked unsafe SSE content: %q", immediate.GetBody())
	}
	var payload blockErrorResponse
	if err := json.Unmarshal(immediate.GetBody(), &payload); err != nil || payload.Error.Code != "TSZ_RESPONSE_GUARDRAIL_BLOCKED" || payload.TSZMeta.PolicyVersion != 7 {
		t.Fatalf("halt payload = %+v, error = %v", payload, err)
	}
	events := auditor.eventsSnapshot()
	if len(events) != 3 {
		t.Fatalf("audit event count = %d, want 3: %+v", len(events), events)
	}
	streamEvent := events[2]
	if streamEvent.Stage != guardrails.AuditStageResponse || streamEvent.Action != guardrails.RuleActionBlock ||
		streamEvent.PolicyVersion != 7 || streamEvent.DetectionCount != 1 || strings.Join(streamEvent.Categories, ",") != "SECRET" {
		t.Fatalf("stream halt audit event = %+v", streamEvent)
	}
}

func TestServerDropsOperationalAuditWhenQueueIsFullWithoutBlocking(t *testing.T) {
	server, err := NewServer(NewAllowProcessor(), newKeyedPolicyCache(nil))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()
	server.Close()
	for range cap(server.operationalAudits) {
		server.operationalAudits <- guardrails.AuditEvent{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { server.enqueueCancellationAudit(ctx, newStreamState()); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("full operational audit queue blocked cancellation handling")
	}
	if got, want := len(server.operationalAudits), cap(server.operationalAudits); got != want {
		t.Fatalf("queue len = %d, want %d; event was not dropped", got, want)
	}
}

func TestServerPinsTrustedRoutePolicyAndRejectsInvalidPolicyHeaders(t *testing.T) {
	t.Run("guardrail override does not replace route policy", func(t *testing.T) {
		processor := &recordingProcessor{}
		client := newExternalProcessorTestClient(t, processor, newKeyedPolicyCache(map[string]int{"required-route-policy": 4}))
		stream, err := client.Process(context.Background())
		if err != nil {
			t.Fatalf("open stream: %v", err)
		}
		message := requestHeadersMessageWithEndOfStream("rid-policy", "envoy-policy", "required-route-policy", true)
		message.GetRequestHeaders().GetHeaders().Headers = append(message.GetRequestHeaders().GetHeaders().Headers,
			&corev3.HeaderValue{Key: "x-tsz-guardrails", RawValue: []byte("none")},
		)
		if err := stream.Send(message); err != nil {
			t.Fatalf("send headers: %v", err)
		}
		if _, err := stream.Recv(); err != nil {
			t.Fatalf("receive response: %v", err)
		}
		processor.mu.Lock()
		defer processor.mu.Unlock()
		if len(processor.requests) != 1 || processor.requests[0].PolicyID != "required-route-policy" || processor.requests[0].PolicyVersion != 4 {
			t.Fatalf("pinned request = %+v", processor.requests)
		}
	})

	for _, test := range []struct {
		name      string
		policy    string
		duplicate bool
	}{
		{name: "empty policy", policy: ""},
		{name: "duplicate policy", policy: "required-route-policy", duplicate: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newExternalProcessorTestClient(t, NewAllowProcessor(), newKeyedPolicyCache(nil))
			stream, err := client.Process(context.Background())
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			message := requestHeadersMessageWithEndOfStream("rid-invalid-policy", "envoy-invalid-policy", test.policy, true)
			if test.duplicate {
				message.GetRequestHeaders().GetHeaders().Headers = append(message.GetRequestHeaders().GetHeaders().Headers,
					&corev3.HeaderValue{Key: "X-TSZ-Policy", RawValue: []byte("other-policy")},
				)
			}
			if err := stream.Send(message); err != nil {
				t.Fatalf("send headers: %v", err)
			}
			if _, err := stream.Recv(); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("invalid policy error = %v (%s), want INVALID_ARGUMENT", err, status.Code(err))
			}
		})
	}
}

func TestConcurrentStreamsKeepRIDAndPolicyIsolated(t *testing.T) {
	processor := &recordingProcessor{}
	policyCache := newKeyedPolicyCache(map[string]int{"policy-a": 3, "policy-b": 8})
	client := newExternalProcessorTestClient(t, processor, policyCache)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := make(chan struct{})
	errors := make(chan error, 2)
	streams := []struct {
		rid        string
		envoyReqID string
		policyID   string
	}{
		{rid: "rid-a", envoyReqID: "envoy-a", policyID: "policy-a"},
		{rid: "rid-b", envoyReqID: "envoy-b", policyID: "policy-b"},
	}
	for _, spec := range streams {
		spec := spec
		go func() {
			<-start
			errors <- runAllowStream(ctx, client, spec.rid, spec.envoyReqID, spec.policyID)
		}()
	}
	close(start)
	for range streams {
		if err := <-errors; err != nil {
			t.Fatalf("concurrent stream failed: %v", err)
		}
	}

	grouped := processor.requestsByRID()
	seenRIDs := make(map[string]struct{}, 2)
	for rid, requests := range grouped {
		seenRIDs[rid] = struct{}{}
		switch requests[0].EnvoyReqID {
		case "envoy-a":
			assertStreamIdentity(t, requests, "envoy-a", "policy-a", 3)
		case "envoy-b":
			assertStreamIdentity(t, requests, "envoy-b", "policy-b", 8)
		default:
			t.Fatalf("unexpected Envoy request ID in stream: %+v", requests[0])
		}
	}
	if len(grouped) != 2 {
		t.Fatalf("observed RID groups = %v, want two isolated streams", mapKeys(grouped))
	}
	if len(seenRIDs) != 2 {
		t.Fatalf("BYG RIDs were shared between streams: %v", mapKeys(grouped))
	}
	if got := policyCache.getCount("policy-a"); got != 1 {
		t.Fatalf("policy-a cache reads = %d, want 1", got)
	}
	if got := policyCache.getCount("policy-b"); got != 1 {
		t.Fatalf("policy-b cache reads = %d, want 1", got)
	}
}

func TestOpenStreamKeepsPinnedVersionWhileNewStreamUsesUpdatedVersion(t *testing.T) {
	processor := &recordingProcessor{}
	policyCache := newKeyedPolicyCache(map[string]int{"default": 1})
	client := newExternalProcessorTestClient(t, processor, policyCache)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	oldStream, err := client.Process(ctx)
	if err != nil {
		t.Fatalf("open old stream: %v", err)
	}
	if err := exchangeMessages(oldStream, []*extprocv3.ProcessingRequest{requestHeadersMessage("rid-old", "envoy-old", "default")}, 0); err != nil {
		t.Fatalf("pin old stream: %v", err)
	}

	policyCache.setVersion("default", 2)
	errors := make(chan error, 2)
	go func() {
		err := exchangeMessages(oldStream, remainingStreamMessages(), 1)
		if err == nil {
			err = oldStream.CloseSend()
		}
		errors <- err
	}()
	go func() {
		errors <- runAllowStream(ctx, client, "rid-new", "envoy-new", "default")
	}()
	for index := 0; index < 2; index++ {
		if err := <-errors; err != nil {
			t.Fatalf("stream after cache update failed: %v", err)
		}
	}

	grouped := processor.requestsByRID()
	for _, requests := range grouped {
		switch requests[0].EnvoyReqID {
		case "envoy-old":
			assertStreamIdentity(t, requests, "envoy-old", "default", 1)
		case "envoy-new":
			assertStreamIdentity(t, requests, "envoy-new", "default", 2)
		default:
			t.Fatalf("unexpected Envoy request ID in stream: %+v", requests[0])
		}
	}
	if len(grouped) != 2 {
		t.Fatalf("observed RID groups = %v, want two isolated streams", mapKeys(grouped))
	}
	if got := policyCache.getCount("default"); got != 2 {
		t.Fatalf("default cache reads = %d, want one per stream (2)", got)
	}
}

func TestThirdConcurrentStreamIsRejectedAndCancellationReleasesPermits(t *testing.T) {
	processor := newCancellationProcessor(3)
	client := newExternalProcessorTestClient(t, processor, newKeyedPolicyCache(map[string]int{"default": 1}), 2)

	firstContext, cancelFirst := context.WithCancel(context.Background())
	firstStream, err := client.Process(firstContext)
	if err != nil {
		t.Fatalf("open first stream: %v", err)
	}
	if err := firstStream.Send(requestHeadersMessage("rid-first", "envoy-first", "default")); err != nil {
		t.Fatalf("send first stream headers: %v", err)
	}
	awaitSignal(t, processor.started, "first processor start")

	secondContext, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	secondStream, err := client.Process(secondContext)
	if err != nil {
		t.Fatalf("open second stream: %v", err)
	}
	if err := secondStream.Send(requestHeadersMessage("rid-second", "envoy-second", "default")); err != nil {
		t.Fatalf("send second stream headers: %v", err)
	}
	awaitSignal(t, processor.started, "second processor start")

	thirdContext, cancelThird := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelThird()
	thirdStream, err := client.Process(thirdContext)
	if err != nil {
		t.Fatalf("open third stream: %v", err)
	}
	if err := thirdStream.Send(requestHeadersMessage("rid-third", "envoy-third", "default")); err != nil {
		t.Fatalf("send third stream headers: %v", err)
	}
	if _, err := thirdStream.Recv(); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("third stream error = %v (%s), want RESOURCE_EXHAUSTED", err, status.Code(err))
	}

	cancelFirst()
	cancelSecond()
	_, _ = firstStream.Recv()
	_, _ = secondStream.Recv()
	awaitSignal(t, processor.exited, "first processor cancellation")
	awaitSignal(t, processor.exited, "second processor cancellation")

	fourthContext, cancelFourth := context.WithCancel(context.Background())
	fourthStream, err := client.Process(fourthContext)
	if err != nil {
		t.Fatalf("open fourth stream after permit release: %v", err)
	}
	if err := fourthStream.Send(requestHeadersMessage("rid-fourth", "envoy-fourth", "default")); err != nil {
		t.Fatalf("send fourth stream headers: %v", err)
	}
	awaitSignal(t, processor.started, "fourth processor start after permit release")
	cancelFourth()
	_, _ = fourthStream.Recv()
	awaitSignal(t, processor.exited, "fourth processor cancellation")
}

func TestNewServerRejectsZeroStreamLimit(t *testing.T) {
	_, err := NewServer(NewAllowProcessor(), newKeyedPolicyCache(nil), 0)
	if err == nil {
		t.Fatal("NewServer() error = nil, want invalid stream limit error")
	}
}

func awaitSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func assertStreamIdentity(t *testing.T, requests []ProcessingRequest, envoyReqID, policyID string, version int) {
	t.Helper()
	if len(requests) != 4 {
		t.Fatalf("Envoy request ID %q processor requests = %d, want 4", envoyReqID, len(requests))
	}
	rid := requests[0].RID
	if !strings.HasPrefix(rid, "RID-") {
		t.Fatalf("BYG RID = %q, want RID-*", rid)
	}
	for index, request := range requests {
		if request.RID != rid || request.EnvoyReqID != envoyReqID || request.PolicyID != policyID || request.PolicyVersion != version {
			t.Fatalf("RID %q request %d identity/version = %+v", rid, index, request)
		}
		if request.PolicySnapshot == nil || request.PolicySnapshot.PolicyID != policyID || request.PolicySnapshot.Version != version {
			t.Fatalf("RID %q request %d snapshot = %+v", rid, index, request.PolicySnapshot)
		}
	}
}

func mapKeys(values map[string][]ProcessingRequest) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func TestAllowProcessorAllowsEveryValidStage(t *testing.T) {
	processor := NewAllowProcessor()
	for _, stage := range []ProcessingStage{StageRequest, StageResponse} {
		result, err := processor.Process(context.Background(), ProcessingRequest{Stage: stage})
		if err != nil || result.Action != ActionAllow {
			t.Fatalf("Process(%s) result=%+v error=%v", stage, result, err)
		}
	}
}
