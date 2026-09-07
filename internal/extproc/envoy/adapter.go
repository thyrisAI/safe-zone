package envoy

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"sort"
	"strings"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/structpb"
	. "thyris-sz/internal/extproc"
)

type envoyMessageKind string

const safeMetadataNamespace = "io.thyris.tsz"

const (
	blockErrorCode    = "TSZ_GUARDRAIL_BLOCKED"
	blockErrorMessage = "Request blocked by guardrail policy."
)

// blockErrorResponse is deliberately small and stable. It must never contain
// a finding, matched value, prompt, or validator output.
type blockErrorResponse struct {
	Error   blockError     `json:"error"`
	TSZMeta blockErrorMeta `json:"tsz_meta"`
}

type blockError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type blockErrorMeta struct {
	RID            string `json:"rid"`
	EnvoyRequestID string `json:"envoy_request_id"`
	PolicyID       string `json:"policy_id"`
	PolicyVersion  int    `json:"policy_version"`
}

const (
	envoyRequestHeaders  envoyMessageKind = "request_headers"
	envoyResponseHeaders envoyMessageKind = "response_headers"
	envoyRequestBody     envoyMessageKind = "request_body"
	envoyResponseBody    envoyMessageKind = "response_body"
)

type envoyStageState struct {
	headers map[string][]string
}

type envoyStreamState struct {
	request             envoyStageState
	response            envoyStageState
	requestHeadersSeen  bool
	requestBodySeen     bool
	requestEnded        bool
	responseHeadersSeen bool
	responseBodySeen    bool
	responseEnded       bool
	responseOnly        bool
	responseStreaming   bool
	responseSSE         *OpenAISSEParser
	windowedResponse    *sseWindow
	completedSSEEvents  []OpenAISSEEvent
	streamBufferLimit   int
	rpcMethod           string
}

func (state *envoyStreamState) setStreamBufferLimit(limit int) {
	if limit > 0 {
		state.streamBufferLimit = limit
	}
}

func newEnvoyStreamState() *envoyStreamState {
	return &envoyStreamState{
		request:  envoyStageState{headers: map[string][]string{}},
		response: envoyStageState{headers: map[string][]string{}},
	}
}

