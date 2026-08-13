package capabilities

import (
	"errors"
	"fmt"

	securityv1alpha1 "thyris-sz/api/v1alpha1"
)

type StreamingCapability string

const (
	StreamingNone     StreamingCapability = "None"
	StreamingWindowed StreamingCapability = "Windowed"
)

type AdapterCapabilities struct {
	Name                                                                        string
	Version                                                                     string
	RequestHeaders, RequestBufferedBody, RequestBodyMutation, ImmediateResponse bool
	ResponseBufferedBody, ResponseBodyMutation                                  bool
	ResponseStreaming                                                           StreamingCapability
	DynamicMetadata, NativePolicyAttachment                                     bool
}

var EnvoyGatewayCapabilities = AdapterCapabilities{Name: "envoy-gateway", Version: "1.8.3", RequestHeaders: true, RequestBufferedBody: true, RequestBodyMutation: true, ImmediateResponse: true, ResponseBufferedBody: true, ResponseBodyMutation: true, ResponseStreaming: StreamingNone, DynamicMetadata: true, NativePolicyAttachment: true}
var ErrUnsupportedCapability = errors.New("unsupported adapter capability")

func CheckCapabilities(spec securityv1alpha1.TSZGuardrailPolicySpec, caps AdapterCapabilities) error {
	if spec.Streaming != nil && spec.Streaming.Mode == "Windowed" && caps.ResponseStreaming != StreamingWindowed {
		return fmt.Errorf("%w: adapter %s does not support windowed response streaming", ErrUnsupportedCapability, caps.Name)
	}
	return nil
}
