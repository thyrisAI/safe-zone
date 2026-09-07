package capabilities

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	securityv1beta1 "thyris-sz/api/v1beta1"
	"thyris-sz/internal/extproc/policy"
)

func TestCheckCapabilitiesAcceptsWindowedStreaming(t *testing.T) {
	err := CheckCapabilities(securityv1beta1.TSZGuardrailPolicySpec{Streaming: &securityv1beta1.StreamingSpec{Enabled: true, Mode: "Windowed"}}, EnvoyGatewayCapabilities)
	if err != nil {
		t.Fatalf("CheckCapabilities() error = %v", err)
	}
}

func TestCheckCapabilitiesRejectsUnsupportedWindowedStreaming(t *testing.T) {
	err := CheckCapabilities(securityv1beta1.TSZGuardrailPolicySpec{
		Streaming: &securityv1beta1.StreamingSpec{Enabled: true, Mode: "Windowed"},
		Response:  &securityv1beta1.ResponsePolicySpec{Enabled: true, PII: securityv1beta1.PolicyActionMask, Secret: securityv1beta1.PolicyActionBlock, UnsafeContent: securityv1beta1.PolicyActionAuditOnly},
	}, EnvoyGatewayCapabilities)
	if !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("CheckCapabilities() error = %v, want ErrUnsupportedCapability", err)
	}
}

