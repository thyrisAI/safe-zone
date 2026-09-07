package envoy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	. "thyris-sz/internal/extproc"
	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

// streamState belongs to one Process invocation only. It is never shared
// across streams or stored in package-level mutable state.
type streamState struct {
	rid                 string
	envoyReqID          string
	traceID             string
	traceParent         string
	policyID            string
	policySnapshot      policy.CompiledSnapshot
	hasPolicySnapshot   bool
	policyPinned        bool
	requestFailureMode  policy.FailureMode
	responseFailureMode policy.FailureMode
	protocol            *envoyStreamState
	responseOnlyAction  Action
	responseOnlyHandled bool
}

type asyncAuditJob struct {
	request ProcessingRequest
	events  []OpenAISSEEvent
}

func newStreamState() *streamState {
	return &streamState{rid: NewBYGRID(), protocol: newEnvoyStreamState()}
}

// Server implements Envoy's bidirectional ext-proc stream and delegates all
// decisions to the injected gateway-neutral Processor.
type Server struct {
	extprocv3.UnimplementedExternalProcessorServer
	processor             Processor
	policyCache           PolicyCache
	policyResolver        PolicyResolver
	auditor               guardrails.Auditor
	streamPermit          chan struct{}
	defaultFailureMode    policy.FailureMode
	maxBodyBytes          int64
	maxStreamBufferBytes  int
	processingTimeout     time.Duration
	responseStateObserver ResponseStateObserver
	metricsObserver       MetricsObserver
	traceObserver         TraceObserver
	operationalAudits     chan guardrails.AuditEvent
	asyncAuditJobs        chan asyncAuditJob
	operationalStop       context.CancelFunc
	operationalWorkers    sync.WaitGroup
	closeOnce             sync.Once
}

// ResponseStateObserver records a bounded operational outcome without
// receiving any request or response content.
type ResponseStateObserver interface {
	ObserveResponseWithoutRequestState(outcome string)
}

type noopResponseStateObserver struct{}

func (noopResponseStateObserver) ObserveResponseWithoutRequestState(string) {}

// MetricsObserver records only aggregate, PII-safe operational data. All
// parameters are bounded enums or policy IDs controlled by the operator.
type MetricsObserver interface {
	IncRequest()
	IncResponse()
	ObserveAction(Action, ProcessingStage, string)
	ObserveDetections([]string, ProcessingStage)
	ObserveDuration(ProcessingStage, float64)
	IncFailure(string)
	IncTimeout()
	ObserveBodyBytes(string, int)
	IncActiveStreams()
	DecActiveStreams()
	IncStreamHalt()
}

type TraceObserver interface {
	StartGuardrail(context.Context, ProcessingRequest) (context.Context, func(ProcessingResult, error))
}

type noopTraceObserver struct{}

func (noopTraceObserver) StartGuardrail(ctx context.Context, _ ProcessingRequest) (context.Context, func(ProcessingResult, error)) {
	return ctx, func(ProcessingResult, error) {}
}

type noopMetricsObserver struct{}

func (noopMetricsObserver) IncRequest()                                   {}
func (noopMetricsObserver) IncResponse()                                  {}
func (noopMetricsObserver) ObserveAction(Action, ProcessingStage, string) {}
func (noopMetricsObserver) ObserveDetections([]string, ProcessingStage)   {}
func (noopMetricsObserver) ObserveDuration(ProcessingStage, float64)      {}
func (noopMetricsObserver) IncFailure(string)                             {}
func (noopMetricsObserver) IncTimeout()                                   {}
func (noopMetricsObserver) ObserveBodyBytes(string, int)                  {}
func (noopMetricsObserver) IncActiveStreams()                             {}
func (noopMetricsObserver) DecActiveStreams()                             {}
func (noopMetricsObserver) IncStreamHalt()                                {}

// Register binds this Envoy-specific adapter to a generic gRPC server.
func (s *Server) Register(server *grpc.Server) {
	extprocv3.RegisterExternalProcessorServer(server, s)
}

