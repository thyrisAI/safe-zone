package policyattach

import (
	"context"
	"fmt"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	securityv1alpha1 "thyris-sz/api/v1alpha1"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const targetRefIndex = ".spec.targetRefs"

// PolicyAttachmentReconciler reconciles TSZ policy attachments. Subsequent
// phases add policy resolution, effective-policy compilation, and status
// updates; this phase establishes target resolution and event fan-out.
type PolicyAttachmentReconciler struct {
	client.Client
	Resolver *Resolver
}

// Reconcile resolves targets so target disappearance and section changes are
// observed immediately. Conditions are written by the status phase.
func (r *PolicyAttachmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	policy := &securityv1alpha1.TSZGuardrailPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	r.resolver().ResolveTargets(ctx, policy)
	return ctrl.Result{}, nil
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

func (r *PolicyAttachmentReconciler) resolver() *Resolver {
	if r.Resolver != nil {
		return r.Resolver
	}
	return &Resolver{Client: r.Client}
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