func TestCapabilitiesRejectUnsupportedEnforcement(t *testing.T) {
	tests := []struct {
		name     string
		restrict func(*AdapterCapabilities)
		spec     securityv1beta1.TSZGuardrailPolicySpec
	}{
		{"native attachment", func(c *AdapterCapabilities) { c.NativePolicyAttachment = false }, securityv1beta1.TSZGuardrailPolicySpec{}},
		{"headers", func(c *AdapterCapabilities) { c.RequestHeaders = false }, securityv1beta1.TSZGuardrailPolicySpec{}},
		{"request inspection", func(c *AdapterCapabilities) { c.RequestBufferedBody, c.RequestBodyMutation = false, false }, securityv1beta1.TSZGuardrailPolicySpec{}},
		{"request masking", func(c *AdapterCapabilities) { c.RequestBodyMutation = false }, securityv1beta1.TSZGuardrailPolicySpec{Request: &securityv1beta1.RequestPolicySpec{PII: securityv1beta1.PolicyActionMask}}},
		{"blocking", func(c *AdapterCapabilities) { c.ImmediateResponse = false }, securityv1beta1.TSZGuardrailPolicySpec{Request: &securityv1beta1.RequestPolicySpec{Secret: securityv1beta1.PolicyActionBlock}}},
		{"response inspection", func(c *AdapterCapabilities) {
			c.ResponseBufferedBody, c.ResponseBodyMutation, c.ResponseStreaming = false, false, StreamingNone
		}, securityv1beta1.TSZGuardrailPolicySpec{Response: &securityv1beta1.ResponsePolicySpec{Enabled: true}}},
		{"response masking", func(c *AdapterCapabilities) { c.ResponseBodyMutation = false }, securityv1beta1.TSZGuardrailPolicySpec{Response: &securityv1beta1.ResponsePolicySpec{Enabled: true, PII: securityv1beta1.PolicyActionMask}}},
		{"streaming", func(c *AdapterCapabilities) { c.ResponseStreaming = StreamingNone }, securityv1beta1.TSZGuardrailPolicySpec{Streaming: &securityv1beta1.StreamingSpec{Enabled: true, Mode: "Windowed"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := EnvoyGatewayCapabilities
			tt.restrict(&caps)
			if err := CheckCapabilities(tt.spec, caps); !errors.Is(err, ErrUnsupportedCapability) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestNegotiateSpecReturnsExplicitAgreement(t *testing.T) {
	spec := securityv1beta1.TSZGuardrailPolicySpec{
		Request:   &securityv1beta1.RequestPolicySpec{PII: securityv1beta1.PolicyActionMask, Secret: securityv1beta1.PolicyActionBlock},
		Response:  &securityv1beta1.ResponsePolicySpec{Enabled: true, PII: securityv1beta1.PolicyActionMask},
		Streaming: &securityv1beta1.StreamingSpec{Enabled: true, Mode: "Windowed"},
	}
	got, err := NegotiateSpec(spec, EnvoyGatewayCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	want := Negotiation{
		AdapterName:    "envoy-gateway",
		AdapterVersion: "1.8.3",
		Required: Requirements{
			CapabilityNativePolicyAttachment,
			CapabilityRequestHeaders,
			CapabilityRequestBufferedBody,
			CapabilityRequestBodyMutation,
			CapabilityImmediateResponse,
			CapabilityResponseBufferedBody,
			CapabilityResponseBodyMutation,
			CapabilityResponseStreamingWindowed,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("negotiation = %#v, want %#v", got, want)
	}
}

func TestNegotiateReportsEveryMissingCapability(t *testing.T) {
	offered := AdapterCapabilities{Name: "audit-gateway", Version: "0.1.0", RequestHeaders: true}
	_, err := Negotiate(Requirements{
		CapabilityNativePolicyAttachment,
		CapabilityRequestHeaders,
		CapabilityRequestBufferedBody,
		CapabilityImmediateResponse,
		CapabilityDynamicMetadata,
	}, offered)
	if !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("error = %v, want ErrUnsupportedCapability", err)
	}
	for _, missing := range []string{"nativePolicyAttachment", "requestBufferedBody", "immediateResponse", "dynamicMetadata"} {
		if !strings.Contains(err.Error(), missing) {
			t.Errorf("error %q does not report %s", err, missing)
		}
	}
	if strings.Count(err.Error(), "requestHeaders") != 0 {
		t.Fatalf("error reports offered capability: %v", err)
	}
}

func TestValidateDeclarationRejectsImpossibleCapabilities(t *testing.T) {
	tests := []AdapterCapabilities{
		{},
		{Name: "Not_A_DNS_Label", Version: "1"},
		{Name: "test", Version: " 1 "},
		{Name: "test", Version: "1", ResponseStreaming: "Strict"},
		{Name: "test", Version: "1", RequestBodyMutation: true},
		{Name: "test", Version: "1", ResponseBodyMutation: true},
	}
	for _, caps := range tests {
		if err := ValidateDeclaration(caps); !errors.Is(err, ErrInvalidCapabilityDeclaration) {
			t.Errorf("ValidateDeclaration(%+v) error = %v", caps, err)
		}
	}
}

func TestNegotiateDefinitionChecksResolvedSnapshotActions(t *testing.T) {
	caps := EnvoyGatewayCapabilities
	caps.ImmediateResponse = false
	definition := policy.PolicyDefinition{Request: policy.RequestPolicy{Secret: policy.ActionBlock}}
	if _, err := NegotiateDefinition(definition, caps); !errors.Is(err, ErrUnsupportedCapability) || !strings.Contains(err.Error(), "immediateResponse") {
		t.Fatalf("error = %v, want missing immediateResponse", err)
	}
}

func TestAuditOnlyDoesNotRequireMutationOrBlocking(t *testing.T) {
	caps := EnvoyGatewayCapabilities
	caps.RequestBodyMutation, caps.ResponseBodyMutation, caps.ImmediateResponse = false, false, false
	spec := securityv1beta1.TSZGuardrailPolicySpec{
		Request:  &securityv1beta1.RequestPolicySpec{PII: securityv1beta1.PolicyActionAuditOnly},
		Response: &securityv1beta1.ResponsePolicySpec{Enabled: true, PII: securityv1beta1.PolicyActionAuditOnly},
	}
	if err := CheckCapabilities(spec, caps); err != nil {
		t.Fatal(err)
	}
}