type ServerSettings struct {
	FailMode     policy.FailureMode
	MaxBodyBytes int64
	// MaxStreamBufferBytes bounds the per-stream SSE parser and window queue.
	// The Process loop sends a response before Recv'ing the next message, so it
	// also provides transport-level backpressure to Envoy.
	MaxStreamBufferBytes  int
	ProcessingTimeout     time.Duration
	ResponseStateObserver ResponseStateObserver
	MetricsObserver       MetricsObserver
	TraceObserver         TraceObserver
}

func defaultServerSettings() ServerSettings {
	return ServerSettings{FailMode: policy.FailureModeClosed, MaxBodyBytes: 1024 * 1024, MaxStreamBufferBytes: 256 * 1024, ProcessingTimeout: 2 * time.Second}
}

func NewServer(processor Processor, policyCache PolicyCache, maxConcurrentStreams ...uint32) (*Server, error) {
	return NewServerWithAuditor(processor, policyCache, nil, maxConcurrentStreams...)
}

func NewServerWithPolicyResolver(processor Processor, policyCache PolicyCache, resolver PolicyResolver, maxConcurrentStreams ...uint32) (*Server, error) {
	return newServer(processor, policyCache, resolver, nil, defaultServerSettings(), maxConcurrentStreams...)
}

func NewServerWithAuditor(processor Processor, policyCache PolicyCache, auditor guardrails.Auditor, maxConcurrentStreams ...uint32) (*Server, error) {
	return newServer(processor, policyCache, HeaderPolicyResolver{}, auditor, defaultServerSettings(), maxConcurrentStreams...)
}

func NewServerWithSettings(processor Processor, policyCache PolicyCache, auditor guardrails.Auditor, settings ServerSettings, maxConcurrentStreams ...uint32) (*Server, error) {
	return newServer(processor, policyCache, HeaderPolicyResolver{}, auditor, settings, maxConcurrentStreams...)
}

func NewServerWithResolverAndSettings(processor Processor, policyCache PolicyCache, resolver PolicyResolver, auditor guardrails.Auditor, settings ServerSettings, maxConcurrentStreams ...uint32) (*Server, error) {
	return newServer(processor, policyCache, resolver, auditor, settings, maxConcurrentStreams...)
}

func newServer(processor Processor, policyCache PolicyCache, resolver PolicyResolver, auditor guardrails.Auditor, settings ServerSettings, maxConcurrentStreams ...uint32) (*Server, error) {
	if processor == nil {
		return nil, errors.New("ext-proc processor is required")
	}
	if policyCache == nil {
		return nil, errors.New("policy cache is required")
	}
	if resolver == nil {
		return nil, errors.New("policy resolver is required")
	}
	if len(maxConcurrentStreams) > 1 {
		return nil, errors.New("at most one max concurrent streams value is allowed")
	}
	limit := uint32(100)
	if len(maxConcurrentStreams) == 1 {
		limit = maxConcurrentStreams[0]
	}
	if limit == 0 {
		return nil, errors.New("max concurrent streams must be greater than zero")
	}
	if auditor == nil {
		auditor = guardrails.NoopAuditor{}
	}
	defaults := defaultServerSettings()
	if settings.FailMode == "" {
		settings.FailMode = defaults.FailMode
	}
	if settings.MaxBodyBytes <= 0 {
		settings.MaxBodyBytes = defaults.MaxBodyBytes
	}
	if settings.MaxStreamBufferBytes <= 0 {
		settings.MaxStreamBufferBytes = defaults.MaxStreamBufferBytes
	}
	if settings.ProcessingTimeout <= 0 {
		settings.ProcessingTimeout = defaults.ProcessingTimeout
	}
	if settings.ResponseStateObserver == nil {
		settings.ResponseStateObserver = noopResponseStateObserver{}
	}
	if settings.MetricsObserver == nil {
		settings.MetricsObserver = noopMetricsObserver{}
	}
	if settings.TraceObserver == nil {
		settings.TraceObserver = noopTraceObserver{}
	}
	server := &Server{
		processor:          processor,
		policyCache:        policyCache,
		policyResolver:     resolver,
		auditor:            auditor,
		streamPermit:       make(chan struct{}, limit),
		defaultFailureMode: settings.FailMode, maxBodyBytes: settings.MaxBodyBytes, maxStreamBufferBytes: settings.MaxStreamBufferBytes, processingTimeout: settings.ProcessingTimeout,
		responseStateObserver: settings.ResponseStateObserver,
		metricsObserver:       settings.MetricsObserver,
		traceObserver:         settings.TraceObserver,
		operationalAudits:     make(chan guardrails.AuditEvent, 100),
		asyncAuditJobs:        make(chan asyncAuditJob, 100),
	}
	operationalCtx, cancelOperational := context.WithCancel(context.Background())
	server.operationalStop = cancelOperational
	for range 4 {
		server.operationalWorkers.Add(2)
		go server.runOperationalAuditWorker(operationalCtx)
		go server.runAsyncAuditWorker(operationalCtx)
	}
	return server, nil
}

