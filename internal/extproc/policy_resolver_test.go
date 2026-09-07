package extproc

import (
	"errors"
	"testing"

	"thyris-sz/internal/extproc/policy"
)

func TestHeaderPolicyResolverAcceptsExactlyOneTrustedPolicy(t *testing.T) {
	tenant := "tenant-a"
	resolved, err := (HeaderPolicyResolver{}).ResolvePolicy(PolicyResolutionInput{
		Headers: map[string][]string{"X-TSZ-Policy": {" route-policy "}},
		Tenant:  &tenant,
	})
	if err != nil {
		t.Fatalf("ResolvePolicy() error = %v", err)
	}
	if resolved.PolicyID != "route-policy" || resolved.Tenant == nil || *resolved.Tenant != tenant {
		t.Fatalf("ResolvePolicy() = %+v", resolved)
	}
}

func TestAttributePolicyResolverUsesMostSpecificRouteIdentity(t *testing.T) {
	mapper := routeMapper{bindings: map[policy.RouteIdentity]string{
		{Gateway: "gateway", Listener: "https", Route: "orders", Rule: "checkout"}: "rule-policy",
		{Gateway: "gateway", Listener: "https", Route: "orders"}:                   "route-policy",
		{Gateway: "gateway", Listener: "https"}:                                    "listener-policy",
		{Gateway: "gateway"}:                                                       "gateway-policy",
	}}
	resolved, err := (AttributePolicyResolver{Mapping: mapper}).ResolvePolicy(PolicyResolutionInput{Attributes: map[string]string{
		"xds.gateway_name":    "gateway",
		"xds.listener_name":   "https",
		"xds.route_name":      "orders",
		"xds.route_rule_name": "checkout",
	}})
	if err != nil {
		t.Fatalf("ResolvePolicy() error = %v", err)
	}
	if resolved.PolicyID != "rule-policy" {
		t.Fatalf("ResolvePolicy() policy = %q, want rule-policy", resolved.PolicyID)
	}
}

func TestAttributePolicyResolverFallsBackAndRejectsMissingBinding(t *testing.T) {
	resolver := AttributePolicyResolver{Mapping: routeMapper{bindings: map[policy.RouteIdentity]string{
		{Gateway: "gateway", Listener: "https"}: "listener-policy",
	}}}
	resolved, err := resolver.ResolvePolicy(PolicyResolutionInput{Attributes: map[string]string{
		"xds.gateway_name": "gateway", "xds.listener_name": "https", "xds.route_name": "orders",
	}})
	if err != nil {
		t.Fatalf("ResolvePolicy() fallback error = %v", err)
	}
	if resolved.PolicyID != "listener-policy" {
		t.Fatalf("ResolvePolicy() fallback policy = %q, want listener-policy", resolved.PolicyID)
	}
	_, err = resolver.ResolvePolicy(PolicyResolutionInput{Attributes: map[string]string{"xds.gateway_name": "other"}})
	if !errors.Is(err, ErrRoutePolicyNotFound) {
		t.Fatalf("ResolvePolicy() missing binding error = %v, want ErrRoutePolicyNotFound", err)
	}
}

func TestAttributePolicyResolverUsesRouteOnlyFallback(t *testing.T) {
	resolver := AttributePolicyResolver{Mapping: routeMapper{bindings: map[policy.RouteIdentity]string{
		{Route: "httproute/demo/orders/rule/0"}: "route-policy",
	}}}
	resolved, err := resolver.ResolvePolicy(PolicyResolutionInput{Attributes: map[string]string{
		"xds.gateway_name": "gateway", "xds.route_name": "httproute/demo/orders/rule/0",
	}})
	if err != nil {
		t.Fatalf("ResolvePolicy() fallback error = %v", err)
	}
	if resolved.PolicyID != "route-policy" {
		t.Fatalf("ResolvePolicy() policy = %q, want route-policy", resolved.PolicyID)
	}
}

func TestAttributePolicyResolverExtractsEnvoyGatewayRuleIndex(t *testing.T) {
	resolver := AttributePolicyResolver{Mapping: routeMapper{bindings: map[policy.RouteIdentity]string{
		{Route: "orders", Rule: "1"}: "orders-rule-policy",
	}}}
	resolved, err := resolver.ResolvePolicy(PolicyResolutionInput{Attributes: map[string]string{
		"xds.route_name": "httproute/apps/orders/rule/1/match/0/*",
	}})
	if err != nil {
		t.Fatalf("ResolvePolicy() error = %v", err)
	}
	if resolved.PolicyID != "orders-rule-policy" {
		t.Fatalf("ResolvePolicy() policy = %q, want orders-rule-policy", resolved.PolicyID)
	}
}

type routeMapper struct {
	bindings map[policy.RouteIdentity]string
}

func (m routeMapper) LookupRoutePolicy(identity policy.RouteIdentity) (policy.RoutePolicyBinding, bool, error) {
	policyID, found := m.bindings[identity]
	return policy.RoutePolicyBinding{PolicyID: policyID}, found, nil
}

func TestHeaderPolicyResolverRejectsMissingDuplicateAndEmptyValues(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string][]string
	}{
		{name: "missing", headers: map[string][]string{}},
		{name: "empty", headers: map[string][]string{trustedPolicyHeader: {"  "}}},
		{name: "duplicate values", headers: map[string][]string{trustedPolicyHeader: {"one", "two"}}},
		{name: "duplicate header casing", headers: map[string][]string{"X-TSZ-Policy": {"one"}, "x-tsz-policy": {"two"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := (HeaderPolicyResolver{}).ResolvePolicy(PolicyResolutionInput{Headers: test.headers})
			if !errors.Is(err, ErrInvalidPolicyIdentity) {
				t.Fatalf("ResolvePolicy() error = %v, want ErrInvalidPolicyIdentity", err)
			}
		})
	}
}

func TestHeaderPolicyResolverDoesNotUseGuardrailOverrideHeaders(t *testing.T) {
	resolved, err := (HeaderPolicyResolver{}).ResolvePolicy(PolicyResolutionInput{Headers: map[string][]string{
		trustedPolicyHeader:     {"required-route-policy"},
		"x-tsz-guardrails":      {"none"},
		"x-tsz-guardrails-mode": {"open"},
	}})
	if err != nil {
		t.Fatalf("ResolvePolicy() error = %v", err)
	}
	if resolved.PolicyID != "required-route-policy" {
		t.Fatalf("policy override changed resolved policy: %+v", resolved)
	}
}