func requestFromEnvoy(message *extprocv3.ProcessingRequest, state *envoyStreamState) (ProcessingRequest, envoyMessageKind, error) {
	if message == nil || state == nil {
		return ProcessingRequest{}, "", errors.New("Envoy processing request and stream state are required")
	}
	attributes := attributesFromEnvoy(message.GetAttributes())
	switch typed := message.GetRequest().(type) {
	case *extprocv3.ProcessingRequest_RequestHeaders:
		if state.requestHeadersSeen || state.responseHeadersSeen {
			return ProcessingRequest{}, "", errors.New("request headers must be the first ext-proc message")
		}
		if hasUnexpectedPolicyMetadata(message) {
			return ProcessingRequest{}, "", errors.New("policy identity must be supplied by trusted X-TSZ-Policy route header")
		}
		headers := headersFromEnvoy(typed.RequestHeaders.GetHeaders())
		state.request.headers = CloneHeaders(headers)
		state.requestHeadersSeen = true
		state.requestEnded = typed.RequestHeaders.GetEndOfStream()
		request := contractRequest(StageRequest, headers, nil, attributes)
		request.EndOfStream = typed.RequestHeaders.GetEndOfStream()
		return request, envoyRequestHeaders, nil
	case *extprocv3.ProcessingRequest_ResponseHeaders:
		if state.responseHeadersSeen || (state.requestHeadersSeen && !state.requestEnded) {
			return ProcessingRequest{}, "", errors.New("response headers require a completed request and may appear only once")
		}
		// A filter that runs before ext_proc can reject a request and produce an
		// Envoy local reply. In that case Envoy may invoke ext_proc only for the
		// response path, so there is intentionally no request-side state to pin.
		state.responseOnly = !state.requestHeadersSeen
		headers := headersFromEnvoy(typed.ResponseHeaders.GetHeaders())
		state.response.headers = CloneHeaders(headers)
		state.responseHeadersSeen = true
		state.responseEnded = typed.ResponseHeaders.GetEndOfStream()
		state.responseStreaming = isSSEContentType(FirstHeader(headers, "content-type"))
		if state.responseStreaming {
			state.responseSSE = NewOpenAISSEParser(state.streamBufferLimit)
		}
		request := contractRequest(StageResponse, headers, nil, attributes)
		request.RequestPath = FirstHeader(state.request.headers, ":path")
		request.RPCMethod = state.rpcMethod
		request.EndOfStream = typed.ResponseHeaders.GetEndOfStream()
		return request, envoyResponseHeaders, nil
	case *extprocv3.ProcessingRequest_RequestBody:
		if !state.requestHeadersSeen || state.requestEnded || state.requestBodySeen {
			return ProcessingRequest{}, "", errors.New("request body requires request headers and may appear only once before end of stream")
		}
		state.requestBodySeen = true
		state.requestEnded = typed.RequestBody.GetEndOfStream()
		request := contractRequest(StageRequest, state.request.headers, typed.RequestBody.GetBody(), attributes)
		state.rpcMethod = request.RPCMethod
		request.EndOfStream = typed.RequestBody.GetEndOfStream()
		return request, envoyRequestBody, nil
	case *extprocv3.ProcessingRequest_ResponseBody:
		if !state.responseHeadersSeen || state.responseEnded || (state.responseBodySeen && !state.responseStreaming) {
			return ProcessingRequest{}, "", errors.New("response body requires response headers and may appear once for buffered bodies or repeatedly for streaming bodies before end of stream")
		}
		state.responseBodySeen = true
		state.responseEnded = typed.ResponseBody.GetEndOfStream()
		if state.responseStreaming {
			events, err := state.responseSSE.Feed(typed.ResponseBody.GetBody())
			if err != nil {
				return ProcessingRequest{}, "", fmt.Errorf("parse OpenAI SSE response: %w", err)
			}
			state.completedSSEEvents = events
			if state.responseEnded {
				if err := state.responseSSE.Finish(); err != nil {
					return ProcessingRequest{}, "", fmt.Errorf("finish OpenAI SSE response: %w", err)
				}
			}
		}
		request := contractRequest(StageResponse, state.response.headers, typed.ResponseBody.GetBody(), attributes)
		request.RequestPath = FirstHeader(state.request.headers, ":path")
		request.RPCMethod = state.rpcMethod
		request.EndOfStream = typed.ResponseBody.GetEndOfStream()
		return request, envoyResponseBody, nil
	case *extprocv3.ProcessingRequest_RequestTrailers, *extprocv3.ProcessingRequest_ResponseTrailers:
		return ProcessingRequest{}, "", errors.New("trailer processing is not enabled in Phase 1")
	default:
		return ProcessingRequest{}, "", errors.New("unsupported or empty Envoy processing request")
	}
}

func (state *envoyStreamState) enableWindowedResponse(windowBytes int) {
	if state.responseStreaming && state.windowedResponse == nil {
		state.windowedResponse = newSSEWindow(windowBytes, 512, state.streamBufferLimit)
	}
}

func (state *envoyStreamState) takeWindow(endOfStream bool) ([]OpenAISSEEvent, int, bool, error) {
	if state.windowedResponse == nil {
		return nil, 0, false, nil
	}
	if err := state.windowedResponse.add(state.completedSSEEvents); err != nil {
		return nil, 0, false, err
	}
	state.completedSSEEvents = nil
	events, emit, ready := state.windowedResponse.next(endOfStream)
	return events, emit, ready, nil
}

// close releases per-stream parser and window references on every Process
// exit path, including cancellation and malformed protocol sequences.
func (state *envoyStreamState) close() {
	if state.windowedResponse != nil {
		state.windowedResponse.events = nil
		state.windowedResponse.bytes = 0
	}
	state.completedSSEEvents = nil
	state.responseSSE = nil
}