// Close stops bounded operational-audit workers. It is idempotent so tests and
// the application shutdown path may both call it safely.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.operationalStop()
		s.operationalWorkers.Wait()
	})
}

func (s *Server) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	ctx := stream.Context()
	if err := acquireStreamPermit(ctx, s.streamPermit); err != nil {
		return err
	}
	defer func() { <-s.streamPermit }()
	s.metricsObserver.IncActiveStreams()
	defer s.metricsObserver.DecActiveStreams()

	state := newStreamState()
	state.protocol.setStreamBufferLimit(s.maxStreamBufferBytes)
	defer state.protocol.close()
	defer s.enqueueCancellationAudit(ctx, state)
	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			if contextErr := grpcContextError(ctx, err); contextErr != nil {
				return contextErr
			}
			return err
		}
		if err := grpcContextError(ctx, nil); err != nil {
			return err
		}

		request, kind, err := requestFromEnvoy(message, state.protocol)
		if err != nil {
			s.metricsObserver.IncFailure("protocol")
			if errors.Is(err, ErrSSEBufferLimit) {
				s.metricsObserver.IncFailure("stream_buffer")
				return status.Error(codes.ResourceExhausted, "streaming response buffer limit exceeded")
			}
			return status.Error(codes.InvalidArgument, err.Error())
		}
		if kind == envoyRequestHeaders {
			s.metricsObserver.IncRequest()
		}
		if kind == envoyResponseHeaders {
			s.metricsObserver.IncResponse()
		}
		if len(request.Body) > 0 {
			s.metricsObserver.ObserveBodyBytes(string(request.Stage), len(request.Body))
		}
		if state.protocol.isResponseOnly() {
			result, terminal := s.responseOnlyResult(ctx, state, request)
			s.observeDecision(request, result, 0)
			response, err := responseToEnvoy(kind, request.Stage, result)
			if err != nil {
				s.metricsObserver.IncFailure("policy_resolution")
				return status.Error(codes.Internal, fmt.Sprintf("adapt response-only %s response: %v", kind, err))
			}
			if !message.GetObservabilityMode() {
				if err := stream.Send(response); err != nil {
					return err
				}
			}
			if terminal {
				return nil
			}
			continue
		}
		if kind == envoyRequestHeaders {
			resolved, err := s.policyResolver.ResolvePolicy(PolicyResolutionInput{
				Headers:    request.Headers,
				Gateway:    request.Gateway,
				Route:      request.Route,
				Attributes: request.Attributes,
			})
			if err != nil {
				// Attribute values are trusted Envoy route identifiers. Logging them
				// on a resolution failure makes native attachment mismatches
				// diagnosable without recording client headers or request content.
				log.Printf("native policy resolution failed: attributes=%v error=%v", request.Attributes, err)
				return status.Error(codes.InvalidArgument, err.Error())
			}
			request.PolicyID = resolved.PolicyID
			if resolved.Tenant != nil {
				request.Tenant = *resolved.Tenant
			}
			state.pinPolicy(request, s.policyCache, s.defaultFailureMode)
		}
		state.apply(&request)
		if kind == envoyResponseHeaders && state.hasPolicySnapshot {
			switch state.policySnapshot.Definition.Streaming.Mode {
			case policy.StreamingModeWindowed:
				state.protocol.enableWindowedResponse(state.policySnapshot.Definition.Streaming.WindowBytesOrDefault())
			case policy.StreamingModeAsyncAudit:
				state.protocol.enableAsyncAuditResponse()
			}
		}
		if immediateStatus, exceeded := bodyLimitStatus(kind, request.Headers, request.Body, s.maxBodyBytes); exceeded {
			s.metricsObserver.IncFailure("body_limit")
			result := ProcessingResult{Action: ActionBlock, ImmediateStatus: immediateStatus}
			enrichResultIdentity(&result, request, kind, 0)
			if err := s.auditDecision(ctx, request, result); err != nil {
				s.metricsObserver.IncFailure("audit")
			}
			s.observeDecision(request, result, 0)
			response, adaptErr := responseToEnvoy(kind, request.Stage, result)
			if adaptErr != nil {
				return status.Error(codes.Internal, adaptErr.Error())
			}
			if err := stream.Send(response); err != nil {
				return err
			}
			return nil
		}
		if kind == envoyResponseBody && state.protocol.isStreamingResponse() {
			if state.protocol.asyncAuditResponse {
				response, adaptErr := s.processAsyncAuditResponse(state, request)
				if adaptErr != nil {
					return status.Error(codes.Internal, adaptErr.Error())
				}
				if !message.GetObservabilityMode() {
					if err := stream.Send(response); err != nil {
						return err
					}
				}
				continue
			}
			response, terminal, adaptErr := s.processWindowedResponse(ctx, state, request)
			if adaptErr != nil {
				if contextErr := grpcContextError(ctx, adaptErr); contextErr != nil {
					return contextErr
				}
				if errors.Is(adaptErr, ErrSSEBufferLimit) {
					return status.Error(codes.ResourceExhausted, "streaming response buffer limit exceeded")
				}
				return status.Error(codes.Internal, adaptErr.Error())
			}
			if !message.GetObservabilityMode() {
				if err := stream.Send(response); err != nil {
					return err
				}
			}
			if terminal {
				return nil
			}
			continue
		}
		if kind == envoyRequestHeaders && (!state.hasPolicySnapshot || state.policySnapshot.Version <= 0) {
			result := s.failureResult(request)
			if result.Action != ActionBlock {
				// Open mode continues to the processor with an absent snapshot; the
				// result is marked degraded after processing below.
				goto processRequest
			}
			enrichResultIdentity(&result, request, kind, 0)
			s.metricsObserver.IncFailure("policy_resolution")
			s.observeDecision(request, result, 0)
			_ = s.auditDecision(ctx, request, result)
			response, adaptErr := responseToEnvoy(kind, request.Stage, result)
			if adaptErr != nil {
				return status.Error(codes.Internal, adaptErr.Error())
			}
			if err := stream.Send(response); err != nil {
				return err
			}
			return nil
		}
	processRequest:
		started := time.Now()
		processingCtx, cancel := context.WithTimeout(ctx, s.processingTimeout)
		tracedCtx, endTrace := s.traceObserver.StartGuardrail(processingCtx, request)
		result, err := s.processor.Process(tracedCtx, request)
		endTrace(result, err)
		processingErr := processingCtx.Err()
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return status.FromContextError(ctx.Err()).Err()
			}
			// A processor/validator deadline belongs to the BYG request budget,
			// so it is handled by the stream-pinned failure policy rather than
			// surfaced as an unclassified gRPC error.
			if errors.Is(processingErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
				s.metricsObserver.IncTimeout()
				s.metricsObserver.IncFailure("timeout")
			} else {
				s.metricsObserver.IncFailure("processor")
			}
			result = s.failureResult(request)
		}
		if !state.hasPolicySnapshot {
			result.Degraded = true
		}
		if err := grpcContextError(ctx, nil); err != nil {
			return err
		}
		enrichResultIdentity(&result, request, kind, time.Since(started))
		if err := s.auditDecision(ctx, request, result); err != nil {
			s.metricsObserver.IncFailure("audit")
			// Audit sink failure must not expose or alter content. The pinned
			// stage failure policy decides whether the already-made enforcement
			// decision can proceed in degraded mode.
			result.Degraded = true
			if auditFailureClosed(request) {
				result.Action = ActionBlock
				result.Body = nil
				result.HeaderMutations = nil
			}
			enrichResultIdentity(&result, request, kind, time.Since(started))
		}
		s.observeDecision(request, result, time.Since(started))
		response, err := responseToEnvoy(kind, request.Stage, result)
		if err != nil {
			return status.Error(codes.Internal, fmt.Sprintf("adapt %s response: %v", kind, err))
		}
		if message.GetObservabilityMode() {
			continue
		}
		if err := stream.Send(response); err != nil {
			if contextErr := grpcContextError(ctx, err); contextErr != nil {
				return contextErr
			}
			return err
		}
	}
}

