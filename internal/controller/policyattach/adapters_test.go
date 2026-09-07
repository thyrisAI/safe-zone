package policyattach

import (
	"context"
	"errors"
	"reflect"
	"testing"

	security "thyris-sz/api/v1beta1"
	"thyris-sz/internal/controller"
	"thyris-sz/internal/controller/capabilities"
	"thyris-sz/internal/controller/effectivepolicy"
	"thyris-sz/internal/controller/nativeadapter"
	"thyris-sz/internal/extproc/policy"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gateway "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// This test-only adapter deliberately uses a non-Envoy Kubernetes resource.
// It verifies the controller boundary without claiming another gateway ships.
type configMapAdapter struct {
	client          client.Client
	scheme          *runtime.Scheme
	caps            capabilities.AdapterCapabilities
	negotiated      capabilities.Negotiation
	calls, removals int
	failure         error
}

func (a *configMapAdapter) Descriptor() nativeadapter.Descriptor {
	return nativeadapter.Descriptor{Capabilities: a.caps, Targets: []nativeadapter.TargetCapability{{Group: gatewayAPIGroup, Kind: "HTTPRoute"}}, ResourceKind: "ConfigMap", ProgrammedReason: "NativePolicyConfigured"}
}
func (a *configMapAdapter) Reconcile(ctx context.Context, owner *security.TSZGuardrailPolicy, ref gateway.LocalPolicyTargetReferenceWithSectionName, effective nativeadapter.EffectivePolicy) (controllerutil.OperationResult, error) {
	a.calls++
	a.negotiated = effective.NegotiatedCapabilities.Clone()
	if a.failure != nil {
		return controllerutil.OperationResultNone, a.failure
	}
	resource := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test-" + string(ref.Name), Namespace: owner.Namespace}}
	return controllerutil.CreateOrUpdate(ctx, a.client, resource, func() error {
		resource.Data = map[string]string{"policy": owner.Name}
		return ctrl.SetControllerReference(owner, resource, a.scheme)
	})
}
func (a *configMapAdapter) Remove(ctx context.Context, owner *security.TSZGuardrailPolicy, ref gateway.LocalPolicyTargetReferenceWithSectionName) error {
	a.removals++
	resource := &corev1.ConfigMap{}
	if err := a.client.Get(ctx, client.ObjectKey{Namespace: owner.Namespace, Name: "test-" + string(ref.Name)}, resource); err != nil {
		return client.IgnoreNotFound(err)
	}
	if ref := metav1.GetControllerOf(resource); ref != nil && ref.UID == owner.UID {
		return a.client.Delete(ctx, resource)
	}
	return nil
}
func (a *configMapAdapter) RouteIdentity(ref gateway.LocalPolicyTargetReferenceWithSectionName, _ client.Object) policy.RouteIdentity {
	return policy.RouteIdentity{Route: "test/" + string(ref.Name)}
}
func (a *configMapAdapter) OwnedResources() []client.Object {
	return []client.Object{&corev1.ConfigMap{}}
}
func (a *configMapAdapter) ManagedResourceCount(context.Context) (int, error) { return 0, nil }

func adapterFixture(t *testing.T, objects ...client.Object) (client.Client, *configMapAdapter, *recordingEnvoy, *PolicyAttachmentReconciler) {
	t.Helper()
	scheme, err := controller.NewScheme()
	if err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&security.TSZGuardrailPolicy{}).WithIndex(&security.TSZGuardrailPolicy{}, targetRefIndex, targetRefIndexValues).WithObjects(objects...).Build()
	caps := capabilities.EnvoyGatewayCapabilities
	caps.Name = "test-gateway"
	native := &configMapAdapter{client: c, scheme: scheme, caps: caps}
	envoy := &recordingEnvoy{}
	r := NewPolicyAttachmentReconciler(c, &Resolver{Client: c}, selector{}, nil, nil, testRegistry(t, envoy, native))
	return c, native, envoy, r
}

