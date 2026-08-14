package policyattach

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	securityv1alpha1 "thyris-sz/api/v1alpha1"
	"thyris-sz/internal/controller/capabilities"
	"thyris-sz/internal/controller/effectivepolicy"
	"thyris-sz/internal/controller/envoyresource"
	"thyris-sz/internal/extproc/policy"

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
	if err := capabilities.CheckCapabilities(policy.Spec, capabilities.EnvoyGatewayCapabilities); err != nil {
		securityv1alpha1.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{Type: securityv1alpha1.ConditionProgrammed, Status: metav1.ConditionFalse, Reason: securityv1alpha1.ReasonUnsupportedCapability, Message: err.Error(), ObservedGeneration: policy.Generation})
		if updateErr := r.Status().Update(ctx, policy); updateErr != nil {
			return ctrl.Result{}, fmt.Errorf("update unsupported capability status: %w", updateErr)
		}
		return ctrl.Result{}, nil
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
			candidates, err := r.candidatesForTarget(ctx, policy, target)
			if err != nil {
				return ctrl.Result{}, err
			}
			winner, conflict := r.precedence.Select(candidates)
			if conflict != nil {
				r.publishConflict(ctx, policy, conflict)
				continue
			}
			if winner.ID != policyIdentity(policy) {
				continue
			}
			if policy.Spec.PolicySource == securityv1alpha1.PolicySourceInline && r.effectiveCompiler != nil {
				definition, err := effectivepolicy.ToPolicyDefinition(policy.Spec, policyScope(target))
				if err != nil {
					return r.lastKnownGoodFailure(ctx, policy, err)
				}
				_, _, err = r.effectiveCompiler.EnsureCompiledAndActive(ctx, effectivepolicy.InlinePolicyName(policy.Namespace, policy.Name, targetKey(target)), definition)
				if err != nil {
					return r.lastKnownGoodFailure(ctx, policy, err)
				}
				securityv1alpha1.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{Type: securityv1alpha1.ConditionPolicySynced, Status: metav1.ConditionTrue, Reason: securityv1alpha1.ReasonSnapshotActive, Message: "activation published; per-replica confirmation not yet implemented", ObservedGeneration: policy.Generation})
			}
			_, err = r.envoyReconciler.ReconcileExtensionPolicy(ctx, policy, target.Ref, envoyresource.EffectivePolicy{ProcessingTimeout: policy.Spec.ProcessingTimeoutOrDefault(), FailOpen: policy.Spec.FailOpen()})
			if err != nil {
				return ctrl.Result{}, err
			}
			securityv1alpha1.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{Type: securityv1alpha1.ConditionProgrammed, Status: metav1.ConditionTrue, Reason: securityv1alpha1.ReasonExtProcConfigured, Message: "EnvoyExtensionPolicy reconciled", ObservedGeneration: policy.Generation})
		}
	}
	if err := r.publishTargetResolutionStatus(ctx, policy, resolved); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *PolicyAttachmentReconciler) candidatesForTarget(ctx context.Context, current *securityv1alpha1.TSZGuardrailPolicy, target ResolvedTarget) ([]effectivepolicy.Candidate, error) {
	keys := []string{targetRefKey(string(target.Ref.Group), target.Kind, string(target.Ref.Name))}
	listenerNames := map[string]struct{}{}
	switch object := target.Object.(type) {
	case *gatewayv1.HTTPRoute:
		for _, parent := range object.Spec.ParentRefs {
			if parent.Name == "" {
				continue
			}
			keys = append(keys, targetRefKey(gatewayAPIGroup, "Gateway", string(parent.Name)))
			if parent.SectionName != nil {
				listenerNames[string(*parent.SectionName)] = struct{}{}
			}
		}
	case *gatewayv1.GRPCRoute:
		for _, parent := range object.Spec.ParentRefs {
			if parent.Name == "" {
				continue
			}
			keys = append(keys, targetRefKey(gatewayAPIGroup, "Gateway", string(parent.Name)))
			if parent.SectionName != nil {
				listenerNames[string(*parent.SectionName)] = struct{}{}
			}
		}
	}
	seen := map[string]struct{}{}
	candidates := []effectivepolicy.Candidate{}
	for _, key := range keys {
		policies := &securityv1alpha1.TSZGuardrailPolicyList{}
		if err := r.List(ctx, policies, client.InNamespace(current.Namespace), client.MatchingFields{targetRefIndex: key}); err != nil {
			return nil, fmt.Errorf("list policy candidates: %w", err)
		}
		for index := range policies.Items {
			candidatePolicy := &policies.Items[index]
			for _, ref := range candidatePolicy.Spec.TargetRefs {
				if !candidateRefApplies(target, ref, listenerNames) {
					continue
				}
				id := policyIdentity(candidatePolicy) + "#" + targetRefKey(string(ref.Group), string(ref.Kind), string(ref.Name)) + sectionKey(ref)
				if _, exists := seen[id]; exists {
					continue
				}
				seen[id] = struct{}{}
				candidates = append(candidates, effectivepolicy.Candidate{ID: policyIdentity(candidatePolicy), Kind: string(ref.Kind), Ref: ref})
			}
		}
	}
	return candidates, nil
}

