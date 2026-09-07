package envoyresource

import (
	"context"
	"fmt"
	"strconv"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	securityv1beta1 "thyris-sz/api/v1beta1"
	"thyris-sz/internal/controller/capabilities"
	"thyris-sz/internal/controller/nativeadapter"
	extprocpolicy "thyris-sz/internal/extproc/policy"
)

// +kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=envoyextensionpolicies,verbs=get;list;watch;create;update;patch;delete

var _ nativeadapter.Adapter = (*EnvoyResourceReconciler)(nil)

func (r *EnvoyResourceReconciler) Descriptor() nativeadapter.Descriptor {
	return nativeadapter.Descriptor{
		Capabilities: capabilities.EnvoyGatewayCapabilities,
		Targets: []nativeadapter.TargetCapability{
			{Group: gatewayv1.GroupName, Kind: "Gateway", SectionName: true},
			{Group: gatewayv1.GroupName, Kind: "HTTPRoute", SectionName: true},
			{Group: gatewayv1.GroupName, Kind: "GRPCRoute", SectionName: true},
		},
		ResourceKind:     "EnvoyExtensionPolicy",
		ProgrammedReason: securityv1beta1.ReasonExtProcConfigured,
	}
}

func (r *EnvoyResourceReconciler) Reconcile(ctx context.Context, owner *securityv1beta1.TSZGuardrailPolicy, ref gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName, effective nativeadapter.EffectivePolicy) (controllerutil.OperationResult, error) {
	return r.ReconcileExtensionPolicy(ctx, owner, ref, effective)
}

func (r *EnvoyResourceReconciler) OwnedResources() []client.Object {
	return []client.Object{&egv1alpha1.EnvoyExtensionPolicy{}}
}

func (r *EnvoyResourceReconciler) ManagedResourceCount(ctx context.Context) (int, error) {
	resources := &egv1alpha1.EnvoyExtensionPolicyList{}
	if err := r.Client.List(ctx, resources, client.MatchingLabels{managedByLabel: "tsz-controller"}); err != nil {
		return 0, err
	}
	return len(resources.Items), nil
}

func (r *EnvoyResourceReconciler) Remove(ctx context.Context, owner *securityv1beta1.TSZGuardrailPolicy, ref gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName) error {
	resource := &egv1alpha1.EnvoyExtensionPolicy{}
	key := client.ObjectKey{Namespace: owner.Namespace, Name: DeterministicName(ref)}
	if err := r.Client.Get(ctx, key, resource); err != nil {
		return client.IgnoreNotFound(err)
	}
	reference := metav1.GetControllerOf(resource)
	if reference == nil || reference.UID != owner.UID {
		return nil
	}
	if err := r.Client.Delete(ctx, resource, client.Preconditions{UID: &resource.UID, ResourceVersion: &resource.ResourceVersion}); err != nil {
		return fmt.Errorf("delete conflicted EnvoyExtensionPolicy %s: %w", key, err)
	}
	return nil
}

func (r *EnvoyResourceReconciler) RouteIdentity(ref gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName, object client.Object) extprocpolicy.RouteIdentity {
	identity := extprocpolicy.RouteIdentity{}
	if string(ref.Kind) == "Gateway" {
		identity.Gateway = string(ref.Name)
		if ref.SectionName != nil {
			identity.Listener = string(*ref.SectionName)
		}
	} else {
		identity.Route = string(ref.Name)
		if route, ok := object.(*gatewayv1.HTTPRoute); ok && len(route.Spec.ParentRefs) > 0 {
			identity.Gateway = string(route.Spec.ParentRefs[0].Name)
		}
		if ref.SectionName != nil {
			identity.Rule = routeRuleIndex(object, *ref.SectionName)
		}
	}
	return identity
}

// routeRuleIndex translates the Gateway API rule section name to the stable
// rule index used in Envoy Gateway's xds.route_name, for example
// httproute/<namespace>/<route>/rule/0/match/0/*. A section is already
// validated by Resolver before this function is reached.
func routeRuleIndex(object client.Object, section gatewayv1.SectionName) string {
	switch route := object.(type) {
	case *gatewayv1.HTTPRoute:
		for index, rule := range route.Spec.Rules {
			if rule.Name != nil && *rule.Name == section {
				return strconv.Itoa(index)
			}
		}
	case *gatewayv1.GRPCRoute:
		for index, rule := range route.Spec.Rules {
			if rule.Name != nil && *rule.Name == section {
				return strconv.Itoa(index)
			}
		}
	}
	return ""
}
