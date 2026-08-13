package policyattach

import (
	"context"
	"testing"

	securityv1alpha1 "thyris-sz/api/v1alpha1"
	"thyris-sz/internal/controller"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestResolverResolvesSupportedTargetsAndSections(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme, err := controller.NewScheme()
	if err != nil {
		t.Fatalf("new scheme: %v", err)
	}

	listenerName := gatewayv1.SectionName("https")
	httpRuleName := gatewayv1.SectionName("inspect")
	grpcRuleName := gatewayv1.SectionName("grpc-inspect")
	resolver := &Resolver{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "edge"},
			Spec:       gatewayv1.GatewaySpec{Listeners: []gatewayv1.Listener{{Name: listenerName}}},
		},
		&gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "web"},
			Spec:       gatewayv1.HTTPRouteSpec{Rules: []gatewayv1.HTTPRouteRule{{Name: &httpRuleName}}},
		},
		&gatewayv1.GRPCRoute{
			ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "grpc"},
			Spec:       gatewayv1.GRPCRouteSpec{Rules: []gatewayv1.GRPCRouteRule{{Name: &grpcRuleName}}},
		},
	).Build()}

	policy := &securityv1alpha1.TSZGuardrailPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "guardrails"},
		Spec: securityv1alpha1.TSZGuardrailPolicySpec{TargetRefs: []gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			targetRef("Gateway", "edge", &listenerName),
			targetRef("HTTPRoute", "web", &httpRuleName),
			targetRef("GRPCRoute", "grpc", &grpcRuleName),
		}},
	}

	targets := resolver.ResolveTargets(ctx, policy)
	if len(targets) != 3 {
		t.Fatalf("resolved %d targets, want 3", len(targets))
	}
	for _, target := range targets {
		if target.Err != nil {
			t.Errorf("%s: unexpected resolve error: %v", target.Kind, target.Err)
		}
		if !target.SectionOK {
			t.Errorf("%s: expected matching section", target.Kind)
		}
	}
}

func TestResolverReportsInvalidTargetsWithoutSkippingThem(t *testing.T) {
	t.Parallel()
	scheme, err := controller.NewScheme()
	if err != nil {
		t.Fatalf("new scheme: %v", err)
	}
	missingSection := gatewayv1.SectionName("missing")
	resolver := &Resolver{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "edge"}},
	).Build()}
	policy := &securityv1alpha1.TSZGuardrailPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "apps"},
		Spec: securityv1alpha1.TSZGuardrailPolicySpec{TargetRefs: []gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			targetRef("Gateway", "edge", &missingSection),
			targetRef("HTTPRoute", "missing", nil),
			targetRefWithGroup("other.example", "Gateway", "edge", nil),
		}},
	}

	targets := resolver.ResolveTargets(context.Background(), policy)
	if len(targets) != 3 {
		t.Fatalf("resolved %d targets, want 3", len(targets))
	}
	if targets[0].Err != nil || targets[0].SectionOK {
		t.Errorf("missing section result = %#v, want resolved object with SectionOK=false", targets[0])
	}
	if targets[1].Err == nil {
		t.Error("missing HTTPRoute was silently accepted")
	}
	if targets[2].Err == nil {
		t.Error("unsupported group was silently accepted")
	}
}

func targetRef(kind, name string, section *gatewayv1.SectionName) gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return targetRefWithGroup(gatewayAPIGroup, kind, name, section)
}

func targetRefWithGroup(group, kind, name string, section *gatewayv1.SectionName) gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName(gatewayv1.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{
			Group: gatewayv1.Group(group),
			Kind:  gatewayv1.Kind(kind),
			Name:  gatewayv1.ObjectName(name),
		},
		SectionName: section,
	})
}
