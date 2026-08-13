package envoyresource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	securityv1alpha1 "thyris-sz/api/v1alpha1"
	"thyris-sz/internal/controller/policyattach"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const managedByLabel = "security.thyris.ai/managed-by"

// EffectivePolicy contains the data-plane settings already validated against
// the effective immutable snapshot.
type EffectivePolicy struct {
	ProcessingTimeout time.Duration
	FailOpen          bool
}

// EnvoyResourceReconciler owns generated Envoy Gateway resources.
type EnvoyResourceReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

// ReconcileExtensionPolicy creates or updates exactly one deterministic
// EnvoyExtensionPolicy for a resolved target and assigns its CRD owner.
func (r *EnvoyResourceReconciler) ReconcileExtensionPolicy(ctx context.Context, owner *securityv1alpha1.TSZGuardrailPolicy, target policyattach.ResolvedTarget, effective EffectivePolicy) (controllerutil.OperationResult, error) {
	if r == nil || r.Client == nil || r.Scheme == nil {
		return controllerutil.OperationResultNone, fmt.Errorf("envoy resource reconciler client and scheme are required")
	}
	desired := BuildEnvoyExtensionPolicy(owner, target, effective)
	existing := &egv1alpha1.EnvoyExtensionPolicy{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	operation, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.Labels = desired.Labels
		existing.Spec = desired.Spec
		return ctrl.SetControllerReference(owner, existing, r.Scheme)
	})
	if err != nil {
		return operation, fmt.Errorf("reconcile EnvoyExtensionPolicy %s/%s: %w", desired.Namespace, desired.Name, err)
	}
	return operation, nil
}

// BuildEnvoyExtensionPolicy creates the native equivalent of the manual
// preview manifest. The manual file remains supported for preview installs.
func BuildEnvoyExtensionPolicy(owner *securityv1alpha1.TSZGuardrailPolicy, target policyattach.ResolvedTarget, effective EffectivePolicy) *egv1alpha1.EnvoyExtensionPolicy {
	timeout := effective.ProcessingTimeout
	if timeout <= 0 {
		timeout = owner.Spec.ProcessingTimeoutOrDefault()
	}
	failOpen := effective.FailOpen
	messageTimeout := gatewayv1.Duration(timeout.String())
	group, kind, port := gatewayv1.Group(""), gatewayv1.Kind("Service"), gatewayv1.PortNumber(9002)
	return &egv1alpha1.EnvoyExtensionPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: DeterministicName(target), Namespace: owner.Namespace, Labels: map[string]string{managedByLabel: "tsz-controller"}},
		Spec: egv1alpha1.EnvoyExtensionPolicySpec{
			PolicyTargetReferences: egv1alpha1.PolicyTargetReferences{TargetRefs: []gatewayv1.LocalPolicyTargetReferenceWithSectionName{gatewayv1.LocalPolicyTargetReferenceWithSectionName(target.Ref)}},
			ExtProc: []egv1alpha1.ExtProc{{
				BackendCluster: egv1alpha1.BackendCluster{BackendRefs: []egv1alpha1.BackendRef{{BackendObjectReference: gatewayv1.BackendObjectReference{Group: &group, Kind: &kind, Name: "tsz-ext-proc", Port: &port}}}},
				MessageTimeout: &messageTimeout,
				FailOpen:       &failOpen,
				ProcessingMode: &egv1alpha1.ExtProcProcessingMode{Request: &egv1alpha1.ProcessingModeOptions{Body: bodyMode()}},
				Metadata:       &egv1alpha1.ExtProcMetadata{WritableNamespaces: []string{"io.thyris.tsz"}},
			}},
		},
	}
}

func bodyMode() *egv1alpha1.ExtProcBodyProcessingMode {
	mode := egv1alpha1.BufferedExtProcBodyProcessingMode
	return &mode
}

// DeterministicName is target-only by design: renaming the owning CRD cannot
// duplicate the Envoy policy for the same attachment target.
func DeterministicName(target policyattach.ResolvedTarget) string {
	section := ""
	if target.Ref.SectionName != nil {
		section = string(*target.Ref.SectionName)
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s/%s", target.Ref.Group, target.Ref.Kind, target.Ref.Name, section)))
	return "tsz-guardrail-" + hex.EncodeToString(digest[:4])
}
