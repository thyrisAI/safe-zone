package policy

// RouteIdentity identifies a Gateway API attachment point as observed by the
// data plane. Empty trailing fields intentionally represent less-specific
// attachment levels (route, listener, then gateway).
type RouteIdentity struct {
	Gateway  string
	Listener string
	Route    string
	Rule     string
}

// RoutePolicyBinding is the controller-owned association between a route
// identity and one immutable policy snapshot.
type RoutePolicyBinding struct {
	PolicyID string
}

// RouteIdentityMapper resolves controller-owned route bindings. Implementations
// will use the route_policy_bindings store with local and Redis-backed caches.
// The interface has no gateway protocol dependency so it is safe for ext-proc.
type RouteIdentityMapper interface {
	LookupRoutePolicy(RouteIdentity) (RoutePolicyBinding, bool, error)
}