type sseWindow struct {
	events                           []OpenAISSEEvent
	bytes, target, overlap, maxBytes int
}

func newSSEWindow(target, overlap, maxBytes int) *sseWindow {
	if target <= 0 {
		target = 4096
	}
	return &sseWindow{target: target, overlap: overlap, maxBytes: maxBytes}
}
func (w *sseWindow) add(events []OpenAISSEEvent) error {
	for _, event := range events {
		if w.maxBytes > 0 && len(event.Raw) > w.maxBytes-w.bytes {
			return ErrSSEBufferLimit
		}
		w.events = append(w.events, event)
		w.bytes += len(event.Raw)
	}
	return nil
}
func (w *sseWindow) next(end bool) ([]OpenAISSEEvent, int, bool) {
	if end {
		events := w.events
		w.events = nil
		w.bytes = 0
		return events, len(events), len(events) > 0
	}
	if w.bytes < w.target+w.overlap {
		return nil, 0, false
	}
	keep, keptBytes := 0, 0
	for index := len(w.events) - 1; index >= 0 && keptBytes < w.overlap; index-- {
		keep++
		keptBytes += len(w.events[index].Raw)
	}
	emit := len(w.events) - keep
	if emit == 0 && len(w.events) == 1 {
		emit, keep, keptBytes = 1, 0, 0
	}
	if emit == 0 {
		return nil, 0, false
	}
	events := append([]OpenAISSEEvent(nil), w.events...)
	w.events = append([]OpenAISSEEvent(nil), w.events[emit:]...)
	w.bytes = keptBytes
	return events, emit, true
}

func encodeSSEEvents(events []OpenAISSEEvent) []byte {
	var body []byte
	for _, event := range events {
		body = append(body, event.Raw...)
	}
	return body
}

func (state *envoyStreamState) isResponseOnly() bool {
	return state.responseOnly
}

func (state *envoyStreamState) isStreamingResponse() bool {
	return state.responseStreaming
}

func isSSEContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "text/event-stream")
}

func contractRequest(stage ProcessingStage, headers map[string][]string, body []byte, attributes map[string]string) ProcessingRequest {
	requestID := FirstHeader(headers, "x-request-id")
	traceParent, traceID := trustedTraceContext(attributes)
	var bodyCopy []byte
	if body != nil {
		bodyCopy = make([]byte, len(body))
		copy(bodyCopy, body)
	}
	request := ProcessingRequest{
		RequestPath: FirstHeader(headers, ":path"),
		EnvoyReqID:  requestID, TraceID: traceID, TraceParent: traceParent, Stage: stage,
		Headers: CloneHeaders(headers), Body: bodyCopy,
		ContentType: FirstHeader(headers, "content-type"),
		Gateway:     FirstHeader(headers, "x-tsz-gateway"), Route: FirstHeader(headers, "x-tsz-route"),
		Tenant:     FirstHeader(headers, "x-tsz-tenant"),
		Attributes: attributes,
	}
	if stage == StageRequest {
		request.RPCMethod = MCPMethodFromMessage(request.ContentType, body)
	}
	return request
}

func trustedTraceContext(attributes map[string]string) (string, string) {
	traceParent := strings.TrimSpace(attributes["traceparent"])
	parts := strings.Split(traceParent, "-")
	if len(parts) != 4 || len(parts[1]) != 32 {
		return "", ""
	}
	traceID, err := trace.TraceIDFromHex(parts[1])
	if err != nil || !traceID.IsValid() {
		return "", ""
	}
	return traceParent, traceID.String()
}

// Policy identity must be resolved from the trusted route header. Legacy
// filter metadata is rejected so policy sources cannot be silently mixed.
func hasUnexpectedPolicyMetadata(message *extprocv3.ProcessingRequest) bool {
	metadata := message.GetMetadataContext()
	if metadata == nil {
		return false
	}
	namespace := metadata.GetFilterMetadata()[safeMetadataNamespace]
	if namespace == nil {
		return false
	}
	return strings.TrimSpace(namespace.GetFields()["policy_id"].GetStringValue()) != ""
}

