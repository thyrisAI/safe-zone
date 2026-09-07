package envoy

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	"thyris-sz/internal/controller/capabilities"
	"thyris-sz/internal/extproc"
	"thyris-sz/internal/extproc/adaptertest"
)

func TestAdapterContract(t *testing.T) {
	adaptertest.Run(t, envoyContractDriver{})
}

type envoyContractDriver struct{}

func (envoyContractDriver) Name() string { return "envoy-gateway" }
func (envoyContractDriver) Capabilities() capabilities.AdapterCapabilities {
	return capabilities.EnvoyGatewayCapabilities
}
func (envoyContractDriver) NewSession() adaptertest.Session {
	return &envoyContractSession{state: newEnvoyStreamState()}
}

type envoyContractSession struct {
	state *envoyStreamState
}

func (s *envoyContractSession) AdaptInput(input adaptertest.Input) (extproc.ProcessingRequest, error) {
	message, err := envoyContractInput(input)
	if err != nil {
		return extproc.ProcessingRequest{}, err
	}
	request, kind, err := requestFromEnvoy(message, s.state)
	if err != nil {
		return extproc.ProcessingRequest{}, err
	}
	if kind != envoyKind(input.Kind) {
		return extproc.ProcessingRequest{}, fmt.Errorf("Envoy kind %q does not match contract kind %q", kind, input.Kind)
	}
	return request, nil
}

func (s *envoyContractSession) AdaptOutput(kind adaptertest.MessageKind, result extproc.ProcessingResult) (adaptertest.Output, error) {
	stage, err := kind.Stage()
	if err != nil {
		return adaptertest.Output{}, err
	}
	response, err := responseToEnvoy(envoyKind(kind), stage, result)
	if err != nil {
		return adaptertest.Output{}, err
	}
	wire, err := protojson.Marshal(response)
	if err != nil {
		return adaptertest.Output{}, err
	}
	output := adaptertest.Output{Wire: wire}
	if immediate := response.GetImmediateResponse(); immediate != nil {
		output.Immediate = true
		output.StatusCode = int(immediate.GetStatus().GetCode())
		output.HeaderMutations = envoyHeaderMutations(immediate.GetHeaders())
	} else {
		common := envoyCommonResponse(response, kind)
		if common == nil {
			return adaptertest.Output{}, fmt.Errorf("Envoy response has no %s common response", kind)
		}
		output.Continued = common.GetStatus() == extprocv3.CommonResponse_CONTINUE
		output.HeaderMutations = envoyHeaderMutations(common.GetHeaderMutation())
		if common.GetBodyMutation() != nil {
			output.BodyMutationSet = true
			output.BodyMutation = append([]byte(nil), common.GetBodyMutation().GetBody()...)
		}
	}
	if response.GetDynamicMetadata() != nil {
		metadata, found, err := envoyContractMetadata(response.GetDynamicMetadata())
		if err != nil {
			return adaptertest.Output{}, err
		}
		output.Metadata, output.DynamicMetadataSet = metadata, found
	}
	return output, nil
}

func envoyContractInput(input adaptertest.Input) (*extprocv3.ProcessingRequest, error) {
	headers := &corev3.HeaderMap{}
	keys := make([]string, 0, len(input.Headers))
	for key := range input.Headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range input.Headers[key] {
			headers.Headers = append(headers.Headers, &corev3.HeaderValue{Key: key, RawValue: []byte(value)})
		}
	}
	message := &extprocv3.ProcessingRequest{Attributes: envoyContractAttributes(input.Attributes)}
	switch input.Kind {
	case adaptertest.RequestHeaders:
		message.Request = &extprocv3.ProcessingRequest_RequestHeaders{RequestHeaders: &extprocv3.HttpHeaders{Headers: headers, EndOfStream: input.EndOfStream}}
	case adaptertest.RequestBody:
		message.Request = &extprocv3.ProcessingRequest_RequestBody{RequestBody: &extprocv3.HttpBody{Body: input.Body, EndOfStream: input.EndOfStream}}
	case adaptertest.ResponseHeaders:
		message.Request = &extprocv3.ProcessingRequest_ResponseHeaders{ResponseHeaders: &extprocv3.HttpHeaders{Headers: headers, EndOfStream: input.EndOfStream}}
	case adaptertest.ResponseBody:
		message.Request = &extprocv3.ProcessingRequest_ResponseBody{ResponseBody: &extprocv3.HttpBody{Body: input.Body, EndOfStream: input.EndOfStream}}
	default:
		return nil, fmt.Errorf("unknown adapter contract input kind %q", input.Kind)
	}
	return message, nil
}

func envoyContractAttributes(attributes map[string]string) map[string]*structpb.Struct {
	if len(attributes) == 0 {
		return nil
	}
	fields := make(map[string]*structpb.Value, len(attributes))
	for key, value := range attributes {
		fields[key] = structpb.NewStringValue(value)
	}
	return map[string]*structpb.Struct{"envoy.filters.http.ext_proc": {Fields: fields}}
}

func envoyKind(kind adaptertest.MessageKind) envoyMessageKind {
	switch kind {
	case adaptertest.RequestHeaders:
		return envoyRequestHeaders
	case adaptertest.RequestBody:
		return envoyRequestBody
	case adaptertest.ResponseHeaders:
		return envoyResponseHeaders
	case adaptertest.ResponseBody:
		return envoyResponseBody
	default:
		return envoyMessageKind(kind)
	}
}

func envoyCommonResponse(response *extprocv3.ProcessingResponse, kind adaptertest.MessageKind) *extprocv3.CommonResponse {
	switch kind {
	case adaptertest.RequestHeaders:
		return response.GetRequestHeaders().GetResponse()
	case adaptertest.RequestBody:
		return response.GetRequestBody().GetResponse()
	case adaptertest.ResponseHeaders:
		return response.GetResponseHeaders().GetResponse()
	case adaptertest.ResponseBody:
		return response.GetResponseBody().GetResponse()
	default:
		return nil
	}
}

func envoyHeaderMutations(mutation *extprocv3.HeaderMutation) map[string]string {
	result := make(map[string]string)
	if mutation == nil {
		return result
	}
	for _, header := range mutation.GetSetHeaders() {
		result[header.GetHeader().GetKey()] = string(header.GetHeader().GetRawValue())
	}
	return result
}

func envoyContractMetadata(value *structpb.Struct) (extproc.SafeMetadata, bool, error) {
	namespace := value.GetFields()[safeMetadataNamespace].GetStructValue()
	if namespace == nil {
		return extproc.SafeMetadata{}, false, nil
	}
	encoded, err := json.Marshal(namespace.AsMap())
	if err != nil {
		return extproc.SafeMetadata{}, false, err
	}
	var metadata extproc.SafeMetadata
	if err := json.Unmarshal(encoded, &metadata); err != nil {
		return extproc.SafeMetadata{}, false, err
	}
	return metadata, true, nil
}
