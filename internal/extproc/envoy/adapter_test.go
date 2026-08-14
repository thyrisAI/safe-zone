package envoy

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/protobuf/types/known/structpb"
	. "thyris-sz/internal/extproc"
)

func TestRequestFromEnvoyCarriesHeadersIntoBufferedBody(t *testing.T) {
	state := newEnvoyStreamState()
	headerMessage := &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestHeaders{RequestHeaders: &extprocv3.HttpHeaders{
		Headers: testHeaderMap(
			[2]string{"x-request-id", "envoy-1"},
			[2]string{"x-tsz-rid", "rid-1"},
			[2]string{"content-type", "application/json"},
			[2]string{"x-repeat", "one"},
			[2]string{"x-repeat", "two"},
		),
	}}}
	request, kind, err := requestFromEnvoy(headerMessage, state)
	if err != nil {
		t.Fatalf("requestFromEnvoy(headers) error = %v", err)
	}
	if kind != envoyRequestHeaders || request.Stage != StageRequest || request.RID != "" || request.EnvoyReqID != "envoy-1" {
		t.Fatalf("header request = %+v kind=%s", request, kind)
	}
	if len(request.Headers["x-repeat"]) != 2 || request.ContentType != "application/json" {
		t.Fatalf("headers = %+v content-type=%q", request.Headers, request.ContentType)
	}

	bodyMessage := &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestBody{RequestBody: &extprocv3.HttpBody{Body: []byte(`{"messages":[]}`), EndOfStream: true}}}
	bodyRequest, kind, err := requestFromEnvoy(bodyMessage, state)
	if err != nil {
		t.Fatalf("requestFromEnvoy(body) error = %v", err)
	}
	if kind != envoyRequestBody || bodyRequest.RID != "" || string(bodyRequest.Body) != `{"messages":[]}` || bodyRequest.ContentType != "application/json" {
		t.Fatalf("body request = %+v kind=%s", bodyRequest, kind)
	}
}

func TestRequestFromEnvoyFlattensExtProcAttributeEnvelope(t *testing.T) {
	message := requestHeadersForAdapterTest(true)
	message.Attributes = map[string]*structpb.Struct{
		"envoy.filters.http.ext_proc": {
			Fields: map[string]*structpb.Value{
				"xds.route_name": structpb.NewStringValue("httproute/demo/orders/rule/0/match/0/*"),
			},
		},
	}
	request, _, err := requestFromEnvoy(message, newEnvoyStreamState())
	if err != nil {
		t.Fatalf("requestFromEnvoy() error = %v", err)
	}
	if got, want := request.Attributes["xds.route_name"], "httproute/demo/orders/rule/0/match/0/*"; got != want {
		t.Fatalf("route attribute = %q, want %q (attributes=%v)", got, want, request.Attributes)
	}
}

func TestRequestFromEnvoyHandlesResponseStagesEmptyBodiesAndEndOfStream(t *testing.T) {
	state := newEnvoyStreamState()
	messages := []struct {
		message *extprocv3.ProcessingRequest
		kind    envoyMessageKind
		stage   ProcessingStage
	}{
		{requestHeadersForAdapterTest(false), envoyRequestHeaders, StageRequest},
		{requestBodyForAdapterTest(nil, true), envoyRequestBody, StageRequest},
		{responseHeadersForAdapterTest(false), envoyResponseHeaders, StageResponse},
		{responseBodyForAdapterTest(nil, true), envoyResponseBody, StageResponse},
	}
	for index, test := range messages {
		request, kind, err := requestFromEnvoy(test.message, state)
		if err != nil {
			t.Fatalf("requestFromEnvoy(%d) error = %v", index, err)
		}
		if kind != test.kind || request.Stage != test.stage || len(request.Body) != 0 {
			t.Fatalf("request %d = %+v kind=%s, want stage=%s kind=%s empty body", index, request, kind, test.stage, test.kind)
		}
	}
	if !state.requestEnded || !state.responseEnded {
		t.Fatalf("end-of-stream state was not recorded: %+v", state)
	}
}

