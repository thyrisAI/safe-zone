// Package agentgatewayresource reconciles native agentgateway ExtProc policy
// attachments for the BYG controller.
package agentgatewayresource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	securityv1beta1 "thyris-sz/api/v1beta1"
	"thyris-sz/internal/controller/nativeadapter"
	extprocpolicy "thyris-sz/internal/extproc/policy"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

const managedByLabel = "security.thyris.ai/managed-by"

var (
	policyGVK     = schema.GroupVersionKind{Group: "agentgateway.dev", Version: "v1alpha1", Kind: "AgentgatewayPolicy"}
	policyListGVK = schema.GroupVersionKind{Group: "agentgateway.dev", Version: "v1alpha1", Kind: "AgentgatewayPolicyList"}
)

// Reconciler owns generated AgentgatewayPolicy resources. Unstructured objects
// keep TSZ independent of agentgateway's controller implementation while the
// API remains validated by the installed agentgateway 1.5 CRD.
type Reconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

func newPolicy() *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(policyGVK)
	return object
}

func newPolicyList() *unstructured.UnstructuredList {
	objects := &unstructured.UnstructuredList{}
	objects.SetGroupVersionKind(policyListGVK)
	return objects
}

// Reconcile creates or updates the route-local AgentgatewayPolicy and makes the
// TSZ policy its controller owner.
func (r *Reconciler) Reconcile(ctx context.Context, owner *securityv1beta1.TSZGuardrailPolicy, target gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName, effective nativeadapter.EffectivePolicy) (controllerutil.OperationResult, error) {
	if r == nil || r.Client == nil || r.Scheme == nil {
		return controllerutil.OperationResultNone, fmt.Errorf("agentgateway resource reconciler client and scheme are required")
	}
	identity, err := r.resolveRouteIdentity(ctx, owner.Namespace, target)
	if err != nil {
		return controllerutil.OperationResultNone, err
	}
	desired := BuildAgentgatewayPolicy(owner, target, effective, identity)
	existing := newPolicy()
	existing.SetName(desired.GetName())
	existing.SetNamespace(desired.GetNamespace())
	operation, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.SetLabels(desired.GetLabels())
		existing.Object["spec"] = runtime.DeepCopyJSONValue(desired.Object["spec"])
		return ctrl.SetControllerReference(owner, existing, r.Scheme)
	})
	if err != nil {
		return operation, fmt.Errorf("reconcile AgentgatewayPolicy %s/%s: %w", desired.GetNamespace(), desired.GetName(), err)
	}
	return operation, nil
}

// BuildAgentgatewayPolicy returns an agentgateway 1.5 compatible buffered
// ExtProc attachment. Route identity is sent as trusted ExtProc attributes so
// the shared native policy resolver never depends on client supplied headers.
func BuildAgentgatewayPolicy(owner *securityv1beta1.TSZGuardrailPolicy, target gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName, effective nativeadapter.EffectivePolicy, identity extprocpolicy.RouteIdentity) *unstructured.Unstructured {
	object := newPolicy()
	object.SetName(DeterministicName(target))
	object.SetNamespace(owner.Namespace)
	object.SetLabels(map[string]string{managedByLabel: "tsz-controller"})

	targetRef := map[string]any{
		"group": string(target.Group),
		"kind":  string(target.Kind),
		"name":  string(target.Name),
	}
	if target.SectionName != nil {
		targetRef["sectionName"] = string(*target.SectionName)
	}
	attributes := map[string]any{}
	if identity.Gateway != "" {
		attributes["xds.gateway_name"] = strconv.Quote(identity.Gateway)
	}
	if identity.Listener != "" {
		attributes["xds.listener_name"] = strconv.Quote(identity.Listener)
	}
	if identity.Route != "" {
		attributes["xds.route_name"] = strconv.Quote(identity.Route)
	}
	if identity.Rule != "" {
		attributes["xds.route_rule_name"] = strconv.Quote(identity.Rule)
	}
	failureMode := "FailClosed"
	// agentgateway exposes one mode for both directions. Capability negotiation
	// rejects mixed modes; require both here as a defense against direct callers.
	if effective.RequestFailOpen && effective.ResponseFailOpen {
		failureMode = "FailOpen"
	}
	extProc := map[string]any{
		"backendRef":  map[string]any{"name": "tsz-ext-proc", "port": int64(9002)},
		"failureMode": failureMode,
		"processingOptions": map[string]any{
			"requestHeaderMode":   "Send",
			"responseHeaderMode":  "Send",
			"requestBodyMode":     "Buffered",
			"responseBodyMode":    "Buffered",
			"requestTrailerMode":  "Skip",
			"responseTrailerMode": "Skip",
			"allowModeOverride":   false,
		},
	}
	if len(attributes) > 0 {
		extProc["requestAttributes"] = attributes
	}
	object.Object["spec"] = map[string]any{
		"targetRefs": []any{targetRef},
		"traffic":    map[string]any{"extProc": extProc},
	}
	return object
}