func (s *Server) processAsyncAuditResponse(state *streamState, request ProcessingRequest) (*extprocv3.ProcessingResponse, error) {
	events, ready, dropped := state.protocol.takeAsyncAudit(request.EndOfStream)
	if dropped {
		s.metricsObserver.IncFailure("async_audit_buffer")
	}
	if ready && !dropped && len(events) > 0 {
		request.Body = nil
		select {
		case s.asyncAuditJobs <- asyncAuditJob{request: request, events: events}:
		default:
			s.metricsObserver.IncFailure("async_audit_queue")
		}
	}
	return responseToEnvoy(envoyResponseBody, StageResponse, ProcessingResult{Action: ActionAllow})
}

func (s *Server) processWindowedResponse(ctx context.Context, state *streamState, request ProcessingRequest) (*extprocv3.ProcessingResponse, bool, error) {
	if state.protocol.windowedResponse == nil {
		response, err := responseToEnvoy(envoyResponseBody, StageResponse, ProcessingResult{Action: ActionAllow})
		return response, false, err
	}
	events, emit, ready, windowErr := state.protocol.takeWindow(request.EndOfStream)
	if windowErr != nil {
		return nil, true, windowErr
	}
	if !ready {
		response, err := responseToEnvoy(envoyResponseBody, StageResponse, ProcessingResult{Action: ActionAllow, Body: []byte{}})
		return response, false, err
	}
	windowProcessor, ok := s.processor.(StreamingWindowProcessor)
	if !ok {
		result := s.failureResult(request)
		enrichResultIdentity(&result, request, envoyResponseBody, 0)
		s.metricsObserver.IncFailure("processor")
		if err := s.auditDecision(ctx, request, result); err != nil {
			s.metricsObserver.IncFailure("audit")
		}
		s.observeDecision(request, result, 0)
		if result.Action == ActionBlock {
			s.metricsObserver.IncStreamHalt()
		}
		response, err := responseToEnvoy(envoyResponseBody, StageResponse, result)
		return response, result.Action == ActionBlock, err
	}
	started := time.Now()
	processingCtx, cancel := context.WithTimeout(ctx, s.processingTimeout)
	tracedCtx, endTrace := s.traceObserver.StartGuardrail(processingCtx, request)
	result, mutated, err := windowProcessor.ProcessSSEWindow(tracedCtx, request, events)
	endTrace(result, err)
	processingErr := processingCtx.Err()
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil, true, status.FromContextError(ctx.Err()).Err()
		}
		if errors.Is(processingErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			s.metricsObserver.IncTimeout()
			s.metricsObserver.IncFailure("timeout")
		} else {
			s.metricsObserver.IncFailure("processor")
		}
		result = s.failureResult(request)
		// A fail-open response must pass through the original, event-aligned
		// window when its processor fails. Processors are permitted to return
		// no mutations alongside an error, so never slice a nil result here.
		mutated = events
	}
	enrichResultIdentity(&result, request, envoyResponseBody, 0)
	if err := s.auditDecision(ctx, request, result); err != nil {
		s.metricsObserver.IncFailure("audit")
		result.Degraded = true
		if auditFailureClosed(request) {
			result = s.failureResult(request)
		}
		enrichResultIdentity(&result, request, envoyResponseBody, time.Since(started))
	}
	s.observeDecision(request, result, time.Since(started))
	if result.Action == ActionBlock {
		s.metricsObserver.IncStreamHalt()
		response, adaptErr := responseToEnvoy(envoyResponseBody, StageResponse, result)
		return response, true, adaptErr
	}
	result.Body = encodeSSEEvents(mutated[:emit])
	response, adaptErr := responseToEnvoy(envoyResponseBody, StageResponse, result)
	return response, false, adaptErr
}

