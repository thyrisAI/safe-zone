package extproc

import (
	"errors"
	"fmt"
	"strings"

	"thyris-sz/internal/extproc/policy"
)

const trustedPolicyHeader = "x-tsz-policy"

var ErrInvalidPolicyIdentity = errors.New("invalid trusted policy identity")

// ErrRoutePolicyNotFound indicates that no controller-owned native attachment
// matched the trusted Envoy attributes.
var ErrRoutePolicyNotFound = errors.New("route policy binding not found")

// PolicyResolutionInput is gateway-neutral. Future policy controllers may
// resolve a route policy from any source without introducing Envoy or
// Kubernetes types into processing code.
type PolicyResolutionInput struct {
	Headers map[string][]string
	Gateway string
	Route   string
	Tenant  *string
	// Attributes are trusted gateway context supplied by the adapter, not
	// downstream request headers.
	Attributes map[string]string
}

// ResolvedPolicy is the immutable identity that is pinned to a stream. Tenant
// remains nullable for the Phase 2 MVP.
type ResolvedPolicy struct {
	PolicyID string
	Tenant   *string
}

// PolicyResolver resolves a trusted route policy before a stream is pinned.
type PolicyResolver interface {
	ResolvePolicy(PolicyResolutionInput) (ResolvedPolicy, error)
}

// HeaderPolicyResolver accepts exactly one policy header. The header must have
// been removed and overwritten by trusted route configuration before Envoy
// sends it to ext-proc; this resolver never treats a downstream source as
// authoritative by itself.
type HeaderPolicyResolver struct{}

func (HeaderPolicyResolver) ResolvePolicy(input PolicyResolutionInput) (ResolvedPolicy, error) {
	var values []string
	for name, candidates := range input.Headers {
		if strings.EqualFold(name, trustedPolicyHeader) {
			values = append(values, candidates...)
		}
	}
	if len(values) == 0 {
		return ResolvedPolicy{}, fmt.Errorf("%w: %s header is required", ErrInvalidPolicyIdentity, trustedPolicyHeader)
	}
	if len(values) != 1 {
		return ResolvedPolicy{}, fmt.Errorf("%w: %s header must occur exactly once", ErrInvalidPolicyIdentity, trustedPolicyHeader)
	}
	policyID := strings.TrimSpace(values[0])
	if policyID == "" {
		return ResolvedPolicy{}, fmt.Errorf("%w: %s header must not be empty", ErrInvalidPolicyIdentity, trustedPolicyHeader)
	}
	return ResolvedPolicy{PolicyID: policyID, Tenant: input.Tenant}, nil
}

// AttributePolicyResolver resolves native/managed policy attachments from
// trusted Envoy ext_proc attributes. HeaderPolicyResolver remains the resolver
// for manual and preview installations.
type AttributePolicyResolver struct {
	Mapping policy.RouteIdentityMapper
}

func (r AttributePolicyResolver) ResolvePolicy(input PolicyResolutionInput) (ResolvedPolicy, error) {
	if r.Mapping == nil {
		return ResolvedPolicy{}, fmt.Errorf("%w: route identity mapper is required", ErrRoutePolicyNotFound)
	}

	for _, identity := range routeIdentityCandidates(input.Attributes) {
		binding, found, err := r.Mapping.LookupRoutePolicy(identity)
		if err != nil {
			return ResolvedPolicy{}, fmt.Errorf("lookup route policy binding: %w", err)
		}
		if !found {
			continue
		}
		policyID := strings.TrimSpace(binding.PolicyID)
		if policyID == "" {
			return ResolvedPolicy{}, fmt.Errorf("%w: empty policy ID for route identity", ErrRoutePolicyNotFound)
		}
		return ResolvedPolicy{PolicyID: policyID, Tenant: input.Tenant}, nil
	}

	return ResolvedPolicy{}, fmt.Errorf("%w: no matching Envoy route attributes", ErrRoutePolicyNotFound)
}

func routeIdentityCandidates(attributes map[string]string) []policy.RouteIdentity {
	if len(attributes) == 0 {
		return nil
	}
	gateway := strings.TrimSpace(attributes["xds.gateway_name"])
	if gateway == "" {
		gateway = strings.TrimSpace(attributes["xds.cluster_name"])
	}
	listener := strings.TrimSpace(attributes["xds.listener_name"])
	route := strings.TrimSpace(attributes["xds.route_name"])
	rule := strings.TrimSpace(attributes["xds.route_rule_name"])

	candidates := make([]policy.RouteIdentity, 0, 4)
	if route != "" && rule != "" {
		candidates = append(candidates, policy.RouteIdentity{Gateway: gateway, Listener: listener, Route: route, Rule: rule})
	}
	if route != "" {
		candidates = append(candidates, policy.RouteIdentity{Gateway: gateway, Listener: listener, Route: route})
	}
	if listener != "" {
		candidates = append(candidates, policy.RouteIdentity{Gateway: gateway, Listener: listener})
	}
	if gateway != "" {
		candidates = append(candidates, policy.RouteIdentity{Gateway: gateway})
	}
	return candidates
}