func (r *Reconciler) resolveRouteIdentity(ctx context.Context, namespace string, ref gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName) (extprocpolicy.RouteIdentity, error) {
	key := client.ObjectKey{Namespace: namespace, Name: string(ref.Name)}
	switch string(ref.Kind) {
	case "Gateway":
		gateway := &gatewayv1.Gateway{}
		if err := r.Client.Get(ctx, key, gateway); err != nil {
			return extprocpolicy.RouteIdentity{}, fmt.Errorf("resolve agentgateway Gateway identity %s: %w", key, err)
		}
		return r.RouteIdentity(ref, gateway), nil
	case "HTTPRoute":
		route := &gatewayv1.HTTPRoute{}
		if err := r.Client.Get(ctx, key, route); err != nil {
			return extprocpolicy.RouteIdentity{}, fmt.Errorf("resolve agentgateway HTTPRoute identity %s: %w", key, err)
		}
		return r.RouteIdentity(ref, route), nil
	default:
		return extprocpolicy.RouteIdentity{}, fmt.Errorf("unsupported agentgateway target kind %q", ref.Kind)
	}
}

func (r *Reconciler) Remove(ctx context.Context, owner *securityv1beta1.TSZGuardrailPolicy, ref gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName) error {
	resource := newPolicy()
	key := client.ObjectKey{Namespace: owner.Namespace, Name: DeterministicName(ref)}
	if err := r.Client.Get(ctx, key, resource); err != nil {
		return client.IgnoreNotFound(err)
	}
	reference := metav1.GetControllerOf(resource)
	if reference == nil || reference.UID != owner.UID {
		return nil
	}
	if err := r.Client.Delete(ctx, resource, client.Preconditions{UID: ptr(resource.GetUID()), ResourceVersion: ptr(resource.GetResourceVersion())}); err != nil {
		return fmt.Errorf("delete conflicted AgentgatewayPolicy %s: %w", key, err)
	}
	return nil
}

func ptr[T any](value T) *T { return &value }

func (r *Reconciler) OwnedResources() []client.Object { return []client.Object{newPolicy()} }

func (r *Reconciler) ManagedResourceCount(ctx context.Context) (int, error) {
	resources := newPolicyList()
	if err := r.Client.List(ctx, resources, client.MatchingLabels{managedByLabel: "tsz-controller"}); err != nil {
		return 0, err
	}
	return len(resources.Items), nil
}

func (r *Reconciler) RouteIdentity(ref gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName, object client.Object) extprocpolicy.RouteIdentity {
	identity := extprocpolicy.RouteIdentity{}
	if string(ref.Kind) == "Gateway" {
		identity.Gateway = string(ref.Name)
		if ref.SectionName != nil {
			identity.Listener = string(*ref.SectionName)
		}
		return identity
	}
	identity.Route = string(ref.Name)
	if route, ok := object.(*gatewayv1.HTTPRoute); ok && len(route.Spec.ParentRefs) > 0 {
		identity.Gateway = string(route.Spec.ParentRefs[0].Name)
		if route.Spec.ParentRefs[0].SectionName != nil {
			identity.Listener = string(*route.Spec.ParentRefs[0].SectionName)
		}
		if ref.SectionName != nil {
			identity.Rule = string(*ref.SectionName)
		}
	}
	return identity
}

// DeterministicName is target based so renaming the owning TSZ policy cannot
// duplicate an attachment for the same agentgateway target.
func DeterministicName(target gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName) string {
	section := ""
	if target.SectionName != nil {
		section = string(*target.SectionName)
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("agentgateway/%s/%s/%s/%s", target.Group, target.Kind, target.Name, section)))
	return "tsz-guardrail-" + hex.EncodeToString(digest[:4])
}