func (s *Server) observeDecision(request ProcessingRequest, result ProcessingResult, duration time.Duration) {
	s.metricsObserver.ObserveAction(result.Action, request.Stage, request.PolicyID)
	s.metricsObserver.ObserveDetections(result.Metadata.Categories, request.Stage)
	if duration > 0 {
		s.metricsObserver.ObserveDuration(request.Stage, duration.Seconds())
	}
}

func (s *Server) enqueueCancellationAudit(ctx context.Context, state *streamState) {
	if ctx.Err() == nil {
		return
	}
	reason := "stream_cancelled"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		reason = "stream_deadline_exceeded"
	}
	event := guardrails.AuditEvent{
		Timestamp: time.Now().UTC(), EventType: "operational", Reason: reason,
		RID: state.rid, RequestID: state.envoyReqID, Adapter: "envoy-gateway",
		PolicyID: state.policyID,
	}
	if state.hasPolicySnapshot {
		event.PolicyVersion = state.policySnapshot.Version
	}
	select {
	case s.operationalAudits <- event:
	default:
		// TODO(phase-5): expose dropped operational audit events as a metric.
		log.Printf("warn: dropped operational audit event type=%s reason=%s", event.EventType, event.Reason)
	}
}

func (s *Server) runOperationalAuditWorker(ctx context.Context) {
	defer s.operationalWorkers.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-s.operationalAudits:
			auditCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			err := s.auditor.Audit(auditCtx, event)
			cancel()
			if err != nil {
				log.Printf("operational audit delivery failed: %v", err)
			}
		}
	}
}

