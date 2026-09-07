package envoyresource_test

import (
	"context"
	"testing"

	alpha "thyris-sz/api/v1alpha1"
	beta "thyris-sz/api/v1beta1"
	"thyris-sz/internal/controller"
	"thyris-sz/internal/controller/envoyresource"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestBetaControllerRetainsAlphaOwnedEnvoyResource(t *testing.T) {
	scheme, err := controller.NewScheme()
	if err != nil {
		t.Fatal(err)
	}
	owner := &beta.TSZGuardrailPolicy{ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: "apps", UID: "original-policy-uid"}}
	target := gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{
			Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Name: "orders",
		},
	}
	existing := envoyresource.BuildEnvoyExtensionPolicy(owner, target, envoyresource.EffectivePolicy{})
	existing.UID = "original-extension-policy-uid"
	legacyOwner := &alpha.TSZGuardrailPolicy{ObjectMeta: owner.ObjectMeta}
	if err := ctrl.SetControllerReference(legacyOwner, existing, scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	r := &envoyresource.EnvoyResourceReconciler{Client: c, Scheme: scheme}
	if _, err := r.ReconcileExtensionPolicy(context.Background(), owner, target, envoyresource.EffectivePolicy{}); err != nil {
		t.Fatal(err)
	}
	got := &egv1alpha1.EnvoyExtensionPolicy{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(existing), got); err != nil {
		t.Fatal(err)
	}
	if got.UID != existing.UID || got.Name != existing.Name || len(got.OwnerReferences) != 1 {
		t.Fatal("version upgrade recreated the Envoy attachment or duplicated its owner")
	}
	ref := got.OwnerReferences[0]
	if ref.UID != owner.UID || ref.APIVersion != beta.GroupVersion.String() {
		t.Fatalf("owner = %+v", ref)
	}
	// An alpha-based controller can regain ownership on workload rollback.
	if err := ctrl.SetControllerReference(legacyOwner, got, scheme); err != nil {
		t.Fatal(err)
	}
	if len(got.OwnerReferences) != 1 || got.OwnerReferences[0].APIVersion != alpha.GroupVersion.String() {
		t.Fatal("alpha rollback duplicated or lost ownership")
	}
}

func TestNativeRemovalOnlyDeletesControllerOwnedResources(t *testing.T) {
	for _, scenario := range []string{"owned", "foreign", "unowned"} {
		t.Run(scenario, func(t *testing.T) {
			scheme, err := controller.NewScheme()
			if err != nil {
				t.Fatal(err)
			}
			owner := &beta.TSZGuardrailPolicy{ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: "apps", UID: "owner"}}
			target := gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName{LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Name: "orders"}}
			existing := envoyresource.BuildEnvoyExtensionPolicy(owner, target, envoyresource.EffectivePolicy{})
			existing.UID = "resource-uid"
			if scenario != "unowned" {
				controllerOwner := owner.DeepCopy()
				if scenario == "foreign" {
					controllerOwner.UID = "another-owner"
				}
				if err := ctrl.SetControllerReference(controllerOwner, existing, scheme); err != nil {
					t.Fatal(err)
				}
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
			r := &envoyresource.EnvoyResourceReconciler{Client: c, Scheme: scheme}
			if err := r.Remove(context.Background(), owner, target); err != nil {
				t.Fatal(err)
			}
			remaining := &egv1alpha1.EnvoyExtensionPolicyList{}
			if err := c.List(context.Background(), remaining); err != nil {
				t.Fatal(err)
			}
			want := 1
			if scenario == "owned" {
				want = 0
			}
			if len(remaining.Items) != want {
				t.Fatalf("remaining resources = %d, want %d", len(remaining.Items), want)
			}
			if err := r.Remove(context.Background(), owner, target); err != nil {
				t.Fatalf("idempotent removal: %v", err)
			}
		})
	}
}
