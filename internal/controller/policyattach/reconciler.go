package policyattach

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	securityv1beta1 "thyris-sz/api/v1beta1"
	"thyris-sz/internal/controller/capabilities"
	"thyris-sz/internal/controller/controllermetrics"
	"thyris-sz/internal/controller/effectivepolicy"
	"thyris-sz/internal/controller/nativeadapter"
	extprocpolicy "thyris-sz/internal/extproc/policy"

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

const (
	targetRefIndex                = ".spec.targetRefs"
	policyAttachmentFinalizer     = "security.thyris.ai/tsz-guardrail-policy-cleanup"
	activationNotificationPending = "activation committed, notification pending"
)

// +kubebuilder:rbac:groups=security.thyris.ai,resources=tszguardrailpolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=security.thyris.ai,resources=tszguardrailpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways;httproutes;grpcroutes,verbs=get;list;watch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

// PolicyAttachmentReconciler resolves, compiles and programs policy attachments
// through explicitly registered native gateway adapters.
type PolicyAttachmentReconciler struct {
	client.Client
	targetResolver    TargetResolver
	precedence        PrecedenceSelector
	referenceResolver *effectivepolicy.ReferenceResolver
	effectiveCompiler *effectivepolicy.Compiler
	adapters          *nativeadapter.Registry
	ownershipTracker  OwnershipTracker
	routeBindings     RouteBindingStore
}

type TargetResolver interface {
	ResolveTargets(context.Context, *securityv1beta1.TSZGuardrailPolicy) []ResolvedTarget
}
type PrecedenceSelector interface {
	Select([]effectivepolicy.Candidate) (effectivepolicy.Candidate, *effectivepolicy.ConflictError)
}

type OwnershipTracker interface {
	ClaimOwnership(context.Context, string, *string, string, string) error
	ReleaseOwnership(context.Context, string, *string, string, string) error
}
type RouteBindingStore interface {
	UpsertRoutePolicy(context.Context, extprocpolicy.RouteIdentity, extprocpolicy.RoutePolicyBinding) error
	DeleteRoutePolicy(context.Context, extprocpolicy.RouteIdentity) error
}

func NewPolicyAttachmentReconciler(c client.Client, targets TargetResolver, precedence PrecedenceSelector, references *effectivepolicy.ReferenceResolver, compiler *effectivepolicy.Compiler, adapters *nativeadapter.Registry, ownership ...OwnershipTracker) *PolicyAttachmentReconciler {
	reconciler := &PolicyAttachmentReconciler{Client: c, targetResolver: targets, precedence: precedence, referenceResolver: references, effectiveCompiler: compiler, adapters: adapters}
	if len(ownership) > 0 {
		reconciler.ownershipTracker = ownership[0]
	}
	return reconciler
}

func (r *PolicyAttachmentReconciler) WithRoutePolicyBindings(store RouteBindingStore) *PolicyAttachmentReconciler {
	r.routeBindings = store
	return r
}

