package effectivepolicy

import (
	"context"
	"errors"

	securityv1alpha1 "thyris-sz/api/v1alpha1"
	"thyris-sz/internal/extproc/policy"
)

const (
	ResolutionResolved             = "Resolved"
	ResolutionPolicyNotFound       = "PolicyNotFound"
	ResolutionVersionIncompatible  = "VersionIncompatible"
	ResolutionProcessorUnavailable = "ProcessorUnavailable"
)

// ReferenceResolver validates PostgresRef attachments through the established
// policy repository. It deliberately contains no direct SQL.
type ReferenceResolver struct {
	Repo policy.Repository
}

// ResolutionResult identifies both a usable snapshot and the status reason to
// report when it is unavailable.
type ResolutionResult struct {
	Snapshot policy.PolicySnapshot
	Reason   string
	Err      error
}

// Resolve validates that the named policy exists and resolves its requested
// immutable version. Omitted versions are rejected so an attachment never
// floats to a later active snapshot.
func (r *ReferenceResolver) Resolve(ctx context.Context, ref *securityv1alpha1.PolicyReference, tenant *string) ResolutionResult {
	if r == nil || r.Repo == nil {
		return ResolutionResult{Reason: ResolutionProcessorUnavailable, Err: errors.New("policy repository is required")}
	}
	if ref == nil || ref.Name == "" || ref.Version == nil || *ref.Version < 1 {
		return ResolutionResult{Reason: ResolutionPolicyNotFound, Err: policy.ErrNotFound}
	}

	if _, err := r.Repo.PolicyByName(ctx, ref.Name, tenant); err != nil {
		return resolutionFailure(err)
	}

	snapshot, err := r.Repo.SnapshotByVersion(ctx, ref.Name, tenant, int(*ref.Version))
	if err != nil {
		return resolutionFailure(err)
	}
	if snapshot.Status != policy.StatusActive && snapshot.Status != policy.StatusCompiled {
		return ResolutionResult{Snapshot: snapshot, Reason: ResolutionVersionIncompatible}
	}
	return ResolutionResult{Snapshot: snapshot, Reason: ResolutionResolved}
}

func resolutionFailure(err error) ResolutionResult {
	if errors.Is(err, policy.ErrNotFound) {
		return ResolutionResult{Reason: ResolutionPolicyNotFound, Err: err}
	}
	return ResolutionResult{Reason: ResolutionProcessorUnavailable, Err: err}
}