func (s *Server) runAsyncAuditWorker(ctx context.Context) {
	defer s.operationalWorkers.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-s.asyncAuditJobs:
			s.processAsyncAuditJob(ctx, job)
		}
	}
}

func (s *Server) processAsyncAuditJob(workerCtx context.Context, job asyncAuditJob) {
	processor, ok := s.processor.(StreamingWindowProcessor)
	if !ok {
		s.metricsObserver.IncFailure("async_audit_processor")
		return
	}
	started := time.Now()
	processingCtx, cancel := context.WithTimeout(workerCtx, s.processingTimeout)
	tracedCtx, endTrace := s.traceObserver.StartGuardrail(processingCtx, job.request)
	result, _, err := processor.ProcessSSEWindow(tracedCtx, job.request, job.events)
	endTrace(result, err)
	cancel()
	if err != nil {
		s.metricsObserver.IncFailure("async_audit_processor")
		result = ProcessingResult{Action: ActionAllow, Degraded: true}
	} else if result.Action != ActionAllow {
		// AsyncAudit is observational: findings are recorded, but an action can
		// never mutate or halt bytes that have already been delivered.
		result.Action = ActionAuditOnly
		result.Body = nil
		result.HeaderMutations = nil
	}
	enrichResultIdentity(&result, job.request, envoyResponseBody, time.Since(started))
	if err := s.auditDecision(workerCtx, job.request, result); err != nil {
		s.metricsObserver.IncFailure("audit")
	}
	s.observeDecision(job.request, result, time.Since(started))
}

