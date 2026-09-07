package policy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestActivatorActivatesCompiledSnapshot(t *testing.T) {
	_, repository := openCompilerTestRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snapshotID := createCompiledSnapshot(t, ctx, repository, "activate")
	publisher := &recordingPublisher{}
	activator, err := NewActivator(repository, publisher)
	if err != nil {
		t.Fatalf("NewActivator() error = %v", err)
	}

	if err := activator.Activate(ctx, snapshotID); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	snapshot, err := repository.SnapshotByID(ctx, snapshotID)
	if err != nil {
		t.Fatalf("SnapshotByID() error = %v", err)
	}
	if snapshot.Status != StatusActive {
		t.Fatalf("snapshot status = %q, want %q", snapshot.Status, StatusActive)
	}
	if snapshot.ActivatedAt == nil || snapshot.ActivatedAt.IsZero() {
		t.Fatal("activated_at was not set")
	}
	if snapshot.CompiledAt == nil || snapshot.CompiledAt.IsZero() {
		t.Fatal("compiled_at was unexpectedly cleared")
	}
	events := publisher.Events()
	if len(events) != 1 || events[0].PolicyID != "activate" || events[0].Version != 1 {
		t.Fatalf("activation events = %+v, want activate/version 1", events)
	}
}

func TestActivatorRejectsNonCompiledSnapshot(t *testing.T) {
	_, repository := openCompilerTestRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snapshotID, err := repository.CreateValidated(ctx, "not-compiled", validPolicyDefinition())
	if err != nil {
		t.Fatalf("CreateValidated() error = %v", err)
	}
	activator, err := NewActivator(repository, &recordingPublisher{})
	if err != nil {
		t.Fatalf("NewActivator() error = %v", err)
	}

	if err := activator.Activate(ctx, snapshotID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Activate() error = %v, want ErrInvalidTransition", err)
	}
	snapshot, err := repository.SnapshotByID(ctx, snapshotID)
	if err != nil {
		t.Fatalf("SnapshotByID() error = %v", err)
	}
	if snapshot.Status != StatusValidated || snapshot.ActivatedAt != nil {
		t.Fatalf("failed activation mutated snapshot: %+v", snapshot)
	}
}

func TestActivatorSupersedesExistingActiveSnapshot(t *testing.T) {
	_, repository := openCompilerTestRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	firstID := createCompiledSnapshot(t, ctx, repository, "conflict")
	secondID := createCompiledSnapshot(t, ctx, repository, "conflict")
	publisher := &recordingPublisher{}
	activator, err := NewActivator(repository, publisher)
	if err != nil {
		t.Fatalf("NewActivator() error = %v", err)
	}
	if err := activator.Activate(ctx, firstID); err != nil {
		t.Fatalf("activate first snapshot: %v", err)
	}
	if err := activator.Activate(ctx, secondID); err != nil {
		t.Fatalf("activate second snapshot: %v", err)
	}

	active, err := repository.ActiveSnapshot(ctx, "conflict", nil)
	if err != nil {
		t.Fatalf("ActiveSnapshot() error = %v", err)
	}
	if active.ID != secondID {
		t.Fatalf("active snapshot = %d, want new %d", active.ID, secondID)
	}
	second, err := repository.SnapshotByID(ctx, secondID)
	if err != nil {
		t.Fatalf("load conflicting snapshot: %v", err)
	}
	if second.Status != StatusActive || second.ActivatedAt == nil {
		t.Fatalf("new snapshot was not activated: %+v", second)
	}
	first, err := repository.SnapshotByID(ctx, firstID)
	if err != nil || first.Status != StatusSuperseded {
		t.Fatalf("old snapshot was not superseded: snapshot=%+v err=%v", first, err)
	}
	if events := publisher.Events(); len(events) != 2 {
		t.Fatalf("activation events = %d, want 2", len(events))
	}
}

