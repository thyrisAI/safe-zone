package policyattach

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	securityv1alpha1 "thyris-sz/api/v1alpha1"
	"thyris-sz/internal/controller"
	"thyris-sz/internal/controller/effectivepolicy"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// TestEnvtestReconcilerPublishesReferenceFailure exercises the real API server,
// generated CRD schema, status subresource and reconciler together. It is kept
// separate from the fast fake-client tests: make test-envtest obtains and sets
// KUBEBUILDER_ASSETS for this test in local development and CI.
func TestEnvtestReconcilerPublishesReferenceFailure(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS is not set; run make test-envtest")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	testEnvironment := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(repoRoot, "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	config, err := testEnvironment.Start()
	if err != nil {
		t.Fatalf("start envtest API server: %v", err)
	}
	t.Cleanup(func() {
		if err := testEnvironment.Stop(); err != nil {
			t.Errorf("stop envtest API server: %v", err)
		}
	})

	scheme, err := controller.NewScheme()
	if err != nil {
		t.Fatal(err)
	}
	kubeClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	version := int32(1)
	object := &securityv1alpha1.TSZGuardrailPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "missing-reference", Namespace: "default"},
		Spec: securityv1alpha1.TSZGuardrailPolicySpec{
			TargetRefs: []gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName(gatewayv1.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{
						Group: gatewayv1.Group("gateway.networking.k8s.io"), Kind: gatewayv1.Kind("HTTPRoute"), Name: gatewayv1.ObjectName("orders"),
					},
				}),
			},
			PolicySource: securityv1alpha1.PolicySourcePostgresRef,
			PolicyRef:    &securityv1alpha1.PolicyReference{Name: "does-not-exist", Version: &version},
			FailurePolicy: securityv1alpha1.FailurePolicySpec{
				Request: securityv1alpha1.FailureModeClosed, Response: securityv1alpha1.FailureModeClosed,
			},
		},
	}
	if err := kubeClient.Create(context.Background(), object); err != nil {
		t.Fatalf("create TSZGuardrailPolicy: %v", err)
	}

	reconciler := NewPolicyAttachmentReconciler(kubeClient, staticTargets{}, selector{}, missingReferenceResolver(), nil, &recordingEnvoy{})
	if _, err := reconciler.Reconcile(context.Background(), request(object)); err == nil {
		t.Fatal("Reconcile() unexpectedly succeeded for a missing policy reference")
	}

	key := client.ObjectKeyFromObject(object)
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := &securityv1alpha1.TSZGuardrailPolicy{}
		if err := kubeClient.Get(context.Background(), key, got); err != nil {
			t.Fatal(err)
		}
		condition := findCondition(got.Status.Conditions, securityv1alpha1.ConditionResolvedRefs)
		if condition.Status == metav1.ConditionFalse && condition.Reason == securityv1alpha1.ReasonPolicyNotFound {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ResolvedRefs = %+v, want False/PolicyNotFound", condition)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func missingReferenceResolver() *effectivepolicy.ReferenceResolver {
	return &effectivepolicy.ReferenceResolver{Repo: missingRepository{}}
}