func responseToEnvoy(kind envoyMessageKind, stage ProcessingStage, result ProcessingResult) (*extprocv3.ProcessingResponse, error) {
	if err := stage.Validate(); err != nil {
		return nil, err
	}
	if err := result.Action.Validate(); err != nil {
		return nil, err
	}
	dynamicMetadata, err := metadataToEnvoy(result.Metadata)
	if err != nil {
		return nil, fmt.Errorf("convert safe metadata: %w", err)
	}
	if result.Action == ActionBlock {
		body, err := safeBlockResponseBody(result.Metadata, result.ImmediateStatus, stage)
		if err != nil {
			return nil, fmt.Errorf("serialize safe block response: %w", err)
		}
		statusCode := typev3.StatusCode_BadRequest
		switch result.ImmediateStatus {
		case 403:
			statusCode = typev3.StatusCode_Forbidden
		case 413:
			statusCode = typev3.StatusCode_PayloadTooLarge
		case 502:
			statusCode = typev3.StatusCode_BadGateway
		}
		return &extprocv3.ProcessingResponse{
			Response: &extprocv3.ProcessingResponse_ImmediateResponse{ImmediateResponse: &extprocv3.ImmediateResponse{
				Status: &typev3.HttpStatus{Code: statusCode}, Body: body,
				Headers: headerMutationToEnvoy(map[string]string{
					"content-type":   "application/json",
					"content-length": fmt.Sprintf("%d", len(body)),
				}),
				Details: "tsz_guardrail_blocked",
			}},
			DynamicMetadata: dynamicMetadata,
		}, nil
	}

	common := &extprocv3.CommonResponse{Status: extprocv3.CommonResponse_CONTINUE}
	common.HeaderMutation = headerMutationToEnvoy(result.HeaderMutations)
	if result.Body != nil {
		common.BodyMutation = &extprocv3.BodyMutation{Mutation: &extprocv3.BodyMutation_Body{Body: append([]byte(nil), result.Body...)}}
	}
	response := &extprocv3.ProcessingResponse{DynamicMetadata: dynamicMetadata}
	switch kind {
	case envoyRequestHeaders:
		response.Response = &extprocv3.ProcessingResponse_RequestHeaders{RequestHeaders: &extprocv3.HeadersResponse{Response: common}}
	case envoyResponseHeaders:
		response.Response = &extprocv3.ProcessingResponse_ResponseHeaders{ResponseHeaders: &extprocv3.HeadersResponse{Response: common}}
	case envoyRequestBody:
		response.Response = &extprocv3.ProcessingResponse_RequestBody{RequestBody: &extprocv3.BodyResponse{Response: common}}
	case envoyResponseBody:
		response.Response = &extprocv3.ProcessingResponse_ResponseBody{ResponseBody: &extprocv3.BodyResponse{Response: common}}
	default:
		return nil, fmt.Errorf("unsupported Envoy message kind %q", kind)
	}
	return response, nil
}

func safeBlockResponseBody(metadata SafeMetadata, immediateStatus int, stage ProcessingStage) ([]byte, error) {
	code, message := blockErrorCode, blockErrorMessage
	if immediateStatus == 413 {
		code, message = "TSZ_REQUEST_BODY_TOO_LARGE", "Request body exceeds configured limit."
	} else if immediateStatus == 502 {
		code, message = "TSZ_RESPONSE_BODY_TOO_LARGE", "Response body exceeds configured limit."
	} else if stage == StageResponse {
		code, message = "TSZ_RESPONSE_GUARDRAIL_BLOCKED", "Response blocked by guardrail policy."
	}
	return json.Marshal(blockErrorResponse{
		Error: blockError{Code: code, Message: message},
		TSZMeta: blockErrorMeta{
			RID: metadata.RID, EnvoyRequestID: metadata.RequestID,
			PolicyID: metadata.PolicyID, PolicyVersion: metadata.PolicyVersion,
		},
	})
}