func TestRequestFromEnvoyRejectsInvalidOrUnexpectedSequence(t *testing.T) {
	tests := []struct {
		name     string
		messages []*extprocv3.ProcessingRequest
	}{
		{name: "nil", messages: []*extprocv3.ProcessingRequest{nil}},
		{name: "empty", messages: []*extprocv3.ProcessingRequest{{}}},
		{name: "body before headers", messages: []*extprocv3.ProcessingRequest{requestBodyForAdapterTest([]byte("body"), true)}},
		{name: "response before request completes", messages: []*extprocv3.ProcessingRequest{
			requestHeadersForAdapterTest(false), responseHeadersForAdapterTest(true),
		}},
		{name: "duplicate request headers", messages: []*extprocv3.ProcessingRequest{
			requestHeadersForAdapterTest(true), requestHeadersForAdapterTest(true),
		}},
		{name: "body after end of stream", messages: []*extprocv3.ProcessingRequest{
			requestHeadersForAdapterTest(true), requestBodyForAdapterTest([]byte("body"), true),
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := newEnvoyStreamState()
			for index, message := range test.messages {
				_, _, err := requestFromEnvoy(message, state)
				if index == len(test.messages)-1 {
					if err == nil {
						t.Fatalf("message %d unexpectedly succeeded", index)
					}
					return
				}
				if err != nil {
					t.Fatalf("setup message %d error = %v", index, err)
				}
			}
		})
	}
}

func TestRequestFromEnvoyDoesNotTrustDownstreamPolicyHeader(t *testing.T) {
	message := &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestHeaders{RequestHeaders: &extprocv3.HttpHeaders{
		Headers: testHeaderMap([2]string{"x-tsz-policy-id", "spoofed"}),
	}}}
	request, _, err := requestFromEnvoy(message, newEnvoyStreamState())
	if err != nil {
		t.Fatalf("requestFromEnvoy() error = %v", err)
	}
	if request.PolicyID != "" {
		t.Fatalf("untrusted policy header selected policy %q", request.PolicyID)
	}
}

func TestRequestFromEnvoyRejectsLegacyPolicyMetadataSource(t *testing.T) {
	message := requestHeadersForAdapterTest(true)
	message.MetadataContext = &corev3.Metadata{FilterMetadata: map[string]*structpb.Struct{
		safeMetadataNamespace: {Fields: map[string]*structpb.Value{"policy_id": structpb.NewStringValue("legacy-policy")}},
	}}
	if _, _, err := requestFromEnvoy(message, newEnvoyStreamState()); err == nil {
		t.Fatal("requestFromEnvoy() accepted legacy policy metadata")
	}
}

