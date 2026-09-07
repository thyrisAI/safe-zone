package nativeadapter_test

import (
	"errors"
	"testing"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1alpha2"
	"thyris-sz/internal/controller/capabilities"
	"thyris-sz/internal/controller/envoyresource"
	"thyris-sz/internal/controller/nativeadapter"
)

type descriptorAdapter struct {
	nativeadapter.Adapter
	descriptor nativeadapter.Descriptor
}

func (a descriptorAdapter) Descriptor() nativeadapter.Descriptor { return a.descriptor }

func TestRegistryRejectsInvalidRegistrationsAndUnknownSelection(t *testing.T) {
	envoy := &envoyresource.EnvoyResourceReconciler{}
	portable := envoy.Descriptor()
	portable.Capabilities.NativePolicyAttachment = false
	invalid := envoy.Descriptor()
	invalid.Capabilities.RequestBufferedBody = false
	for _, adapters := range [][]nativeadapter.Adapter{{nil}, {envoy, envoy}, {descriptorAdapter{descriptor: portable}}, {descriptorAdapter{descriptor: invalid}}, {descriptorAdapter{}}} {
		if _, err := nativeadapter.NewRegistry(adapters...); err == nil {
			t.Fatal("invalid registration accepted")
		}
	}
	r, err := nativeadapter.NewRegistry(envoy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get("uninstalled-gateway"); !errors.Is(err, capabilities.ErrUnsupportedCapability) {
		t.Fatalf("unknown adapter error = %v", err)
	}
	got, err := r.Get("envoy-gateway")
	if err != nil || got != envoy {
		t.Fatalf("registered adapter = %v, %v", got, err)
	}
	all := r.All()
	all[0] = nil
	if r.All()[0] != envoy {
		t.Fatal("caller mutated registry")
	}
}

func TestDescriptorRejectsUnsupportedTargetAndSection(t *testing.T) {
	d := nativeadapter.Descriptor{Capabilities: capabilities.AdapterCapabilities{Name: "test-gateway"}, Targets: []nativeadapter.TargetCapability{{Group: gatewayv1.GroupName, Kind: "HTTPRoute"}}}
	ref := gateway.LocalPolicyTargetReferenceWithSectionName{LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Name: "orders"}}
	if err := d.CheckTarget(ref); err != nil {
		t.Fatal(err)
	}
	section := gatewayv1.SectionName("rule")
	ref.SectionName = &section
	if err := d.CheckTarget(ref); !errors.Is(err, capabilities.ErrUnsupportedCapability) {
		t.Fatalf("section error = %v", err)
	}
	ref.SectionName = nil
	ref.Kind = "GRPCRoute"
	if err := d.CheckTarget(ref); !errors.Is(err, capabilities.ErrUnsupportedCapability) {
		t.Fatalf("kind error = %v", err)
	}
	ref.Kind, ref.Group = "HTTPRoute", "unknown.example"
	if err := d.CheckTarget(ref); !errors.Is(err, capabilities.ErrUnsupportedCapability) {
		t.Fatalf("group error = %v", err)
	}
}