func headersFromEnvoy(headerMap *corev3.HeaderMap) map[string][]string {
	headers := make(map[string][]string)
	if headerMap == nil {
		return headers
	}
	for _, header := range headerMap.GetHeaders() {
		if header == nil || header.GetKey() == "" {
			continue
		}
		value := header.GetValue()
		if raw := header.GetRawValue(); len(raw) > 0 {
			value = string(raw)
		}
		key := strings.ToLower(header.GetKey())
		headers[key] = append(headers[key], value)
	}
	return headers
}

func headerMutationToEnvoy(mutations map[string]string) *extprocv3.HeaderMutation {
	if len(mutations) == 0 {
		return nil
	}
	keys := make([]string, 0, len(mutations))
	for key := range mutations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := &extprocv3.HeaderMutation{SetHeaders: make([]*corev3.HeaderValueOption, 0, len(keys))}
	for _, key := range keys {
		result.SetHeaders = append(result.SetHeaders, &corev3.HeaderValueOption{
			Header:       &corev3.HeaderValue{Key: strings.ToLower(key), RawValue: []byte(mutations[key])},
			AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		})
	}
	return result
}

func metadataToEnvoy(metadata SafeMetadata) (*structpb.Struct, error) {
	if metadata.RequestID == "" && metadata.RID == "" && metadata.PolicyID == "" && metadata.PolicyVersion == 0 &&
		metadata.Adapter == "" && metadata.Stage == "" && metadata.Action == "" && len(metadata.Categories) == 0 &&
		metadata.DetectionCount == 0 && metadata.ProcessorLatencyMS == 0 && !metadata.Degraded {
		return nil, nil
	}
	categories := make([]any, len(metadata.Categories))
	for index, category := range metadata.Categories {
		categories[index] = category
	}
	value, err := structpb.NewStruct(map[string]any{
		safeMetadataNamespace: map[string]any{
			"request_id": metadata.RequestID, "rid": metadata.RID,
			"policy_id": metadata.PolicyID, "policy_version": metadata.PolicyVersion,
			"adapter": metadata.Adapter, "stage": string(metadata.Stage), "action": string(metadata.Action),
			"categories": categories, "detection_count": metadata.DetectionCount,
			"processor_latency_ms": metadata.ProcessorLatencyMS,
			"degraded":             metadata.Degraded,
		},
	})
	if err != nil {
		return nil, err
	}
	return value, nil
}

func attributesFromEnvoy(attributes map[string]*structpb.Struct) map[string]string {
	result := make(map[string]string, len(attributes))
	for key, value := range attributes {
		if value == nil {
			continue
		}
		// Envoy wraps configured attributes under the ext_proc filter name,
		// e.g. envoy.filters.http.ext_proc: {"xds.route_name": "..."}.
		// Flatten that trusted envelope so resolvers receive the configured
		// attribute names, rather than the transport wrapper name.
		if flattenAttributeValues(result, value.AsMap()) {
			continue
		}
		if stringValue, found := attributeString(value.AsMap()); found {
			result[key] = stringValue
			continue
		}
		encoded, err := json.Marshal(value.AsMap())
		if err == nil {
			result[key] = string(encoded)
		}
	}
	return result
}

func flattenAttributeValues(result map[string]string, values map[string]any) bool {
	found := false
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			if typed != "" {
				result[key] = typed
				found = true
			}
		case map[string]any:
			if flattenAttributeValues(result, typed) {
				found = true
			}
		}
	}
	return found
}

func attributeString(value map[string]any) (string, bool) {
	for _, key := range []string{"value", "name", "route_name", "listener_name", "gateway_name", "cluster_name"} {
		if text, ok := value[key].(string); ok && text != "" {
			return text, true
		}
	}
	for _, nested := range value {
		if child, ok := nested.(map[string]any); ok {
			if text, found := attributeString(child); found {
				return text, true
			}
		}
	}
	return "", false
}
