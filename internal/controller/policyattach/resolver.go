package policyattach

import (
	"context"
	"fmt"

	securityv1alpha1 "thyris-sz/api/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

const gatewayAPIGroup = "gateway.networking.k8s.io"

// ResolvedTarget is the outcome of resolving one attachment target. A target
// is always returned, including when it cannot be resolved, so callers can
// report partial success without silently skipping invalid references.
type ResolvedTarget struct {
	Ref       gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName
	Kind      string
	Object    client.Object
	SectionOK bool
	Err       error
}

// Resolver resolves local Gateway API policy targets.
type Resolver struct {
	Client client.Client
}

// ResolveTargets resolves every target relative to the policy namespace.
// LocalPolicyTargetReferenceWithSectionName deliberately has no namespace;
// cross-namespace attachment is therefore not supported by this controller.
func (r *Resolver) ResolveTargets(ctx context.Context, policy *securityv1alpha1.TSZGuardrailPolicy) []ResolvedTarget {
	resolved := make([]ResolvedTarget, 0, len(policy.Spec.TargetRefs))
	for _, ref := range policy.Spec.TargetRefs {
		resolved = append(resolved, r.resolveTarget(ctx, policy.Namespace, ref))
	}
	return resolved
}

func (r *Resolver) resolveTarget(ctx context.Context, namespace string, ref gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName) ResolvedTarget {
	target := ResolvedTarget{Ref: ref, Kind: string(ref.Kind)}
	if string(ref.Group) != gatewayAPIGroup {
		target.Err = fmt.Errorf("unsupported target group %q", ref.Group)
		return target
	}

	key := client.ObjectKey{Namespace: namespace, Name: string(ref.Name)}
	switch string(ref.Kind) {
	case "Gateway":
		object := &gatewayv1.Gateway{}
		if err := r.Client.Get(ctx, key, object); err != nil {
			target.Err = fmt.Errorf("resolve Gateway %s: %w", key, err)
			return target
		}
		target.Object = object
		target.SectionOK = gatewaySectionExists(object, ref.SectionName)
	case "HTTPRoute":
		object := &gatewayv1.HTTPRoute{}
		if err := r.Client.Get(ctx, key, object); err != nil {
			target.Err = fmt.Errorf("resolve HTTPRoute %s: %w", key, err)
			return target
		}
		target.Object = object
		target.SectionOK = httpRouteSectionExists(object, ref.SectionName)
	case "GRPCRoute":
		object := &gatewayv1.GRPCRoute{}
		if err := r.Client.Get(ctx, key, object); err != nil {
			target.Err = fmt.Errorf("resolve GRPCRoute %s: %w", key, err)
			return target
		}
		target.Object = object
		target.SectionOK = grpcRouteSectionExists(object, ref.SectionName)
	default:
		target.Err = fmt.Errorf("unsupported target kind %q", ref.Kind)
	}

	return target
}

func gatewaySectionExists(gateway *gatewayv1.Gateway, section *gatewayv1.SectionName) bool {
	if section == nil {
		return true
	}
	for _, listener := range gateway.Spec.Listeners {
		if listener.Name == *section {
			return true
		}
	}
	return false
}

func httpRouteSectionExists(route *gatewayv1.HTTPRoute, section *gatewayv1.SectionName) bool {
	if section == nil {
		return true
	}
	for _, rule := range route.Spec.Rules {
		if rule.Name != nil && *rule.Name == *section {
			return true
		}
	}
	return false
}

func grpcRouteSectionExists(route *gatewayv1.GRPCRoute, section *gatewayv1.SectionName) bool {
	if section == nil {
		return true
	}
	for _, rule := range route.Spec.Rules {
		if rule.Name != nil && *rule.Name == *section {
			return true
		}
	}
	return false
}