func TestActivatorRollbackRestoresLatestSupersededSnapshot(t *testing.T) {
	_, repository := openCompilerTestRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	v1 := createCompiledSnapshot(t, ctx, repository, "rollback")
	v2 := createCompiledSnapshot(t, ctx, repository, "rollback")
	publisher := &recordingPublisher{}
	activator, err := NewActivator(repository, publisher)
	if err != nil {
		t.Fatalf("NewActivator() error = %v", err)
	}
	if err := activator.Activate(ctx, v1); err != nil {
		t.Fatalf("activate v1: %v", err)
	}
	if err := activator.Activate(ctx, v2); err != nil {
		t.Fatalf("activate v2: %v", err)
	}
	v2Snapshot, err := repository.SnapshotByID(ctx, v2)
	if err != nil {
		t.Fatalf("load v2: %v", err)
	}
	if err := activator.Rollback(ctx, v2Snapshot.PolicyID); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	first, err := repository.SnapshotByID(ctx, v1)
	if err != nil || first.Status != StatusActive || first.ActivatedAt == nil {
		t.Fatalf("v1 after rollback = %+v err=%v", first, err)
	}
	second, err := repository.SnapshotByID(ctx, v2)
	if err != nil || second.Status != StatusRolledBack {
		t.Fatalf("v2 after rollback = %+v err=%v", second, err)
	}
	events := publisher.Events()
	if len(events) != 3 || events[2].Version != 1 {
		t.Fatalf("activation events = %+v, want rollback publication of v1", events)
	}
}

func TestActivatorSerializesConcurrentFirstActivation(t *testing.T) {
	_, repository := openCompilerTestRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	firstID := createCompiledSnapshot(t, ctx, repository, "concurrent-activation")
	secondID := createCompiledSnapshot(t, ctx, repository, "concurrent-activation")
	activator, err := NewActivator(repository, &recordingPublisher{})
	if err != nil {
		t.Fatalf("NewActivator() error = %v", err)
	}

	firstLockAcquired := make(chan struct{})
	releaseFirstLock := make(chan struct{})
	activator.afterActivationPolicyLock = func() {
		close(firstLockAcquired)
		<-releaseFirstLock
	}
	errorsChannel := make(chan error, 2)
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		errorsChannel <- activator.Activate(ctx, firstID)
	}()
	<-firstLockAcquired
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		errorsChannel <- activator.Activate(ctx, secondID)
	}()

	// The second activation must fail while the first transaction owns the
	// policy row; releasing first only after that makes the race deterministic.
	secondResult := <-errorsChannel
	if !errors.Is(secondResult, ErrActiveSnapshotConflict) {
		t.Fatalf("concurrent Activate() error = %v, want ErrActiveSnapshotConflict", secondResult)
	}
	close(releaseFirstLock)
	waitGroup.Wait()
	close(errorsChannel)

	var successes, conflicts int
	conflicts++
	for err := range errorsChannel {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrActiveSnapshotConflict):
			conflicts++
		default:
			t.Fatalf("concurrent Activate() unexpected error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent results successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}

	activeSnapshots, err := repository.ActiveSnapshots(ctx)
	if err != nil {
		t.Fatalf("ActiveSnapshots() error = %v", err)
	}
	if len(activeSnapshots) != 1 {
		t.Fatalf("active snapshot count = %d, want 1", len(activeSnapshots))
	}
}

func TestActivatorReturnsRetryablePublishErrorAfterCommit(t *testing.T) {
	_, repository := openCompilerTestRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snapshotID := createCompiledSnapshot(t, ctx, repository, "publish-degraded")
	publisher := &recordingPublisher{err: errors.New("Redis unavailable")}
	activator, err := NewActivator(repository, publisher)
	if err != nil {
		t.Fatalf("NewActivator() error = %v", err)
	}

	err = activator.Activate(ctx, snapshotID)
	var publishError *ActivationPublishError
	if !errors.As(err, &publishError) {
		t.Fatalf("Activate() error = %v, want ActivationPublishError", err)
	}
	if !publishError.Retryable() || publishError.Event.PolicyID != "publish-degraded" || publishError.Event.Version != 1 {
		t.Fatalf("publish error metadata = %+v", publishError)
	}
	snapshot, loadErr := repository.SnapshotByID(ctx, snapshotID)
	if loadErr != nil {
		t.Fatalf("SnapshotByID() error = %v", loadErr)
	}
	if snapshot.Status != StatusActive || snapshot.ActivatedAt == nil {
		t.Fatalf("activation was rolled back after publish failure: %+v", snapshot)
	}
}

func createCompiledSnapshot(t *testing.T, ctx context.Context, repository Repository, policyName string) int64 {
	t.Helper()
	snapshotID, err := repository.CreateValidated(ctx, policyName, validPolicyDefinition())
	if err != nil {
		t.Fatalf("CreateValidated() error = %v", err)
	}
	compiler, err := NewCompiler(repository)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	if err := compiler.Compile(ctx, snapshotID); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return snapshotID
}
