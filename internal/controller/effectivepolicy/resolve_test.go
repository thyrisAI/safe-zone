package effectivepolicy

import (
	"context"
	"errors"
	"testing"

	securityv1alpha1 "thyris-sz/api/v1alpha1"
	"thyris-sz/internal/extproc/policy"
)

func TestReferenceResolverResolvesCompiledAndActiveVersions(t *testing.T) {
	version := int32(7)
	for _, status := range []policy.SnapshotStatus{policy.StatusCompiled, policy.StatusActive} {
		t.Run(string(status), func(t *testing.T) {
			resolver := ReferenceResolver{Repo: referenceRepository{snapshot: policy.PolicySnapshot{Status: status}}}
			result := resolver.Resolve(context.Background(), &securityv1alpha1.PolicyReference{Name: "payments", Version: &version}, nil)
			if result.Reason != ResolutionResolved || result.Err != nil {
				t.Fatalf("Resolve() = %+v, want resolved", result)
			}
		})
	}
}

func TestReferenceResolverSeparatesNotFoundFromDatabaseFailure(t *testing.T) {
	version := int32(2)
	tests := []struct {
		name       string
		repository referenceRepository
		reason     string
	}{
		{name: "policy missing", repository: referenceRepository{policyErr: policy.ErrNotFound}, reason: ResolutionPolicyNotFound},
		{name: "version missing", repository: referenceRepository{snapshotErr: policy.ErrNotFound}, reason: ResolutionPolicyNotFound},
		{name: "database unavailable", repository: referenceRepository{policyErr: errors.New("connection refused")}, reason: ResolutionProcessorUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := (&ReferenceResolver{Repo: test.repository}).Resolve(context.Background(), &securityv1alpha1.PolicyReference{Name: "payments", Version: &version}, nil)
			if result.Reason != test.reason || result.Err == nil {
				t.Fatalf("Resolve() = %+v, want reason %q and error", result, test.reason)
			}
		})
	}
}

func TestReferenceResolverRejectsIncompatibleVersion(t *testing.T) {
	version := int32(3)
	resolver := ReferenceResolver{Repo: referenceRepository{snapshot: policy.PolicySnapshot{Status: policy.StatusSuperseded}}}
	result := resolver.Resolve(context.Background(), &securityv1alpha1.PolicyReference{Name: "payments", Version: &version}, nil)
	if result.Reason != ResolutionVersionIncompatible || result.Err != nil {
		t.Fatalf("Resolve() = %+v, want incompatible version without repository error", result)
	}
}

type referenceRepository struct {
	policy.Repository
	policyErr   error
	snapshotErr error
	snapshot    policy.PolicySnapshot
}

func (r referenceRepository) PolicyByName(context.Context, string, *string) (policy.Policy, error) {
	if r.policyErr != nil {
		return policy.Policy{}, r.policyErr
	}
	return policy.Policy{ID: 1, Name: "payments"}, nil
}

func (r referenceRepository) SnapshotByVersion(context.Context, string, *string, int) (policy.PolicySnapshot, error) {
	if r.snapshotErr != nil {
		return policy.PolicySnapshot{}, r.snapshotErr
	}
	return r.snapshot, nil
}
