package capabilities

import (
	"errors"
	"testing"

	securityv1alpha1 "thyris-sz/api/v1alpha1"
)

func TestCheckCapabilitiesRejectsUnsupportedWindowedStreaming(t *testing.T) {
	err := CheckCapabilities(securityv1alpha1.TSZGuardrailPolicySpec{Streaming: &securityv1alpha1.StreamingSpec{Mode: "Windowed"}}, EnvoyGatewayCapabilities)
	if !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("CheckCapabilities() error = %v", err)
	}
}
