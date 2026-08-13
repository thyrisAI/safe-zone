package policyattach

import (
	"context"
	"fmt"
	"reflect"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	securityv1alpha1 "thyris-sz/api/v1alpha1"
	"thyris-sz/internal/controller/effectivepolicy"
	"thyris-sz/internal/controller/envoyresource"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

const targetRefIndex = ".spec.targetRefs"

// PolicyAttachmentReconciler reconciles TSZ policy attachments. Subsequent
// phases add policy resolution, effective-policy compilation, and status
// updates; this phase establishes target resolution and event fan-out.
type PolicyAttachmentReconciler struct {
	client.Client
	targetResolver    TargetResolver
	precedence        PrecedenceSelector
	referenceResolver *effectivepolicy.ReferenceResolver
	effectiveCompiler *effectivepolicy.Compiler
	envoyReconciler   EnvoyReconciler
}

type TargetResolver interface {
	ResolveTargets(context.Context, *securityv1alpha1.TSZGuardrailPolicy) []ResolvedTarget
}
type PrecedenceSelector interface {
	Select([]effectivepolicy.Candidate) (effectivepolicy.Candidate, *effectivepolicy.ConflictError)
}
type EnvoyReconciler interface {
	ReconcileExtensionPolicy(context.Context, *securityv1alpha1.TSZGuardrailPolicy, gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName, envoyresource.EffectivePolicy) (controllerutil.OperationResult, error)
}

func NewPolicyAttachmentReconciler(c client.Client, targets TargetResolver, precedence PrecedenceSelector, references *effectivepolicy.ReferenceResolver, compiler *effectivepolicy.Compiler, envoy EnvoyReconciler) *PolicyAttachmentReconciler {
	return &PolicyAttachmentReconciler{Client: c, targetResolver: targets, precedence: precedence, referenceResolver: references, effectiveCompiler: compiler, envoyReconciler: envoy}
}

// Reconcile resolves targets so target disappearance and section changes are
// observed immediately. Conditions are written by the status phase.
func (r *PolicyAttachmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	policy := &securityv1alpha1.TSZGuardrailPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	resolved := r.targetResolver.ResolveTargets(ctx, policy)
	// A failing reference never changes a last-known-good Envoy policy.
	if policy.Spec.PolicySource == securityv1alpha1.PolicySourcePostgresRef && r.referenceResolver != nil {
		result := r.referenceResolver.Resolve(ctx, policy.Spec.PolicyRef, nil)
		if result.Reason != effectivepolicy.ResolutionResolved {
			r.publishReferenceFailure(ctx, policy, result)
			return ctrl.Result{}, result.Err
		}
	}
	if r.envoyReconciler != nil {
		for _, target := range resolved {
			if target.Err != nil || !target.SectionOK {
				continue
			}
			// Full multi-object conflict selection is driven by the target index in
			// the next reconciliation pass; this candidate preserves whole-policy semantics.
			winner, conflict := r.precedence.Select([]effectivepolicy.Candidate{{ID: string(policy.UID), Kind: target.Kind, Ref: target.Ref}})
			if conflict != nil || winner.ID != string(policy.UID) {
				continue
			}
			_, err := r.envoyReconciler.ReconcileExtensionPolicy(ctx, policy, target.Ref, envoyresource.EffectivePolicy{ProcessingTimeout: policy.Spec.ProcessingTimeoutOrDefault(), FailOpen: policy.Spec.FailOpen()})
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	if err := r.publishTargetResolutionStatus(ctx, policy, resolved); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *PolicyAttachmentReconciler) publishReferenceFailure(ctx context.Context, policy *securityv1alpha1.TSZGuardrailPolicy, result effectivepolicy.ResolutionResult) {
	message := result.Reason
	if result.Err != nil {
		message = result.Err.Error()
	}
	securityv1alpha1.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{Type: securityv1alpha1.ConditionResolvedRefs, Status: metav1.ConditionFalse, Reason: result.Reason, Message: message, ObservedGeneration: policy.Generation})
	_ = r.Status().Update(ctx, policy)
}

func (r *PolicyAttachmentReconciler) publishTargetResolutionStatus(ctx context.Context, policy *securityv1alpha1.TSZGuardrailPolicy, resolved []ResolvedTarget) error {
	original := policy.DeepCopy().Status
	policy.Status.ObservedGeneration = policy.Generation
	securityv1alpha1.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{
		Type: securityv1alpha1.ConditionAccepted, Status: metav1.ConditionTrue,
		Reason: securityv1alpha1.ReasonValid, Message: "policy spec accepted", ObservedGeneration: policy.Generation,
	})

	allResolved := true
	statuses := make([]securityv1alpha1.TargetRefStatus, 0, len(resolved))
	for _, target := range resolved {
		status := securityv1alpha1.TargetRefStatus{TargetRef: target.Ref}
		condition := metav1.Condition{Type: securityv1alpha1.ConditionResolvedRefs, ObservedGeneration: policy.Generation}
		if target.Err != nil || !target.SectionOK {
			allResolved = false
			condition.Status, condition.Reason = metav1.ConditionFalse, securityv1alpha1.ReasonInvalidTarget
			if target.Err != nil {
				condition.Message = target.Err.Error()
			} else {
				condition.Message = "target section does not exist"
			}
		} else {
			condition.Status, condition.Reason, condition.Message = metav1.ConditionTrue, securityv1alpha1.ReasonResolved, "target reference resolved"
		}
		securityv1alpha1.SetStatusCondition(&status.Conditions, condition)
		statuses = append(statuses, status)
	}
	policy.Status.TargetRefStatuses = statuses
	resolvedCondition := metav1.Condition{Type: securityv1alpha1.ConditionResolvedRefs, ObservedGeneration: policy.Generation}
	if allResolved {
		resolvedCondition.Status, resolvedCondition.Reason, resolvedCondition.Message = metav1.ConditionTrue, securityv1alpha1.ReasonResolved, "all target references resolved"
	} else {
		resolvedCondition.Status, resolvedCondition.Reason, resolvedCondition.Message = metav1.ConditionFalse, securityv1alpha1.ReasonInvalidTarget, "one or more target references are invalid"
	}
	securityv1alpha1.SetStatusCondition(&policy.Status.Conditions, resolvedCondition)

	if reflect.DeepEqual(original, policy.Status) {
		return nil
	}
	if err := r.Status().Update(ctx, policy); err != nil {
		return fmt.Errorf("update TSZGuardrailPolicy target resolution status: %w", err)
	}
	return nil
}

// SetupWithManager configures indexed watches for every supported target kind.
func (r *PolicyAttachmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &securityv1alpha1.TSZGuardrailPolicy{}, targetRefIndex, targetRefIndexValues); err != nil {
		return fmt.Errorf("index TSZGuardrailPolicy target refs: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&securityv1alpha1.TSZGuardrailPolicy{}).
		Watches(&gatewayv1.Gateway{}, handler.EnqueueRequestsFromMapFunc(r.policiesTargetingGateway)).
		Watches(&gatewayv1.HTTPRoute{}, handler.EnqueueRequestsFromMapFunc(r.policiesTargetingHTTPRoute)).
		Watches(&gatewayv1.GRPCRoute{}, handler.EnqueueRequestsFromMapFunc(r.policiesTargetingGRPCRoute)).
		Owns(&egv1alpha1.EnvoyExtensionPolicy{}).
		Complete(r)
}

func (r *PolicyAttachmentReconciler) policiesTargetingGateway(ctx context.Context, object client.Object) []reconcile.Request {
	return r.policiesTargeting(ctx, object, "Gateway")
}

func (r *PolicyAttachmentReconciler) policiesTargetingHTTPRoute(ctx context.Context, object client.Object) []reconcile.Request {
	return r.policiesTargeting(ctx, object, "HTTPRoute")
}

func (r *PolicyAttachmentReconciler) policiesTargetingGRPCRoute(ctx context.Context, object client.Object) []reconcile.Request {
	return r.policiesTargeting(ctx, object, "GRPCRoute")
}

func (r *PolicyAttachmentReconciler) policiesTargeting(ctx context.Context, object client.Object, kind string) []reconcile.Request {
	policies := &securityv1alpha1.TSZGuardrailPolicyList{}
	key := targetRefKey(gatewayAPIGroup, kind, object.GetName())
	if err := r.List(ctx, policies, client.InNamespace(object.GetNamespace()), client.MatchingFields{targetRefIndex: key}); err != nil {
		log.FromContext(ctx).Error(err, "list policies targeting Gateway API object", "kind", kind, "namespace", object.GetNamespace(), "name", object.GetName())
		return nil
	}

	requests := make([]reconcile.Request, 0, len(policies.Items))
	for _, policy := range policies.Items {
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: policy.Namespace, Name: policy.Name}})
	}
	return requests
}

func targetRefIndexValues(object client.Object) []string {
	policy, ok := object.(*securityv1alpha1.TSZGuardrailPolicy)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(policy.Spec.TargetRefs))
	for _, ref := range policy.Spec.TargetRefs {
		values = append(values, targetRefKey(string(ref.Group), string(ref.Kind), string(ref.Name)))
	}
	return values
}

func targetRefKey(group, kind, name string) string {
	return group + "/" + kind + "/" + name
}