// Reconcile resolves targets so target disappearance and section changes are
// observed immediately. Conditions are written by the status phase.
func (r *PolicyAttachmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, reconcileErr error) {
	started := time.Now()
	defer func() {
		outcome := "success"
		if reconcileErr != nil {
			outcome = "error"
		} else if result.Requeue || result.RequeueAfter > 0 {
			outcome = "requeue"
		}
		controllermetrics.ObserveReconcile("tszguardrailpolicy", outcome, time.Since(started))
		r.recordManagedResources(ctx)
	}()
	policy := &securityv1beta1.TSZGuardrailPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !policy.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, policy)
	}
	if policy.Spec.PolicySource == securityv1beta1.PolicySourceInline && !controllerutil.ContainsFinalizer(policy, policyAttachmentFinalizer) {
		controllerutil.AddFinalizer(policy, policyAttachmentFinalizer)
		if err := r.Update(ctx, policy); err != nil {
			return ctrl.Result{}, fmt.Errorf("add policy attachment finalizer: %w", err)
		}
	}
	adapter, err := r.adapters.Get(policy.Spec.AdapterName())
	if err != nil {
		return r.unsupportedAdapter(ctx, policy, err)
	}
	descriptor := adapter.Descriptor()
	negotiation, err := capabilities.NegotiateSpec(policy.Spec, descriptor.Capabilities)
	if err != nil {
		return r.unsupportedAdapter(ctx, policy, err)
	}
	for _, ref := range policy.Spec.TargetRefs {
		if err := descriptor.CheckTarget(ref); err != nil {
			return r.unsupportedAdapter(ctx, policy, err)
		}
	}

	resolved := r.targetResolver.ResolveTargets(ctx, policy)
	// A failing reference never changes a last-known-good native policy.
	if policy.Spec.PolicySource == securityv1beta1.PolicySourcePostgresRef && r.referenceResolver != nil {
		result := r.referenceResolver.Resolve(ctx, policy.Spec.PolicyRef, nil)
		if result.Reason != effectivepolicy.ResolutionResolved {
			r.publishReferenceFailure(ctx, policy, result)
			return ctrl.Result{}, result.Err
		}
		negotiation, err = capabilities.NegotiateDefinition(result.Snapshot.Definition, descriptor.Capabilities)
		if err != nil {
			return r.unsupportedAdapter(ctx, policy, err)
		}
		if result.Snapshot.Version != nil {
			version := *result.Snapshot.Version
			policy.Status.PolicyVersion = &version
		}
		policy.Status.EffectivePolicyID = policy.Spec.PolicyRef.Name
		securityv1beta1.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{Type: securityv1beta1.ConditionPolicySynced, Status: metav1.ConditionTrue, Reason: securityv1beta1.ReasonSnapshotActive, Message: "referenced immutable policy snapshot resolved", ObservedGeneration: policy.Generation})
	}
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
			controllermetrics.IncEffectivePolicyConflict()
			if err := adapter.Remove(ctx, policy, target.Ref); err != nil {
				return ctrl.Result{}, err
			}
			r.publishConflict(ctx, policy, conflict)
			continue
		}
		if winner.ID != policyIdentity(policy) {
			continue
		}
		if policy.Spec.PolicySource == securityv1beta1.PolicySourceInline && r.effectiveCompiler != nil {
			dbPolicyName := effectivepolicy.InlinePolicyName(policy.Namespace, policy.Name, targetKey(target))
			definition, err := effectivepolicy.ToPolicyDefinition(policy.Spec, policyScope(target))
			if err != nil {
				return r.lastKnownGoodFailure(ctx, policy, descriptor, err)
			}
			negotiation, err = capabilities.NegotiateDefinition(definition, descriptor.Capabilities)
			if err != nil {
				return r.unsupportedAdapter(ctx, policy, err)
			}
			snapshot, changed, err := r.effectiveCompiler.EnsureCompiledAndActive(ctx, dbPolicyName, definition)
			if err != nil {
				var publishErr *extprocpolicy.ActivationPublishError
				if errors.As(err, &publishErr) {
					controllermetrics.IncPolicyActivation("notification_pending")
					return r.activationNotificationPending(ctx, policy, publishErr)
				}
				controllermetrics.IncPolicyActivation("failed")
				return r.lastKnownGoodFailure(ctx, policy, descriptor, err)
			}
			if changed {
				controllermetrics.IncPolicyActivation("success")
			}
			if isActivationNotificationPending(policy) {
				if err := r.effectiveCompiler.RepublishActivation(ctx, dbPolicyName, definition.Scope.Tenant); err != nil {
					return r.activationNotificationPending(ctx, policy, err)
				}
			}
			if r.ownershipTracker != nil {
				if err := r.ownershipTracker.ClaimOwnership(ctx, dbPolicyName, definition.Scope.Tenant, policy.Namespace, policy.Name); err != nil {
					return r.ownershipUnavailable(ctx, policy, err)
				}
			}
			if snapshot.Version != nil {
				version := *snapshot.Version
				policy.Status.PolicyVersion = &version
			}
			policy.Status.EffectivePolicyID = dbPolicyName
			if r.routeBindings != nil {
				if err := r.routeBindings.UpsertRoutePolicy(ctx, adapter.RouteIdentity(target.Ref, target.Object), extprocpolicy.RoutePolicyBinding{PolicyID: dbPolicyName}); err != nil {
					return r.ownershipUnavailable(ctx, policy, err)
				}
			}
			securityv1beta1.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{Type: securityv1beta1.ConditionPolicySynced, Status: metav1.ConditionTrue, Reason: securityv1beta1.ReasonSnapshotActive, Message: "activation published; per-replica confirmation not yet implemented", ObservedGeneration: policy.Generation})
		}
		_, err = adapter.Reconcile(ctx, policy, target.Ref, nativeadapter.EffectivePolicy{ProcessingTimeout: policy.Spec.ProcessingTimeoutOrDefault(), FailOpen: policy.Spec.FailOpen(), NegotiatedCapabilities: negotiation.Clone()})
		if err != nil {
			return ctrl.Result{}, err
		}
		securityv1beta1.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{Type: securityv1beta1.ConditionProgrammed, Status: metav1.ConditionTrue, Reason: descriptor.ProgrammedReason, Message: descriptor.ResourceKind + " reconciled", ObservedGeneration: policy.Generation})
	}
	if err := r.publishTargetResolutionStatus(ctx, policy, resolved); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *PolicyAttachmentReconciler) recordManagedResources(ctx context.Context) {
	for _, adapter := range r.adapters.All() {
		if count, err := adapter.ManagedResourceCount(ctx); err == nil {
			controllermetrics.SetManagedAdapterResources(adapter.Descriptor().Capabilities.Name, count)
		}
	}
}

