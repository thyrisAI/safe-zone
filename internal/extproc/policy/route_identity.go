package policy

import (
	"context"
	"database/sql"
	"fmt"
)

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

// RoutePolicyBindingStore is owned by the controller. It persists native
// attachment identities without making request processing depend on Kubernetes.
type RoutePolicyBindingStore interface {
	RouteIdentityMapper
	UpsertRoutePolicy(context.Context, RouteIdentity, RoutePolicyBinding) error
	DeleteRoutePolicy(context.Context, RouteIdentity) error
}

type PostgresRoutePolicyBindingStore struct{ db *sql.DB }

func NewPostgresRoutePolicyBindingStore(db *sql.DB) (*PostgresRoutePolicyBindingStore, error) {
	if db == nil {
		return nil, fmt.Errorf("PostgreSQL database is required")
	}
	return &PostgresRoutePolicyBindingStore{db: db}, nil
}

func (s *PostgresRoutePolicyBindingStore) UpsertRoutePolicy(ctx context.Context, id RouteIdentity, binding RoutePolicyBinding) error {
	if id.Gateway == "" || binding.PolicyID == "" {
		return fmt.Errorf("route binding requires gateway and policy ID")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO route_policy_bindings (gateway_name, listener_name, route_name, rule_name, policy_name)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (gateway_name, listener_name, route_name, rule_name)
		DO UPDATE SET policy_name=EXCLUDED.policy_name, updated_at=now()`, id.Gateway, nullable(id.Listener), nullable(id.Route), nullable(id.Rule), binding.PolicyID)
	return err
}

func (s *PostgresRoutePolicyBindingStore) DeleteRoutePolicy(ctx context.Context, id RouteIdentity) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM route_policy_bindings WHERE gateway_name=$1 AND listener_name IS NOT DISTINCT FROM $2 AND route_name IS NOT DISTINCT FROM $3 AND rule_name IS NOT DISTINCT FROM $4`, id.Gateway, nullable(id.Listener), nullable(id.Route), nullable(id.Rule))
	return err
}

func (s *PostgresRoutePolicyBindingStore) LookupRoutePolicy(id RouteIdentity) (RoutePolicyBinding, bool, error) {
	var name string
	err := s.db.QueryRow(`SELECT policy_name FROM route_policy_bindings WHERE gateway_name=$1 AND listener_name IS NOT DISTINCT FROM $2 AND route_name IS NOT DISTINCT FROM $3 AND rule_name IS NOT DISTINCT FROM $4`, id.Gateway, nullable(id.Listener), nullable(id.Route), nullable(id.Rule)).Scan(&name)
	if err == nil {
		return RoutePolicyBinding{PolicyID: name}, true, nil
	}
	if err != sql.ErrNoRows {
		return RoutePolicyBinding{}, false, err
	}

	// Envoy Gateway emits an implementation-owned, fully-qualified XDS route
	// name (for example httproute/<namespace>/<name>/rule/0), rather than the
	// bare Gateway API HTTPRoute name that the controller persists. If Envoy did
	// not supply a gateway attribute, resolve an unambiguous suffix match. Exact
	// identities above always win; ambiguity is rejected instead of selecting a
	// policy silently.
	if id.Gateway != "" || id.Listener != "" || id.Rule != "" || id.Route == "" {
		return RoutePolicyBinding{}, false, nil
	}
	rows, err := s.db.Query(`SELECT policy_name FROM route_policy_bindings
		WHERE route_name IS NOT NULL AND ($1 = route_name OR $1 LIKE '%' || route_name || '%')
		ORDER BY updated_at DESC LIMIT 2`, id.Route)
	if err != nil {
		return RoutePolicyBinding{}, false, err
	}
	defer rows.Close()
	matches := make([]string, 0, 2)
	for rows.Next() {
		var policyName string
		if err := rows.Scan(&policyName); err != nil {
			return RoutePolicyBinding{}, false, err
		}
		matches = append(matches, policyName)
	}
	if err := rows.Err(); err != nil {
		return RoutePolicyBinding{}, false, err
	}
	switch len(matches) {
	case 0:
		return RoutePolicyBinding{}, false, nil
	case 1:
		return RoutePolicyBinding{PolicyID: matches[0]}, true, nil
	default:
		return RoutePolicyBinding{}, false, fmt.Errorf("ambiguous native route policy binding for %q", id.Route)
	}
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