// responseOnlyResult handles the exceptional case where Envoy invokes
// ext_proc only for response processing. No request-side policy snapshot is
// available, so this cannot use a policy-specific response failure mode or
// safely infer that the reply is gateway-owned. It therefore uses the global
// TSZ_FAIL_MODE fallback and records a degraded operational signal.
func (s *Server) responseOnlyResult(ctx context.Context, state *streamState, request ProcessingRequest) (ProcessingResult, bool) {
	if !state.responseOnlyHandled {
		state.responseOnlyHandled = true
		result := s.failureResult(ProcessingRequest{Stage: StageResponse, FailureMode: s.defaultFailureMode})
		state.responseOnlyAction = result.Action
		outcome := "fail_closed"
		if result.Action != ActionBlock {
			outcome = "fail_open"
		}
		s.responseStateObserver.ObserveResponseWithoutRequestState(outcome)
		if err := s.auditResponseWithoutRequestState(ctx, request, result.Action, true, "response_without_request_state"); err != nil {
			log.Printf("audit response without request state: %v", err)
		}
		return result, result.Action == ActionBlock
	}
	return ProcessingResult{Action: state.responseOnlyAction}, false
}

func (s *Server) auditResponseWithoutRequestState(ctx context.Context, request ProcessingRequest, action Action, degraded bool, reason string) error {
	auditAction := guardrails.RuleAction(action)
	if err := auditAction.Validate(); err != nil {
		return err
	}
	return s.auditor.Audit(ctx, guardrails.AuditEvent{
		Timestamp: time.Now().UTC(), EventType: "guardrail_decision", RID: NewBYGRID(), RequestID: request.EnvoyReqID,
		Adapter: "envoy-gateway", Target: guardrails.BuildAuditTarget("", "", ""),
		Stage: guardrails.AuditStageResponse, Action: auditAction, Reason: reason, Degraded: degraded,
	})
}

func auditFailureClosed(request ProcessingRequest) bool {
	return request.FailureMode != policy.FailureModeOpen
}

func acquireStreamPermit(ctx context.Context, permits chan struct{}) error {
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	select {
	case permits <- struct{}{}:
		return nil
	default:
		return status.Error(codes.ResourceExhausted, "maximum concurrent ext-proc streams reached")
	}
}

func grpcContextError(ctx context.Context, operationErr error) error {
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	if errors.Is(operationErr, context.Canceled) || errors.Is(operationErr, context.DeadlineExceeded) {
		return status.FromContextError(operationErr).Err()
	}
	return nil
}

func (state *streamState) pinPolicy(request ProcessingRequest, cache PolicyCache, defaultMode policy.FailureMode) {
	if state.policyPinned {
		return
	}
	state.policyPinned = true
	state.requestFailureMode = defaultMode
	state.responseFailureMode = defaultMode
	state.envoyReqID = request.EnvoyReqID
	state.traceID = request.TraceID
	state.traceParent = request.TraceParent
	state.policyID = strings.TrimSpace(request.PolicyID)
	if state.policyID == "" {
		return
	}
	snapshot, found := cache.Get(state.policyID)
	if !found {
		return
	}
	state.policySnapshot = snapshot.Clone()
	state.hasPolicySnapshot = true
	if mode := state.policySnapshot.Definition.FailurePolicy.Request; mode != "" {
		state.requestFailureMode = mode
	}
	if mode := state.policySnapshot.Definition.FailurePolicy.Response; mode != "" {
		state.responseFailureMode = mode
	}
}

func (s *Server) failureResult(request ProcessingRequest) ProcessingResult {
	if request.FailureMode == policy.FailureModeOpen {
		return ProcessingResult{Action: ActionAllow, Degraded: true}
	}
	result := ProcessingResult{Action: ActionBlock, Degraded: true}
	if request.Stage == StageResponse {
		result.ImmediateStatus = 403
	}
	return result
}

