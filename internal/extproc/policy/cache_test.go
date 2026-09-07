package policy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type cacheRepositoryStub struct {
	Repository
	mu        sync.Mutex
	snapshots []PolicySnapshot
	err       error
}

func (r *cacheRepositoryStub) ActiveSnapshots(context.Context) ([]PolicySnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]PolicySnapshot(nil), r.snapshots...), r.err
}

func (r *cacheRepositoryStub) ActiveSnapshot(_ context.Context, policyName string, tenant *string) (PolicySnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	wanted := PolicyIdentifier(policyName, tenant)
	for _, snapshot := range r.snapshots {
		if PolicyIdentifier(snapshot.PolicyName, snapshot.Tenant) == wanted && snapshot.Status == StatusActive {
			return snapshot, nil
		}
	}
	return PolicySnapshot{}, ErrNotFound
}

func (r *cacheRepositoryStub) setSnapshots(snapshots ...PolicySnapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshots = snapshots
}

func TestCacheReadinessAndImmutableGet(t *testing.T) {
	repository := &cacheRepositoryStub{err: errors.New("PostgreSQL unavailable")}
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer redisClient.Close()
	cache, err := NewCache(repository, redisClient, time.Minute)
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	if cache.Ready() {
		t.Fatal("new cache is ready before first full load")
	}
	if err := cache.Reconcile(context.Background()); err == nil {
		t.Fatal("Reconcile() error = nil while repository is unavailable")
	}
	if cache.Ready() {
		t.Fatal("cache became ready after failed full load")
	}

	snapshot := completeActiveSnapshot("default", 1)
	repository.mu.Lock()
	repository.err = nil
	repository.snapshots = []PolicySnapshot{snapshot}
	repository.mu.Unlock()
	if err := cache.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !cache.Ready() {
		t.Fatal("cache is not ready after successful full load")
	}

	first, found := cache.Get("default")
	if !found {
		t.Fatal("Get(default) did not find active snapshot")
	}
	first.Definition.Request.CustomPatternIDs[0] = "mutated-by-request"
	second, found := cache.Get("default")
	if !found {
		t.Fatal("second Get(default) did not find active snapshot")
	}
	if second.Definition.Request.CustomPatternIDs[0] != "1" {
		t.Fatalf("request mutation changed cached snapshot: %+v", second.Definition.Request.CustomPatternIDs)
	}

	newSnapshot := completeActiveSnapshot("default", 2)
	repository.setSnapshots(newSnapshot)
	if err := cache.handleActivationMessage(context.Background(), `{"policy_id":"default","version":2}`); err != nil {
		t.Fatalf("handleActivationMessage() error = %v", err)
	}
	latest, found := cache.Get("default")
	if !found || latest.Version != 2 {
		t.Fatalf("latest cached snapshot = %+v, want version 2", latest)
	}
	if first.Version != 1 {
		t.Fatalf("request-held snapshot changed during swap: %+v", first)
	}
}

