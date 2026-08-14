package envoy

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
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
		return contractRequest(StageRequest, headers, nil, attributes), envoyRequestHeaders, nil
	case *extprocv3.ProcessingRequest_ResponseHeaders:
		if !state.requestHeadersSeen || !state.requestEnded || state.responseHeadersSeen {
			return ProcessingRequest{}, "", errors.New("response headers require a completed request and may appear only once")
		}
		headers := headersFromEnvoy(typed.ResponseHeaders.GetHeaders())
		state.response.headers = CloneHeaders(headers)
		state.responseHeadersSeen = true
		state.responseEnded = typed.ResponseHeaders.GetEndOfStream()
		return contractRequest(StageResponse, headers, nil, attributes), envoyResponseHeaders, nil
	case *extprocv3.ProcessingRequest_RequestBody:
		if !state.requestHeadersSeen || state.requestEnded || state.requestBodySeen {
			return ProcessingRequest{}, "", errors.New("request body requires request headers and may appear only once before end of stream")
		}
		state.requestBodySeen = true
		state.requestEnded = typed.RequestBody.GetEndOfStream()
		return contractRequest(StageRequest, state.request.headers, typed.RequestBody.GetBody(), attributes), envoyRequestBody, nil
	case *extprocv3.ProcessingRequest_ResponseBody:
		if !state.responseHeadersSeen || state.responseEnded || state.responseBodySeen {
			return ProcessingRequest{}, "", errors.New("response body requires response headers and may appear only once before end of stream")
		}
		state.responseBodySeen = true
		state.responseEnded = typed.ResponseBody.GetEndOfStream()
		return contractRequest(StageResponse, state.response.headers, typed.ResponseBody.GetBody(), attributes), envoyResponseBody, nil
	case *extprocv3.ProcessingRequest_RequestTrailers, *extprocv3.ProcessingRequest_ResponseTrailers:
		return ProcessingRequest{}, "", errors.New("trailer processing is not enabled in Phase 1")
	default:
		return ProcessingRequest{}, "", errors.New("unsupported or empty Envoy processing request")
	}
}

func contractRequest(stage ProcessingStage, headers map[string][]string, body []byte, attributes map[string]string) ProcessingRequest {
	requestID := FirstHeader(headers, "x-request-id")
	return ProcessingRequest{
		EnvoyReqID: requestID, Stage: stage,
		Headers: CloneHeaders(headers), Body: append([]byte(nil), body...),
		ContentType: FirstHeader(headers, "content-type"),
		Gateway:     FirstHeader(headers, "x-tsz-gateway"), Route: FirstHeader(headers, "x-tsz-route"),
		Tenant:     FirstHeader(headers, "x-tsz-tenant"),
		Attributes: attributes,
	}
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
		if stage != StageRequest {
			return nil, errors.New("Phase 1 does not support blocking response messages")
		}
		body, err := safeBlockResponseBody(result.Metadata, result.ImmediateStatus)
		if err != nil {
			return nil, fmt.Errorf("serialize safe block response: %w", err)
		}
		statusCode := typev3.StatusCode_BadRequest
		if result.ImmediateStatus == 413 {
			statusCode = typev3.StatusCode_PayloadTooLarge
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

func safeBlockResponseBody(metadata SafeMetadata, immediateStatus int) ([]byte, error) {
	code, message := blockErrorCode, blockErrorMessage
	if immediateStatus == 413 {
		code, message = "TSZ_REQUEST_BODY_TOO_LARGE", "Request body exceeds configured limit."
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
		metadata.DetectionCount == 0 && metadata.ProcessorLatencyMS == 0 {
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