func TestSelectedAdapterOwnsResourcesAndConflictsAreAdapterScoped(t *testing.T) {
	ctx := context.Background()
	first := inlinePolicy("native", target("HTTPRoute", "orders", nil))
	first.UID, first.Spec.Adapter = "native-uid", "test-gateway"
	legacy := inlinePolicy("envoy", first.Spec.TargetRefs[0])
	c, native, envoy, r := adapterFixture(t, first, legacy)
	r.targetResolver = staticTargets{targets: []ResolvedTarget{{Kind: "HTTPRoute", Ref: first.Spec.TargetRefs[0], SectionOK: true}}}
	for i := 0; i < 2; i++ {
		if _, err := r.Reconcile(ctx, request(first)); err != nil {
			t.Fatal(err)
		}
	}
	if native.calls != 2 || envoy.calls != 0 {
		t.Fatalf("native/envoy calls = %d/%d", native.calls, envoy.calls)
	}
	if native.negotiated.AdapterName != "test-gateway" || native.negotiated.AdapterVersion != native.caps.Version || len(native.negotiated.Required) == 0 {
		t.Fatalf("negotiated capabilities = %+v", native.negotiated)
	}
	resources := &corev1.ConfigMapList{}
	if err := c.List(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if len(resources.Items) != 1 || metav1.GetControllerOf(&resources.Items[0]).UID != first.UID {
		t.Fatalf("generated resources = %+v", resources.Items)
	}
	got := &security.TSZGuardrailPolicy{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(first), got); err != nil {
		t.Fatal(err)
	}
	if condition := findCondition(got.Status.Conditions, security.ConditionProgrammed); condition.Status != metav1.ConditionTrue || condition.Reason != "NativePolicyConfigured" {
		t.Fatalf("Programmed = %+v", condition)
	}

	// Failed native updates keep the previously owned object intact.
	native.failure = errors.New("native control plane unavailable")
	before := resources.Items[0].DeepCopy()
	if _, err := r.Reconcile(ctx, request(first)); !errors.Is(err, native.failure) {
		t.Fatalf("error = %v", err)
	}
	after := &corev1.ConfigMap{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(before), after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed update changed last known good resource")
	}
	native.failure = nil

	sibling := inlinePolicy("conflict", first.Spec.TargetRefs[0])
	sibling.UID, sibling.Spec.Adapter = "sibling-uid", "test-gateway"
	if err := c.Create(ctx, sibling); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, request(first)); err != nil {
		t.Fatal(err)
	}
	if native.removals != 1 || envoy.calls != 0 {
		t.Fatalf("native removals/envoy calls = %d/%d", native.removals, envoy.calls)
	}
	if err := c.List(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if len(resources.Items) != 0 {
		t.Fatal("conflicted native attachment remains")
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(first), got); err != nil {
		t.Fatal(err)
	}
	if condition := findCondition(got.Status.Conditions, security.ConditionProgrammed); condition.Reason != security.ReasonConflicted {
		t.Fatalf("Programmed = %+v", condition)
	}
}

func TestUnsupportedAdapterNeverProgramsAndPreservesExistingResources(t *testing.T) {
	for _, scenario := range []string{"unknown", "inline mask", "snapshot mask", "target", "section"} {
		t.Run(scenario, func(t *testing.T) {
			object := compilingInlinePolicy("policy", target("HTTPRoute", "orders", nil))
			object.UID, object.Spec.Adapter = "owner-uid", "test-gateway"
			existing := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test-orders", Namespace: object.Namespace}, Data: map[string]string{"policy": "last-known-good"}}
			c, native, envoy, r := adapterFixture(t, object, existing)
			switch scenario {
			case "unknown":
				object.Spec.Adapter = "uninstalled"
			case "inline mask":
				native.caps.RequestBodyMutation = false
			case "snapshot mask":
				object.Spec.PolicySource, object.Spec.Request, object.Spec.Response = security.PolicySourcePostgresRef, nil, nil
				object.Spec.PolicyRef = &security.PolicyReference{Name: "banking", Version: new(int32(1))}
				native.caps.ResponseBodyMutation = false
				r.referenceResolver = &effectivepolicy.ReferenceResolver{Repo: resolvedReferenceRepository{snapshot: policy.PolicySnapshot{Version: intPointer(1), Status: policy.StatusActive, Definition: policy.PolicyDefinition{Response: policy.ResponsePolicy{Enabled: true, PII: policy.ActionMask}}}}}
			case "target":
				object.Spec.TargetRefs[0].Kind = "GRPCRoute"
			case "section":
				object.Spec.TargetRefs[0].SectionName = new(gateway.SectionName("rule"))
			}
			if err := c.Update(context.Background(), object); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Reconcile(context.Background(), request(object)); err != nil {
				t.Fatal(err)
			}
			if native.calls != 0 || native.removals != 0 || envoy.calls != 0 {
				t.Fatal("unsupported policy reached a resource mutation")
			}
			got := &security.TSZGuardrailPolicy{}
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(object), got); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{security.ConditionAccepted, security.ConditionProgrammed} {
				if condition := findCondition(got.Status.Conditions, kind); condition.Status != metav1.ConditionFalse || condition.Reason != security.ReasonUnsupportedCapability {
					t.Fatalf("%s = %+v", kind, condition)
				}
			}
			after := &corev1.ConfigMap{}
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(existing), after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(existing.Data, after.Data) {
				t.Fatal("unsupported policy changed last known good resource")
			}
		})
	}
}
