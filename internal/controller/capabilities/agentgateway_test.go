package capabilities

import (
	"errors"
	"testing"

	securityv1beta1 "thyris-sz/api/v1beta1"
)

func TestAgentgatewayOpenAIBufferedCapabilities(t *testing.T) {
	if err := ValidateDeclaration(AgentgatewayOpenAIBufferedCapabilities); err != nil {
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
	negotiation, err := Negotiate(required, AgentgatewayOpenAIBufferedCapabilities)
	if err != nil {
		t.Fatalf("Negotiate() error = %v", err)
	}
	if negotiation.AdapterName != "agentgateway" || negotiation.AdapterVersion != "1.0.0" {
		t.Fatalf("negotiation = %+v", negotiation)
	}
}

func TestAgentgatewayOpenAIBufferedProfileDoesNotOverclaimLaterPhases(t *testing.T) {
	streaming := securityv1beta1.TSZGuardrailPolicySpec{
		Streaming: &securityv1beta1.StreamingSpec{Enabled: true, Mode: string(StreamingWindowed)},
	}
	if _, err := NegotiateSpec(streaming, AgentgatewayOpenAIBufferedCapabilities); !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("streaming negotiation error = %v, want ErrUnsupportedCapability", err)
	}

	if AgentgatewayOpenAIBufferedCapabilities.NativePolicyAttachment {
		t.Fatal("profile claims automatic native policy reconciliation before it is implemented")
	}
}
