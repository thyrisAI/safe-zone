package capabilities

import (
	"errors"
	"testing"

	securityv1beta1 "thyris-sz/api/v1beta1"
)

func TestAgentgatewayBufferedContentCapabilities(t *testing.T) {
	if err := ValidateDeclaration(AgentgatewayBufferedContentCapabilities); err != nil {
		t.Fatalf("ValidateDeclaration() error = %v", err)
	}

	required := Requirements{
		CapabilityRequestHeaders,
		CapabilityRequestBufferedBody,
		CapabilityRequestBodyMutation,
		CapabilityImmediateResponse,
		CapabilityResponseBufferedBody,
		CapabilityResponseBodyMutation,
		CapabilityDynamicMetadata,
	}
	negotiation, err := Negotiate(required, AgentgatewayBufferedContentCapabilities)
	if err != nil {
		t.Fatalf("Negotiate() error = %v", err)
	}
	if negotiation.AdapterName != "agentgateway" || negotiation.AdapterVersion != "1.0.0" {
		t.Fatalf("negotiation = %+v", negotiation)
	}
}

func TestAgentgatewayBufferedContentProfileDoesNotOverclaimStreaming(t *testing.T) {
	streaming := securityv1beta1.TSZGuardrailPolicySpec{
		Streaming: &securityv1beta1.StreamingSpec{Enabled: true, Mode: string(StreamingWindowed)},
	}
	if _, err := NegotiateSpec(streaming, AgentgatewayBufferedContentCapabilities); !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("streaming negotiation error = %v, want ErrUnsupportedCapability", err)
	}

	if !AgentgatewayBufferedContentCapabilities.NativePolicyAttachment {
		t.Fatal("profile does not claim the installed native AgentgatewayPolicy reconciler")
	}
}