func exceedsDeclaredBodyLimit(headers map[string][]string, maximum int64) bool {
	value := FirstHeader(headers, "content-length")
	if value == "" {
		return false
	}
	length, err := strconv.ParseInt(value, 10, 64)
	return err == nil && length > maximum
}

// bodyLimitStatus treats buffered response bodies as a separate gateway
// failure from oversized client requests. The limits themselves are symmetric,
// while 413 remains reserved for client-supplied request content.
func bodyLimitStatus(kind envoyMessageKind, headers map[string][]string, body []byte, maximum int64) (int, bool) {
	switch kind {
	case envoyRequestHeaders:
		return 413, exceedsDeclaredBodyLimit(headers, maximum)
	case envoyRequestBody:
		return 413, int64(len(body)) > maximum
	case envoyResponseHeaders:
		return 502, exceedsDeclaredBodyLimit(headers, maximum)
	case envoyResponseBody:
		return 502, int64(len(body)) > maximum
	default:
		return 0, false
	}
}

func enrichResultIdentity(result *ProcessingResult, request ProcessingRequest, kind envoyMessageKind, latency time.Duration) {
	result.Metadata.RID = request.RID
	result.Metadata.RequestID = request.EnvoyReqID
	result.Metadata.PolicyID = request.PolicyID
	result.Metadata.PolicyVersion = request.PolicyVersion
	result.Metadata.Adapter = "envoy-gateway"
	result.Metadata.Stage = request.Stage
	result.Metadata.Action = result.Action
	result.Metadata.ProcessorLatencyMS = latency.Milliseconds()
	result.Metadata.Degraded = result.Degraded
	// Request-header mutations are sent before the buffered body is forwarded.
	// They communicate both distinct IDs without mutating Envoy's request ID.
	if kind != envoyRequestHeaders || request.Stage != StageRequest || result.Action == ActionBlock {
		return
	}
	if result.HeaderMutations == nil {
		result.HeaderMutations = make(map[string]string, 2)
	}
	result.HeaderMutations["x-tsz-rid"] = request.RID
	result.HeaderMutations["x-tsz-envoy-request-id"] = request.EnvoyReqID
}

func (s *Server) auditDecision(ctx context.Context, request ProcessingRequest, result ProcessingResult) error {
	action := guardrails.RuleAction(result.Action)
	if err := action.Validate(); err != nil {
		return err
	}
	stage := guardrails.AuditStageRequest
	if request.Stage == StageResponse {
		stage = guardrails.AuditStageResponse
	}
	return s.auditor.Audit(ctx, guardrails.AuditEvent{
		Timestamp: time.Now().UTC(), EventType: "guardrail_decision", RID: request.RID, RequestID: request.EnvoyReqID, TraceID: request.TraceID,
		Adapter: "envoy-gateway", Target: guardrails.BuildAuditTarget(request.Gateway, request.Tenant, request.Route),
		Gateway: request.Gateway, Route: request.Route, Tenant: request.Tenant,
		PolicyID: request.PolicyID, PolicyVersion: request.PolicyVersion, Stage: stage,
		Action: action, Categories: append([]string(nil), result.Metadata.Categories...),
		DetectionCount: result.DetectionCount, ProcessorLatencyMS: result.Metadata.ProcessorLatencyMS,
		Degraded: result.Degraded,
	})
}

func (state *streamState) apply(request *ProcessingRequest) {
	request.RID = state.rid
	request.EnvoyReqID = state.envoyReqID
	request.TraceID = state.traceID
	request.TraceParent = state.traceParent
	request.PolicyID = state.policyID
	request.FailureMode = state.failureModeFor(request.Stage)
	if !state.hasPolicySnapshot {
		return
	}
	snapshot := state.policySnapshot.Clone()
	request.PolicyVersion = snapshot.Version
	request.PolicySnapshot = &snapshot
}

func (state *streamState) failureModeFor(stage ProcessingStage) policy.FailureMode {
	if stage == StageResponse {
		return state.responseFailureMode
	}
	return state.requestFailureMode
}