func TestResponseToEnvoyMapsActionsAndMessageKinds(t *testing.T) {
	allow, err := responseToEnvoy(envoyResponseBody, StageResponse, ProcessingResult{Action: ActionAllow})
	if err != nil {
		t.Fatalf("responseToEnvoy(ALLOW) error = %v", err)
	}
	if allow.GetResponseBody() == nil || allow.GetResponseBody().GetResponse().GetStatus() != extprocv3.CommonResponse_CONTINUE {
		t.Fatalf("ALLOW response = %+v", allow)
	}

	masked, err := responseToEnvoy(envoyRequestBody, StageRequest, ProcessingResult{
		Action: ActionMask, Body: []byte("masked"), HeaderMutations: map[string]string{"x-tsz-action": "MASK"},
	})
	if err != nil {
		t.Fatalf("responseToEnvoy(MASK) error = %v", err)
	}
	common := masked.GetRequestBody().GetResponse()
	if string(common.GetBodyMutation().GetBody()) != "masked" || len(common.GetHeaderMutation().GetSetHeaders()) != 1 {
		t.Fatalf("MASK response = %+v", masked)
	}

	blocked, err := responseToEnvoy(envoyRequestBody, StageRequest, ProcessingResult{
		Action: ActionBlock,
		// A processor must not be able to leak untrusted body material through
		// the immediate response, even if it accidentally supplies one.
		Body: []byte("raw secret=do-not-return"),
		Metadata: SafeMetadata{
			RID: "rid-1", RequestID: "envoy-1", PolicyID: "policy-1", PolicyVersion: 7,
		},
	})
	if err != nil {
		t.Fatalf("responseToEnvoy(BLOCK) error = %v", err)
	}
	immediate := blocked.GetImmediateResponse()
	if immediate.GetStatus().GetCode() != typev3.StatusCode_BadRequest {
		t.Fatalf("BLOCK response = %+v", blocked)
	}
	var payload blockErrorResponse
	if err := json.Unmarshal(immediate.GetBody(), &payload); err != nil {
		t.Fatalf("decode safe BLOCK body: %v; body=%q", err, immediate.GetBody())
	}
	if payload.Error.Code != blockErrorCode || payload.Error.Message != blockErrorMessage ||
		payload.TSZMeta.RID != "rid-1" || payload.TSZMeta.EnvoyRequestID != "envoy-1" ||
		payload.TSZMeta.PolicyID != "policy-1" || payload.TSZMeta.PolicyVersion != 7 {
		t.Fatalf("safe BLOCK payload = %+v", payload)
	}
	if strings.Contains(string(immediate.GetBody()), "do-not-return") {
		t.Fatalf("BLOCK body leaked unsafe processor data: %q", immediate.GetBody())
	}
	blockHeaders := make(map[string]string)
	for _, header := range immediate.GetHeaders().GetSetHeaders() {
		blockHeaders[header.GetHeader().GetKey()] = string(header.GetHeader().GetRawValue())
	}
	if blockHeaders["content-type"] != "application/json" || blockHeaders["content-length"] != stringLength(immediate.GetBody()) {
		t.Fatalf("BLOCK headers = %v, body length = %d", blockHeaders, len(immediate.GetBody()))
	}
	if _, err := responseToEnvoy(envoyResponseBody, StageResponse, ProcessingResult{Action: ActionBlock}); err == nil {
		t.Fatal("response-stage BLOCK was accepted in Phase 1")
	}

	withMetadata, err := responseToEnvoy(envoyRequestHeaders, StageRequest, ProcessingResult{
		Action:   ActionAllow,
		Metadata: SafeMetadata{RID: "rid-1", Stage: StageRequest, Action: ActionAllow, Categories: []string{"PII", "SECRET"}, DetectionCount: 2},
	})
	if err != nil {
		t.Fatalf("responseToEnvoy(metadata) error = %v", err)
	}
	namespace := withMetadata.GetDynamicMetadata().GetFields()["io.thyris.tsz"].GetStructValue()
	if namespace.GetFields()["rid"].GetStringValue() != "rid-1" || len(namespace.GetFields()["categories"].GetListValue().GetValues()) != 2 {
		t.Fatalf("dynamic metadata = %+v", withMetadata.GetDynamicMetadata())
	}
}

func stringLength(value []byte) string {
	return fmt.Sprintf("%d", len(value))
}

func testHeaderMap(values ...[2]string) *corev3.HeaderMap {
	result := &corev3.HeaderMap{Headers: make([]*corev3.HeaderValue, 0, len(values))}
	for _, value := range values {
		result.Headers = append(result.Headers, &corev3.HeaderValue{Key: value[0], RawValue: []byte(value[1])})
	}
	return result
}

// The semantic helpers keep generated Envoy message details out of individual
// test cases.
func requestHeadersForAdapterTest(endOfStream bool) *extprocv3.ProcessingRequest {
	return &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestHeaders{
		RequestHeaders: &extprocv3.HttpHeaders{Headers: testHeaderMap([2]string{"content-type", "application/json"}), EndOfStream: endOfStream},
	}}
}

func requestBodyForAdapterTest(body []byte, endOfStream bool) *extprocv3.ProcessingRequest {
	return &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestBody{
		RequestBody: &extprocv3.HttpBody{Body: body, EndOfStream: endOfStream},
	}}
}

func responseHeadersForAdapterTest(endOfStream bool) *extprocv3.ProcessingRequest {
	return &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_ResponseHeaders{
		ResponseHeaders: &extprocv3.HttpHeaders{Headers: testHeaderMap([2]string{"content-type", "application/json"}), EndOfStream: endOfStream},
	}}
}

func responseBodyForAdapterTest(body []byte, endOfStream bool) *extprocv3.ProcessingRequest {
	return &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_ResponseBody{
		ResponseBody: &extprocv3.HttpBody{Body: body, EndOfStream: endOfStream},
	}}
}