func TestCacheReadinessUsesFailureThresholdOrSnapshotStaleness(t *testing.T) {
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	repository := &cacheRepositoryStub{snapshots: []PolicySnapshot{completeActiveSnapshot("default", 1)}}
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer redisClient.Close()
	cache, err := NewCacheWithReadiness(repository, redisClient, time.Minute, ReadinessSettings{
		MaxStaleness:              5 * time.Minute,
		ReconcileFailureThreshold: 3,
	})
	if err != nil {
		t.Fatalf("NewCacheWithReadiness() error = %v", err)
	}
	cache.now = func() time.Time { return now }
	if err := cache.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile() error = %v", err)
	}
	if !cache.Ready() {
		t.Fatal("cache not ready after initial reconcile")
	}

	repository.mu.Lock()
	repository.err = errors.New("PostgreSQL unavailable")
	repository.mu.Unlock()
	for failures := 1; failures <= 2; failures++ {
		if err := cache.Reconcile(context.Background()); err == nil {
			t.Fatalf("failure %d: Reconcile() error = nil", failures)
		}
		if !cache.Ready() {
			t.Fatalf("cache became unready after %d consecutive failures", failures)
		}
	}
	if err := cache.Reconcile(context.Background()); err == nil {
		t.Fatal("threshold failure: Reconcile() error = nil")
	}
	if cache.Ready() {
		t.Fatal("cache remained ready after three consecutive failures")
	}
	if got := cache.consecutiveFailures.Load(); got != 3 {
		t.Fatalf("consecutive failures = %d, want 3", got)
	}

	repository.mu.Lock()
	repository.err = nil
	repository.mu.Unlock()
	now = now.Add(time.Minute)
	if err := cache.Reconcile(context.Background()); err != nil {
		t.Fatalf("recovery Reconcile() error = %v", err)
	}
	if !cache.Ready() || cache.consecutiveFailures.Load() != 0 {
		t.Fatalf("cache did not recover: ready=%t failures=%d", cache.Ready(), cache.consecutiveFailures.Load())
	}

	// A successful reconciliation resets both the counter and freshness age, so
	// intermittent failures do not flap readiness.
	repository.mu.Lock()
	repository.err = errors.New("temporary PostgreSQL failure")
	repository.mu.Unlock()
	if err := cache.Reconcile(context.Background()); err == nil {
		t.Fatal("intermittent failure: Reconcile() error = nil")
	}
	repository.mu.Lock()
	repository.err = nil
	repository.mu.Unlock()
	now = now.Add(time.Minute)
	if err := cache.Reconcile(context.Background()); err != nil {
		t.Fatalf("intermittent recovery Reconcile() error = %v", err)
	}
	if !cache.Ready() || cache.consecutiveFailures.Load() != 0 {
		t.Fatalf("intermittent recovery flapped readiness: ready=%t failures=%d", cache.Ready(), cache.consecutiveFailures.Load())
	}

	// Use a high threshold to isolate the independent maximum-staleness path.
	staleCache, err := NewCacheWithReadiness(repository, redisClient, time.Minute, ReadinessSettings{
		MaxStaleness:              5 * time.Minute,
		ReconcileFailureThreshold: 999,
	})
	if err != nil {
		t.Fatalf("NewCacheWithReadiness(stale) error = %v", err)
	}
	staleNow := time.Date(2026, time.August, 19, 13, 0, 0, 0, time.UTC)
	staleCache.now = func() time.Time { return staleNow }
	if err := staleCache.Reconcile(context.Background()); err != nil {
		t.Fatalf("stale initial Reconcile() error = %v", err)
	}
	staleNow = staleNow.Add(5*time.Minute + time.Nanosecond)
	if staleCache.Ready() {
		t.Fatal("cache remained ready after maximum snapshot staleness")
	}
}

func TestPolicyIdentifierPreservesTenant(t *testing.T) {
	tenant := "tenant/a"
	identifier := PolicyIdentifier("policy/b", &tenant)
	policyName, gotTenant, err := parsePolicyIdentifier(identifier)
	if err != nil {
		t.Fatalf("parsePolicyIdentifier() error = %v", err)
	}
	if policyName != "policy/b" || gotTenant == nil || *gotTenant != tenant {
		t.Fatalf("parsed identifier name=%q tenant=%v", policyName, gotTenant)
	}
}

func TestCachePeriodicallyReconcilesFromPostgreSQL(t *testing.T) {
	repository := &cacheRepositoryStub{snapshots: []PolicySnapshot{completeActiveSnapshot("initial", 1)}}
	redisClient := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: time.Millisecond,
	})
	defer redisClient.Close()
	cache, err := NewCache(repository, redisClient, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cache.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	repository.setSnapshots(
		completeActiveSnapshot("initial", 1),
		completeActiveSnapshot("periodic", 1),
	)
	deadline := time.After(time.Second)
	for {
		if snapshot, found := cache.Get("periodic"); found && snapshot.Version == 1 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("periodic reconciliation did not load active snapshot")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func completeActiveSnapshot(policyName string, version int) PolicySnapshot {
	now := time.Now().UTC()
	return PolicySnapshot{
		ID:         int64(version),
		PolicyID:   1,
		PolicyName: policyName,
		Version:    &version,
		Status:     StatusActive,
		Definition: PolicyDefinition{
			Request: RequestPolicy{
				PII:              ActionMask,
				Secret:           ActionBlock,
				PromptInjection:  ActionAuditOnly,
				CustomPatternIDs: []string{"1"},
			},
		},
		IntegrityHash: "sha256:test",
		CompiledAt:    &now,
		ActivatedAt:   &now,
	}
}
