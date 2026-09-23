package capabilities

import (
	"errors"
	"strings"
	"testing"

	securityv1beta1 "thyris-sz/api/v1beta1"
	"thyris-sz/internal/extproc/policy"
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

func TestAgentgatewayRejectsIndependentFailurePolicies(t *testing.T) {
	spec := securityv1beta1.TSZGuardrailPolicySpec{FailurePolicy: securityv1beta1.FailurePolicySpec{
		Request:  securityv1beta1.FailureModeClosed,
		Response: securityv1beta1.FailureModeOpen,
	}}
	_, err := NegotiateSpec(spec, AgentgatewayBufferedContentCapabilities)
	if !errors.Is(err, ErrUnsupportedCapability) || !strings.Contains(err.Error(), string(CapabilityIndependentFailurePolicy)) {
		t.Fatalf("NegotiateSpec() error = %v, want missing independent failure-policy capability", err)
	}
}

func TestAgentgatewayRejectsIndependentFailurePoliciesFromResolvedSnapshot(t *testing.T) {
	definition := policy.PolicyDefinition{FailurePolicy: policy.FailurePolicy{
		Request:  policy.FailureModeClosed,
		Response: policy.FailureModeOpen,
	}}
	_, err := NegotiateDefinition(definition, AgentgatewayBufferedContentCapabilities)
	if !errors.Is(err, ErrUnsupportedCapability) || !strings.Contains(err.Error(), string(CapabilityIndependentFailurePolicy)) {
		t.Fatalf("NegotiateDefinition() error = %v, want missing independent failure-policy capability", err)
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
