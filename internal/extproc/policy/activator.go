package policy

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

type Activator struct {
	repository Repository
	publisher  ActivationPublisher
	now        func() time.Time
	// afterActivationPolicyLock is used only by package tests to hold a real
	// transaction open while another activation attempts the NOWAIT lock.
	afterActivationPolicyLock func()
}

func NewActivator(repository Repository, publisher ActivationPublisher) (*Activator, error) {
	if repository == nil {
		return nil, errors.New("policy repository is required")
	}
	if publisher == nil {
		return nil, errors.New("policy activation publisher is required")
	}
	return &Activator{repository: repository, publisher: publisher, now: time.Now}, nil
}

// Activate atomically supersedes any current active version before promoting
// the new compiled snapshot.
func (a *Activator) Activate(ctx context.Context, snapshotID int64) error {
	if snapshotID <= 0 {
		return fmt.Errorf("activate policy snapshot: invalid snapshot id %d", snapshotID)
	}
	var event ActivationEvent
	if err := a.repository.WithinTransaction(ctx, func(transaction Transaction) error {
		snapshot, err := transaction.SnapshotForUpdate(ctx, snapshotID)
		if err != nil {
			return fmt.Errorf("load compiled snapshot: %w", err)
		}
		if snapshot.Status != StatusCompiled {
			return fmt.Errorf("%w: snapshot %d has status %q, want %q", ErrInvalidTransition, snapshotID, snapshot.Status, StatusCompiled)
		}
		if err := transaction.TryLockPolicyForActivation(ctx, snapshot.PolicyID); err != nil {
			if isLockNotAvailable(err) {
				return fmt.Errorf("%w: policy %q is being activated", ErrActiveSnapshotConflict, snapshot.PolicyName)
			}
			return fmt.Errorf("lock policy for activation: %w", err)
		}
		if a.afterActivationPolicyLock != nil {
			a.afterActivationPolicyLock()
		}

		active, err := transaction.ActiveSnapshotForUpdate(ctx, snapshot.PolicyID)
		switch {
		case err == nil:
			if err := transaction.MarkSuperseded(ctx, active.ID); err != nil {
				return fmt.Errorf("supersede active snapshot: %w", err)
			}
		case !errors.Is(err, ErrNotFound):
			return fmt.Errorf("check active snapshot: %w", err)
		}

		if err := transaction.MarkActive(ctx, snapshot.ID, a.now().UTC()); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: policy %q", ErrActiveSnapshotConflict, snapshot.PolicyName)
			}
			return fmt.Errorf("persist active snapshot: %w", err)
		}
		if snapshot.Version == nil {
			return fmt.Errorf("%w: compiled snapshot %d has no version", ErrInvalidTransition, snapshot.ID)
		}
		event = ActivationEvent{PolicyID: PolicyIdentifier(snapshot.PolicyName, snapshot.Tenant), Version: *snapshot.Version}
		return nil
	}); err != nil {
		return err
	}

	if err := a.publisher.PublishActivation(ctx, event); err != nil {
		publishErr := &ActivationPublishError{Event: event, Err: err}
		log.Printf("[DEGRADED] policy activation committed but Redis publish failed; retryable=true policy_id=%q version=%d error=%v", event.PolicyID, event.Version, err)
		return publishErr
	}
	return nil
}

// Rollback restores the latest immutable superseded version. It does not
// compile or mutate policy definitions.
func (a *Activator) Rollback(ctx context.Context, policyID int64) error {
	if policyID <= 0 {
		return fmt.Errorf("rollback policy: invalid policy id %d", policyID)
	}
	var event ActivationEvent
	if err := a.repository.WithinTransaction(ctx, func(transaction Transaction) error {
		if err := transaction.LockPolicy(ctx, policyID); err != nil {
			return fmt.Errorf("lock policy for rollback: %w", err)
		}
		active, err := transaction.ActiveSnapshotForUpdate(ctx, policyID)
		if err != nil {
			return fmt.Errorf("load active snapshot: %w", err)
		}
		candidate, err := transaction.LatestSupersededForUpdate(ctx, policyID)
		if err != nil {
			return fmt.Errorf("load rollback snapshot: %w", err)
		}
		if candidate.Version == nil {
			return fmt.Errorf("%w: superseded snapshot %d has no version", ErrInvalidTransition, candidate.ID)
		}
		if err := transaction.MarkRolledBack(ctx, active.ID); err != nil {
			return fmt.Errorf("mark active snapshot rolled back: %w", err)
		}
		if err := transaction.MarkActive(ctx, candidate.ID, a.now().UTC()); err != nil {
			return fmt.Errorf("restore superseded snapshot: %w", err)
		}
		event = ActivationEvent{PolicyID: PolicyIdentifier(candidate.PolicyName, candidate.Tenant), Version: *candidate.Version}
		return nil
	}); err != nil {
		return err
	}
	if err := a.publisher.PublishActivation(ctx, event); err != nil {
		publishErr := &ActivationPublishError{Event: event, Err: err}
		log.Printf("[DEGRADED] policy rollback committed but Redis publish failed; retryable=true policy_id=%q version=%d error=%v", event.PolicyID, event.Version, err)
		return publishErr
	}
	return nil
}

// RepublishActivation retries Redis notification for an already-active
// snapshot. Unlike Activate, it never opens an activation transaction or
// changes snapshot state, so it is safe after an ActivationPublishError.
func (a *Activator) RepublishActivation(ctx context.Context, policyName string, tenant *string) error {
	if a == nil || a.repository == nil || a.publisher == nil {
		return errors.New("policy activator dependencies are required")
	}
	snapshot, err := a.repository.ActiveSnapshot(ctx, policyName, tenant)
	if err != nil {
		return fmt.Errorf("load active snapshot for republish: %w", err)
	}
	if snapshot.Version == nil {
		return fmt.Errorf("active snapshot %d has no version", snapshot.ID)
	}
	event := ActivationEvent{PolicyID: PolicyIdentifier(snapshot.PolicyName, snapshot.Tenant), Version: *snapshot.Version}
	if err := a.publisher.PublishActivation(ctx, event); err != nil {
		return &ActivationPublishError{Event: event, Err: err}
	}
	return nil
}

// ActivationPublishError means PostgreSQL activation committed successfully,
// but the Redis notification must be retried. Callers must not roll back or
// repeat the activation transaction.
type ActivationPublishError struct {
	Event ActivationEvent
	Err   error
}

func (e *ActivationPublishError) Error() string {
	return fmt.Sprintf("policy activation committed; Redis notification is retryable: %v", e.Err)
}

func (e *ActivationPublishError) Unwrap() error {
	return e.Err
}

func (e *ActivationPublishError) Retryable() bool {
	return true
}

type sqlStateError interface {
	SQLState() string
}

func isUniqueViolation(err error) bool {
	var stateError sqlStateError
	return errors.As(err, &stateError) && stateError.SQLState() == "23505"
}

func isLockNotAvailable(err error) bool {
	var stateError sqlStateError
	return errors.As(err, &stateError) && stateError.SQLState() == "55P03"
}