func candidateRefApplies(target ResolvedTarget, ref gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName, listenerNames map[string]struct{}) bool {
	if string(ref.Group) != gatewayAPIGroup {
		return false
	}
	if string(ref.Kind) == target.Kind && ref.Name == target.Ref.Name {
		return true
	}
	if (target.Kind == "HTTPRoute" || target.Kind == "GRPCRoute") && string(ref.Kind) == "Gateway" {
		if ref.SectionName == nil {
			return true
		}
		_, found := listenerNames[string(*ref.SectionName)]
		return found
	}
	return false
}
func sectionKey(ref gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName) string {
	if ref.SectionName == nil {
		return ""
	}
	return "/" + string(*ref.SectionName)
}
func policyIdentity(object *securityv1alpha1.TSZGuardrailPolicy) string {
	return object.Namespace + "/" + object.Name
}
func (r *PolicyAttachmentReconciler) publishConflict(ctx context.Context, object *securityv1alpha1.TSZGuardrailPolicy, conflict *effectivepolicy.ConflictError) {
	others := make([]string, 0, len(conflict.Candidates))
	for _, candidate := range conflict.Candidates {
		if candidate.ID != policyIdentity(object) {
			others = append(others, candidate.ID)
		}
	}
	securityv1alpha1.SetStatusCondition(&object.Status.Conditions, metav1.Condition{Type: securityv1alpha1.ConditionProgrammed, Status: metav1.ConditionFalse, Reason: securityv1alpha1.ReasonConflicted, Message: "conflicts with TSZGuardrailPolicy/" + strings.Join(others, ","), ObservedGeneration: object.Generation})
	_ = r.Status().Update(ctx, object)
}

func (r *PolicyAttachmentReconciler) lastKnownGoodFailure(ctx context.Context, object *securityv1alpha1.TSZGuardrailPolicy, err error) (ctrl.Result, error) {
	securityv1alpha1.SetStatusCondition(&object.Status.Conditions, metav1.Condition{Type: securityv1alpha1.ConditionPolicySynced, Status: metav1.ConditionFalse, Reason: securityv1alpha1.ReasonSnapshotRejected, Message: err.Error(), ObservedGeneration: object.Generation})
	// No EnvoyExtensionPolicy operation is attempted here. Any existing child
	// remains the last known good configuration while the update is retried.
	securityv1alpha1.SetStatusCondition(&object.Status.Conditions, metav1.Condition{Type: securityv1alpha1.ConditionProgrammed, Status: metav1.ConditionTrue, Reason: securityv1alpha1.ReasonExtProcConfigured, Message: "last known good EnvoyExtensionPolicy remains programmed", ObservedGeneration: object.Generation})
	if updateErr := r.Status().Update(ctx, object); updateErr != nil {
		return ctrl.Result{}, fmt.Errorf("update last-known-good status: %w", updateErr)
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}
func targetKey(target ResolvedTarget) string {
	section := ""
	if target.Ref.SectionName != nil {
		section = string(*target.Ref.SectionName)
	}
	return strings.Join([]string{string(target.Ref.Group), target.Kind, string(target.Ref.Name), section}, "/")
}
func policyScope(target ResolvedTarget) policy.Scope {
	scope := policy.Scope{}
	if target.Kind == "Gateway" {
		scope.Gateway = string(target.Ref.Name)
	} else {
		scope.Route = string(target.Ref.Name)
	}
	return scope
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
