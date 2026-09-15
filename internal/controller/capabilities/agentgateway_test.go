package capabilities

import (
	"errors"
	"testing"

	securityv1beta1 "thyris-sz/api/v1beta1"
)

func TestAgentgatewayChatCompletionsCapabilities(t *testing.T) {
	if err := ValidateDeclaration(AgentgatewayChatCompletionsCapabilities); err != nil {
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
	negotiation, err := Negotiate(required, AgentgatewayChatCompletionsCapabilities)
	if err != nil {
		t.Fatalf("Negotiate() error = %v", err)
	}
	if negotiation.AdapterName != "agentgateway" || negotiation.AdapterVersion != "1.0.0" {
		t.Fatalf("negotiation = %+v", negotiation)
	}
}

func TestAgentgatewayChatCompletionsProfileDoesNotOverclaimLaterPhases(t *testing.T) {
	streaming := securityv1beta1.TSZGuardrailPolicySpec{
		Streaming: &securityv1beta1.StreamingSpec{Enabled: true, Mode: string(StreamingWindowed)},
	}
	if _, err := NegotiateSpec(streaming, AgentgatewayChatCompletionsCapabilities); !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("streaming negotiation error = %v, want ErrUnsupportedCapability", err)
	}

	if AgentgatewayChatCompletionsCapabilities.NativePolicyAttachment {
		t.Fatal("profile claims automatic native policy reconciliation before it is implemented")
	}
}
