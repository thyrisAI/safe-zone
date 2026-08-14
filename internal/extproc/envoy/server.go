package envoy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
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
	rid               string
	envoyReqID        string
	policyID          string
	policySnapshot    policy.CompiledSnapshot
	hasPolicySnapshot bool
	policyPinned      bool
	failureMode       policy.FailureMode
	protocol          *envoyStreamState
}

func newStreamState() *streamState {
	return &streamState{rid: NewBYGRID(), protocol: newEnvoyStreamState()}
}

// Server implements Envoy's bidirectional ext-proc stream and delegates all
// decisions to the injected gateway-neutral Processor.
type Server struct {
	extprocv3.UnimplementedExternalProcessorServer
	processor          Processor
	policyCache        PolicyCache
	policyResolver     PolicyResolver
	auditor            guardrails.Auditor
	streamPermit       chan struct{}
	defaultFailureMode policy.FailureMode
	maxBodyBytes       int64
	processingTimeout  time.Duration
}

// Register binds this Envoy-specific adapter to a generic gRPC server.
func (s *Server) Register(server *grpc.Server) {
	extprocv3.RegisterExternalProcessorServer(server, s)
}

type ServerSettings struct {
	FailMode          policy.FailureMode
	MaxBodyBytes      int64
	ProcessingTimeout time.Duration
}

func defaultServerSettings() ServerSettings {
	return ServerSettings{FailMode: policy.FailureModeClosed, MaxBodyBytes: 1024 * 1024, ProcessingTimeout: 2 * time.Second}
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
	if settings.ProcessingTimeout <= 0 {
		settings.ProcessingTimeout = defaults.ProcessingTimeout
	}
	return &Server{
		processor:          processor,
		policyCache:        policyCache,
		policyResolver:     resolver,
		auditor:            auditor,
		streamPermit:       make(chan struct{}, limit),
		defaultFailureMode: settings.FailMode, maxBodyBytes: settings.MaxBodyBytes, processingTimeout: settings.ProcessingTimeout,
	}, nil
}

func (s *Server) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	ctx := stream.Context()
	if err := acquireStreamPermit(ctx, s.streamPermit); err != nil {
		return err
	}
	defer func() { <-s.streamPermit }()

	state := newStreamState()
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
			return status.Error(codes.InvalidArgument, err.Error())
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
		if kind == envoyRequestHeaders && exceedsDeclaredBodyLimit(request.Headers, s.maxBodyBytes) {
			result := ProcessingResult{Action: ActionBlock, ImmediateStatus: 413}
			enrichResultIdentity(&result, request, kind, 0)
			response, adaptErr := responseToEnvoy(kind, request.Stage, result)
			if adaptErr != nil {
				return status.Error(codes.Internal, adaptErr.Error())
			}
			if err := stream.Send(response); err != nil {
				return err
			}
			return nil
		}
		if kind == envoyRequestBody && int64(len(request.Body)) > s.maxBodyBytes {
			result := ProcessingResult{Action: ActionBlock, ImmediateStatus: 413}
			enrichResultIdentity(&result, request, kind, 0)
			response, adaptErr := responseToEnvoy(kind, request.Stage, result)
			if adaptErr != nil {
				return status.Error(codes.Internal, adaptErr.Error())
			}
			if err := stream.Send(response); err != nil {
				return err
			}
			return nil
		}
		if kind == envoyRequestHeaders && (!state.hasPolicySnapshot || state.policySnapshot.Version <= 0) {
			result := s.failureResult(request)
			if result.Action != ActionBlock {
				// Open mode continues to the processor with an absent snapshot; the
				// result is marked degraded after processing below.
				goto processRequest
			}
			enrichResultIdentity(&result, request, kind, 0)
			_ = s.auditRequest(ctx, request, result)
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
		result, err := s.processor.Process(processingCtx, request)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return status.FromContextError(ctx.Err()).Err()
			}
			// A processor/validator deadline belongs to the BYG request budget,
			// so it is handled by the stream-pinned failure policy rather than
			// surfaced as an unclassified gRPC error.
			result = s.failureResult(request)
		}
		if !state.hasPolicySnapshot {
			result.Degraded = true
		}
		if err := grpcContextError(ctx, nil); err != nil {
			return err
		}
		enrichResultIdentity(&result, request, kind, time.Since(started))
		if err := s.auditRequest(ctx, request, result); err != nil {
			// Audit sink failure must not expose or alter request content. The
			// request failure policy decides whether the already-made enforcement
			// decision can proceed in degraded mode.
			result.Degraded = true
			if auditFailureClosed(request) {
				result.Action = ActionBlock
				result.Body = nil
				result.HeaderMutations = nil
				result.Metadata.Action = ActionBlock
			}
		}
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
	state.failureMode = defaultMode
	state.envoyReqID = request.EnvoyReqID
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
		state.failureMode = mode
	}
}

func (s *Server) failureResult(request ProcessingRequest) ProcessingResult {
	if request.FailureMode == policy.FailureModeOpen {
		return ProcessingResult{Action: ActionAllow, Degraded: true}
	}
	return ProcessingResult{Action: ActionBlock, Degraded: true}
}

func exceedsDeclaredBodyLimit(headers map[string][]string, maximum int64) bool {
	value := FirstHeader(headers, "content-length")
	if value == "" {
		return false
	}
	length, err := strconv.ParseInt(value, 10, 64)
	return err == nil && length > maximum
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

func (s *Server) auditRequest(ctx context.Context, request ProcessingRequest, result ProcessingResult) error {
	if request.Stage != StageRequest {
		return nil
	}
	action := guardrails.RuleAction(result.Action)
	if err := action.Validate(); err != nil {
		return err
	}
	return s.auditor.Audit(ctx, guardrails.AuditEvent{
		Timestamp: time.Now().UTC(), RID: request.RID, RequestID: request.EnvoyReqID, TraceID: request.TraceID,
		Adapter: "envoy-gateway", Target: guardrails.BuildAuditTarget(request.Gateway, request.Tenant, request.Route),
		Gateway: request.Gateway, Route: request.Route, Tenant: request.Tenant,
		PolicyID: request.PolicyID, PolicyVersion: request.PolicyVersion, Stage: guardrails.AuditStageRequest,
		Action: action, Categories: append([]string(nil), result.Metadata.Categories...),
		DetectionCount: result.DetectionCount, ProcessorLatencyMS: result.Metadata.ProcessorLatencyMS,
		Degraded: result.Degraded,
	})
}

func (state *streamState) apply(request *ProcessingRequest) {
	request.RID = state.rid
	request.EnvoyReqID = state.envoyReqID
	request.PolicyID = state.policyID
	request.FailureMode = state.failureMode
	if !state.hasPolicySnapshot {
		return
	}
	snapshot := state.policySnapshot.Clone()
	request.PolicyVersion = snapshot.Version
	request.PolicySnapshot = &snapshot
}
