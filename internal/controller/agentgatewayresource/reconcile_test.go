package agentgatewayresource_test

import (
	"context"
	"testing"

	security "thyris-sz/api/v1beta1"
	"thyris-sz/internal/controller"
	"thyris-sz/internal/controller/agentgatewayresource"
	"thyris-sz/internal/controller/nativeadapter"
	policy "thyris-sz/internal/extproc/policy"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestBuildAgentgatewayPolicyUsesBufferedExtProcAndTrustedIdentity(t *testing.T) {
	section := gatewayv1.SectionName("chat")
	target := gateway.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Name: "llm-api"},
		SectionName:                &section,
	}
	owner := &security.TSZGuardrailPolicy{ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: "apps"}}
	object := agentgatewayresource.BuildAgentgatewayPolicy(owner, target, nativeadapter.EffectivePolicy{}, policy.RouteIdentity{Gateway: "ai", Listener: "https", Route: "llm-api", Rule: "chat"})

	assertNested(t, object.Object, "FailClosed", "spec", "traffic", "extProc", "failureMode")
	assertNested(t, object.Object, "Buffered", "spec", "traffic", "extProc", "processingOptions", "requestBodyMode")
	assertNested(t, object.Object, "Buffered", "spec", "traffic", "extProc", "processingOptions", "responseBodyMode")
	assertNested(t, object.Object, false, "spec", "traffic", "extProc", "processingOptions", "allowModeOverride")
	assertNested(t, object.Object, `"ai"`, "spec", "traffic", "extProc", "requestAttributes", "xds.gateway_name")
	assertNested(t, object.Object, `"llm-api"`, "spec", "traffic", "extProc", "requestAttributes", "xds.route_name")
	assertNested(t, object.Object, `"chat"`, "spec", "traffic", "extProc", "requestAttributes", "xds.route_rule_name")

	failOpen := agentgatewayresource.BuildAgentgatewayPolicy(owner, target, nativeadapter.EffectivePolicy{RequestFailOpen: true, ResponseFailOpen: true}, policy.RouteIdentity{Route: "llm-api"})
	assertNested(t, failOpen.Object, "FailOpen", "spec", "traffic", "extProc", "failureMode")
	mixed := agentgatewayresource.BuildAgentgatewayPolicy(owner, target, nativeadapter.EffectivePolicy{ResponseFailOpen: true}, policy.RouteIdentity{Route: "llm-api"})
	assertNested(t, mixed.Object, "FailClosed", "spec", "traffic", "extProc", "failureMode")
}

func TestReconcileAndOwnedRemoval(t *testing.T) {
	scheme, err := controller.NewScheme()
	if err != nil {
		t.Fatal(err)
	}
	gatewayName := gatewayv1.ObjectName("ai-gateway")
	listenerName := gatewayv1.SectionName("https")
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "llm-api", Namespace: "apps"},
		Spec:       gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: gatewayName, SectionName: &listenerName}}}},
	}
	owner := &security.TSZGuardrailPolicy{TypeMeta: metav1.TypeMeta{APIVersion: security.GroupVersion.String(), Kind: "TSZGuardrailPolicy"}, ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: "apps", UID: types.UID("owner")}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(route).Build()
	reconciler := &agentgatewayresource.Reconciler{Client: kubeClient, Scheme: scheme}
	target := gateway.LocalPolicyTargetReferenceWithSectionName{LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Name: "llm-api"}}

	if _, err := reconciler.Reconcile(context.Background(), owner, target, nativeadapter.EffectivePolicy{}); err != nil {
		t.Fatal(err)
	}
	created := policyObject(agentgatewayresource.DeterministicName(target), "apps")
	if err := kubeClient.Get(context.Background(), clientKey(created), created); err != nil {
		t.Fatal(err)
	}
	if refs := created.GetOwnerReferences(); len(refs) != 1 || refs[0].UID != owner.UID {
		t.Fatalf("owner references = %+v", refs)
	}
	assertNested(t, created.Object, `"ai-gateway"`, "spec", "traffic", "extProc", "requestAttributes", "xds.gateway_name")
	assertNested(t, created.Object, `"https"`, "spec", "traffic", "extProc", "requestAttributes", "xds.listener_name")
	if _, err := reconciler.Reconcile(context.Background(), owner, target, nativeadapter.EffectivePolicy{RequestFailOpen: true, ResponseFailOpen: true}); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Get(context.Background(), clientKey(created), created); err != nil {
		t.Fatal(err)
	}
	assertNested(t, created.Object, "FailOpen", "spec", "traffic", "extProc", "failureMode")

	if err := reconciler.Remove(context.Background(), owner, target); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Get(context.Background(), clientKey(created), policyObject(created.GetName(), created.GetNamespace())); client.IgnoreNotFound(err) != nil || err == nil {
		t.Fatalf("resource was not removed: %v", err)
	}
}

func TestDescriptorClaimsOnlyImplementedNativeTargets(t *testing.T) {
	descriptor := (&agentgatewayresource.Reconciler{}).Descriptor()
	if !descriptor.Capabilities.NativePolicyAttachment || descriptor.ResourceKind != "AgentgatewayPolicy" || len(descriptor.Targets) != 2 {
		t.Fatalf("descriptor = %+v", descriptor)
	}
}

func policyObject(name, namespace string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetAPIVersion("agentgateway.dev/v1alpha1")
	object.SetKind("AgentgatewayPolicy")
	object.SetName(name)
	object.SetNamespace(namespace)
	return object
}

func clientKey(object client.Object) client.ObjectKey { return client.ObjectKeyFromObject(object) }

func assertNested(t *testing.T, object map[string]any, want any, fields ...string) {
	t.Helper()
	got, found, err := unstructured.NestedFieldNoCopy(object, fields...)
	if err != nil || !found || got != want {
		t.Fatalf("%v = %#v, found=%v, err=%v; want %#v", fields, got, found, err, want)
	}
}