func (r *PolicyAttachmentReconciler) candidatesForTarget(ctx context.Context, current *securityv1beta1.TSZGuardrailPolicy, target ResolvedTarget) ([]effectivepolicy.Candidate, error) {
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
		policies := &securityv1beta1.TSZGuardrailPolicyList{}
		if err := r.List(ctx, policies, client.InNamespace(current.Namespace), client.MatchingFields{targetRefIndex: key}); err != nil {
			return nil, fmt.Errorf("list policy candidates: %w", err)
		}
		for index := range policies.Items {
			candidatePolicy := &policies.Items[index]
			if candidatePolicy.Spec.AdapterName() != current.Spec.AdapterName() || !candidatePolicy.DeletionTimestamp.IsZero() {
				continue
			}
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
		return sameSection(ref.SectionName, target.Ref.SectionName)
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

func sameSection(left, right *gatewayv1.SectionName) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
func sectionKey(ref gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName) string {
	if ref.SectionName == nil {
		return ""
	}
	return "/" + string(*ref.SectionName)
}
func policyIdentity(object *securityv1beta1.TSZGuardrailPolicy) string {
	return object.Namespace + "/" + object.Name
}
func (r *PolicyAttachmentReconciler) publishConflict(ctx context.Context, object *securityv1beta1.TSZGuardrailPolicy, conflict *effectivepolicy.ConflictError) {
	before := object.DeepCopy().Status
	others := make([]string, 0, len(conflict.Candidates))
	for _, candidate := range conflict.Candidates {
		if candidate.ID != policyIdentity(object) {
			others = append(others, candidate.ID)
		}
	}
	securityv1beta1.SetStatusCondition(&object.Status.Conditions, metav1.Condition{Type: securityv1beta1.ConditionProgrammed, Status: metav1.ConditionFalse, Reason: securityv1beta1.ReasonConflicted, Message: "conflicts with TSZGuardrailPolicy/" + strings.Join(others, ","), ObservedGeneration: object.Generation})
	if !reflect.DeepEqual(before, object.Status) {
		_ = r.Status().Update(ctx, object)
	}
}

func (r *PolicyAttachmentReconciler) lastKnownGoodFailure(ctx context.Context, object *securityv1beta1.TSZGuardrailPolicy, descriptor nativeadapter.Descriptor, err error) (ctrl.Result, error) {
	securityv1beta1.SetStatusCondition(&object.Status.Conditions, metav1.Condition{Type: securityv1beta1.ConditionPolicySynced, Status: metav1.ConditionFalse, Reason: securityv1beta1.ReasonSnapshotRejected, Message: err.Error(), ObservedGeneration: object.Generation})
	// No native resource operation is attempted here. Any existing child
	// remains the last known good configuration while the update is retried.
	securityv1beta1.SetStatusCondition(&object.Status.Conditions, metav1.Condition{Type: securityv1beta1.ConditionProgrammed, Status: metav1.ConditionTrue, Reason: descriptor.ProgrammedReason, Message: "last known good " + descriptor.ResourceKind + " remains programmed", ObservedGeneration: object.Generation})
	if updateErr := r.Status().Update(ctx, object); updateErr != nil {
		return ctrl.Result{}, fmt.Errorf("update last-known-good status: %w", updateErr)
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *PolicyAttachmentReconciler) activationNotificationPending(ctx context.Context, object *securityv1beta1.TSZGuardrailPolicy, err error) (ctrl.Result, error) {
	// The active snapshot transaction has committed, but ext-proc replicas have
	// not yet been notified. Keep the previously reported version until Redis
	// publish succeeds, then retry only the notification path.
	securityv1beta1.SetStatusCondition(&object.Status.Conditions, metav1.Condition{Type: securityv1beta1.ConditionPolicySynced, Status: metav1.ConditionFalse, Reason: securityv1beta1.ReasonProcessorUnavailable, Message: activationNotificationPending, ObservedGeneration: object.Generation})
	if updateErr := r.Status().Update(ctx, object); updateErr != nil {
		return ctrl.Result{}, fmt.Errorf("update activation notification status: %w", updateErr)
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *PolicyAttachmentReconciler) ownershipUnavailable(ctx context.Context, object *securityv1beta1.TSZGuardrailPolicy, err error) (ctrl.Result, error) {
	securityv1beta1.SetStatusCondition(&object.Status.Conditions, metav1.Condition{Type: securityv1beta1.ConditionPolicySynced, Status: metav1.ConditionFalse, Reason: securityv1beta1.ReasonProcessorUnavailable, Message: fmt.Sprintf("record Inline policy ownership: %v", err), ObservedGeneration: object.Generation})
	if updateErr := r.Status().Update(ctx, object); updateErr != nil {
		return ctrl.Result{}, fmt.Errorf("update ownership status: %w", updateErr)
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func isActivationNotificationPending(object *securityv1beta1.TSZGuardrailPolicy) bool {
	for _, condition := range object.Status.Conditions {
		if condition.Type == securityv1beta1.ConditionPolicySynced && condition.Status == metav1.ConditionFalse && condition.Reason == securityv1beta1.ReasonProcessorUnavailable && condition.Message == activationNotificationPending {
			return true
		}
	}
	return false
}

func (r *PolicyAttachmentReconciler) handleDeletion(ctx context.Context, object *securityv1beta1.TSZGuardrailPolicy) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(object, policyAttachmentFinalizer) {
		return ctrl.Result{}, nil
	}
	if object.Spec.PolicySource == securityv1beta1.PolicySourceInline && r.ownershipTracker != nil {
		for _, ref := range object.Spec.TargetRefs {
			name := effectivepolicy.InlinePolicyName(object.Namespace, object.Name, targetRefKeyFromRef(ref))
			if err := r.ownershipTracker.ReleaseOwnership(ctx, name, nil, object.Namespace, object.Name); err != nil {
				return ctrl.Result{}, fmt.Errorf("release Inline policy ownership: %w", err)
			}
		}
	}
	// Child native resources are deliberately not deleted here: their
	// controller owner reference lets Kubernetes garbage collection remove them.
	controllerutil.RemoveFinalizer(object, policyAttachmentFinalizer)
	if err := r.Update(ctx, object); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove policy attachment finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func targetRefKeyFromRef(ref gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName) string {
	section := ""
	if ref.SectionName != nil {
		section = string(*ref.SectionName)
	}
	return strings.Join([]string{string(ref.Group), string(ref.Kind), string(ref.Name), section}, "/")
}
func targetKey(target ResolvedTarget) string {
	section := ""
	if target.Ref.SectionName != nil {
		section = string(*target.Ref.SectionName)
	}
	return strings.Join([]string{string(target.Ref.Group), target.Kind, string(target.Ref.Name), section}, "/")
}
func policyScope(target ResolvedTarget) extprocpolicy.Scope {
	scope := extprocpolicy.Scope{}
	if target.Kind == "Gateway" {
		scope.Gateway = string(target.Ref.Name)
	} else {
		scope.Route = string(target.Ref.Name)
	}
	return scope
}

func (r *PolicyAttachmentReconciler) publishReferenceFailure(ctx context.Context, policy *securityv1beta1.TSZGuardrailPolicy, result effectivepolicy.ResolutionResult) {
	message := result.Reason
	if result.Err != nil {
		message = result.Err.Error()
	}
	securityv1beta1.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{Type: securityv1beta1.ConditionAccepted, Status: metav1.ConditionTrue, Reason: securityv1beta1.ReasonValid, Message: "policy spec accepted", ObservedGeneration: policy.Generation})
	securityv1beta1.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{Type: securityv1beta1.ConditionResolvedRefs, Status: metav1.ConditionFalse, Reason: result.Reason, Message: message, ObservedGeneration: policy.Generation})
	_ = r.Status().Update(ctx, policy)
}

func (r *PolicyAttachmentReconciler) publishTargetResolutionStatus(ctx context.Context, policy *securityv1beta1.TSZGuardrailPolicy, resolved []ResolvedTarget) error {
	original := policy.DeepCopy().Status
	policy.Status.ObservedGeneration = policy.Generation
	securityv1beta1.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{
		Type: securityv1beta1.ConditionAccepted, Status: metav1.ConditionTrue,
		Reason: securityv1beta1.ReasonValid, Message: "policy spec accepted", ObservedGeneration: policy.Generation,
	})

	allResolved := true
	statuses := make([]securityv1beta1.TargetRefStatus, 0, len(resolved))
	for _, target := range resolved {
		status := securityv1beta1.TargetRefStatus{TargetRef: target.Ref}
		condition := metav1.Condition{Type: securityv1beta1.ConditionResolvedRefs, ObservedGeneration: policy.Generation}
		if target.Err != nil || !target.SectionOK {
			allResolved = false
			condition.Status, condition.Reason = metav1.ConditionFalse, securityv1beta1.ReasonInvalidTarget
			if target.Err != nil {
				condition.Message = target.Err.Error()
			} else {
				condition.Message = "target section does not exist"
			}
		} else {
			condition.Status, condition.Reason, condition.Message = metav1.ConditionTrue, securityv1beta1.ReasonResolved, "target reference resolved"
		}
		securityv1beta1.SetStatusCondition(&status.Conditions, condition)
		statuses = append(statuses, status)
	}
	policy.Status.TargetRefStatuses = statuses
	resolvedCondition := metav1.Condition{Type: securityv1beta1.ConditionResolvedRefs, ObservedGeneration: policy.Generation}
	if allResolved {
		resolvedCondition.Status, resolvedCondition.Reason, resolvedCondition.Message = metav1.ConditionTrue, securityv1beta1.ReasonResolved, "all target references resolved"
	} else {
		resolvedCondition.Status, resolvedCondition.Reason, resolvedCondition.Message = metav1.ConditionFalse, securityv1beta1.ReasonInvalidTarget, "one or more target references are invalid"
	}
	securityv1beta1.SetStatusCondition(&policy.Status.Conditions, resolvedCondition)

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
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &securityv1beta1.TSZGuardrailPolicy{}, targetRefIndex, targetRefIndexValues); err != nil {
		return fmt.Errorf("index TSZGuardrailPolicy target refs: %w", err)
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&securityv1beta1.TSZGuardrailPolicy{}).
		// A policy addition, edit, or deletion can turn every sibling targeting
		// the same Gateway API object into (or out of) a conflict.
		Watches(&securityv1beta1.TSZGuardrailPolicy{}, handler.EnqueueRequestsFromMapFunc(r.policiesSharingTargetRefs)).
		Watches(&gatewayv1.Gateway{}, handler.EnqueueRequestsFromMapFunc(r.policiesTargetingGateway)).
		Watches(&gatewayv1.HTTPRoute{}, handler.EnqueueRequestsFromMapFunc(r.policiesTargetingHTTPRoute)).
		Watches(&gatewayv1.GRPCRoute{}, handler.EnqueueRequestsFromMapFunc(r.policiesTargetingGRPCRoute))
	for _, adapter := range r.adapters.All() {
		for _, object := range adapter.OwnedResources() {
			builder = builder.Owns(object)
		}
	}
	return builder.Complete(r)
}

func (r *PolicyAttachmentReconciler) policiesSharingTargetRefs(ctx context.Context, object client.Object) []reconcile.Request {
	policy, ok := object.(*securityv1beta1.TSZGuardrailPolicy)
	if !ok {
		return nil
	}
	requests := map[types.NamespacedName]struct{}{}
	for _, ref := range policy.Spec.TargetRefs {
		policies := &securityv1beta1.TSZGuardrailPolicyList{}
		key := targetRefKey(string(ref.Group), string(ref.Kind), string(ref.Name))
		if err := r.List(ctx, policies, client.InNamespace(policy.Namespace), client.MatchingFields{targetRefIndex: key}); err != nil {
			log.FromContext(ctx).Error(err, "list policies sharing target reference", "namespace", policy.Namespace, "name", policy.Name)
			continue
		}
		for _, candidate := range policies.Items {
			requests[types.NamespacedName{Namespace: candidate.Namespace, Name: candidate.Name}] = struct{}{}
		}
	}
	result := make([]reconcile.Request, 0, len(requests))
	for name := range requests {
		result = append(result, reconcile.Request{NamespacedName: name})
	}
	return result
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
	policies := &securityv1beta1.TSZGuardrailPolicyList{}
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
	policy, ok := object.(*securityv1beta1.TSZGuardrailPolicy)
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

func (r *PolicyAttachmentReconciler) unsupportedAdapter(ctx context.Context, policy *securityv1beta1.TSZGuardrailPolicy, err error) (ctrl.Result, error) {
	before := policy.DeepCopy().Status
	policy.Status.ObservedGeneration = policy.Generation
	for _, kind := range []string{securityv1beta1.ConditionAccepted, securityv1beta1.ConditionProgrammed} {
		securityv1beta1.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{Type: kind, Status: metav1.ConditionFalse, Reason: securityv1beta1.ReasonUnsupportedCapability, Message: err.Error(), ObservedGeneration: policy.Generation})
	}
	if reflect.DeepEqual(before, policy.Status) {
		return ctrl.Result{}, nil
	}
	if updateErr := r.Status().Update(ctx, policy); updateErr != nil {
		return ctrl.Result{}, fmt.Errorf("update unsupported adapter status: %w", updateErr)
	}
	return ctrl.Result{}, nil
}
