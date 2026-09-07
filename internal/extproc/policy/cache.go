package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

type CompiledSnapshot struct {
	PolicyID         string
	DatabasePolicyID int64
	Version          int
	Definition       PolicyDefinition
	IntegrityHash    string
	CompiledAt       time.Time
	ActivatedAt      time.Time
}

// Clone returns an independent snapshot value suitable for pinning to a
// request stream. Callers cannot mutate the cache or another stream through
// definition slices or pointers.
func (snapshot CompiledSnapshot) Clone() CompiledSnapshot {
	return cloneCompiledSnapshot(snapshot)
}

type cacheState struct {
	snapshots map[string]CompiledSnapshot
}

const (
	defaultPolicyMaxStaleness              = 5 * time.Minute
	defaultPolicyReconcileFailureThreshold = uint32(3)
)

// ReadinessSettings bound how long a processor may serve the last known good
// policy cache after reconciliation stops succeeding.
type ReadinessSettings struct {
	MaxStaleness              time.Duration
	ReconcileFailureThreshold uint32
}

func DefaultReadinessSettings() ReadinessSettings {
	return ReadinessSettings{
		MaxStaleness:              defaultPolicyMaxStaleness,
		ReconcileFailureThreshold: defaultPolicyReconcileFailureThreshold,
	}
}

func (settings ReadinessSettings) validate() error {
	if settings.MaxStaleness <= 0 {
		return errors.New("policy max staleness must be greater than zero")
	}
	if settings.ReconcileFailureThreshold == 0 {
		return errors.New("policy reconcile failure threshold must be greater than zero")
	}
	return nil
}

type Cache struct {
	repository          Repository
	redis               *redis.Client
	reconcileInterval   time.Duration
	readiness           ReadinessSettings
	state               atomic.Pointer[cacheState]
	ready               atomic.Bool
	started             atomic.Bool
	lastReconcileAt     atomic.Int64
	consecutiveFailures atomic.Uint32
	reconcileMu         sync.Mutex
	now                 func() time.Time
	observer            CacheObserver
}

// CacheObserver records bounded operational signals without exposing policy
// contents, IDs, tenants, or snapshot versions as metric labels.
type CacheObserver interface {
	ObservePolicyReconcile(result string, duration time.Duration)
	ObservePolicyActivationNotification(result string)
	SetActivePolicySnapshots(count int)
	SetPolicySnapshotVersions(snapshots []SnapshotVersion)
}

type SnapshotVersion struct {
	PolicyID string
	Version  int
}

type noopCacheObserver struct{}

func (noopCacheObserver) ObservePolicyReconcile(string, time.Duration) {}
func (noopCacheObserver) ObservePolicyActivationNotification(string)   {}
func (noopCacheObserver) SetActivePolicySnapshots(int)                 {}
func (noopCacheObserver) SetPolicySnapshotVersions([]SnapshotVersion)  {}

func NewCache(repository Repository, redisClient *redis.Client, reconcileInterval time.Duration) (*Cache, error) {
	return NewCacheWithReadiness(repository, redisClient, reconcileInterval, DefaultReadinessSettings())
}

func NewCacheWithReadiness(repository Repository, redisClient *redis.Client, reconcileInterval time.Duration, readiness ReadinessSettings) (*Cache, error) {
	if repository == nil {
		return nil, errors.New("policy repository is required")
	}
	if redisClient == nil {
		return nil, errors.New("existing Redis client is required")
	}
	if reconcileInterval <= 0 {
		return nil, errors.New("policy reconcile interval must be greater than zero")
	}
	if err := readiness.validate(); err != nil {
		return nil, err
	}
	cache := &Cache{
		repository:        repository,
		redis:             redisClient,
		reconcileInterval: reconcileInterval,
		readiness:         readiness,
		now:               time.Now,
		observer:          noopCacheObserver{},
	}
	cache.state.Store(&cacheState{snapshots: map[string]CompiledSnapshot{}})
	return cache, nil
}

// SetObserver must be called before Start. It exists to keep the policy cache
// independent from a particular Prometheus registry or transport package.
func (c *Cache) SetObserver(observer CacheObserver) {
	if observer == nil {
		observer = noopCacheObserver{}
	}
	c.observer = observer
}

// Start performs an immediate full PostgreSQL load, then starts periodic
// reconciliation and Redis subscription loops. An initial load failure keeps
// readiness false while background retries continue.
func (c *Cache) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !c.started.CompareAndSwap(false, true) {
		return errors.New("policy cache has already been started")
	}
	if err := c.Reconcile(ctx); err != nil {
		log.Printf("[DEGRADED] initial policy cache reconciliation failed; readiness=false error=%v", err)
	}
	go c.periodicReconciliation(ctx)
	go c.subscriptionLoop(ctx)
	return nil
}

func (c *Cache) Ready() bool {
	if !c.ready.Load() || c.consecutiveFailures.Load() >= c.readiness.ReconcileFailureThreshold {
		return false
	}
	lastReconcileAt := c.lastReconcileAt.Load()
	if lastReconcileAt == 0 {
		return false
	}
	return c.now().Sub(time.Unix(0, lastReconcileAt)) <= c.readiness.MaxStaleness
}

// Get returns a deep copy. A request can retain this value for its complete
// lifetime while later cache swaps proceed independently.
func (c *Cache) Get(policyID string) (CompiledSnapshot, bool) {
	state := c.state.Load()
	snapshot, found := state.snapshots[policyID]
	if !found {
		return CompiledSnapshot{}, false
	}
	return cloneCompiledSnapshot(snapshot), true
}

// Snapshots returns a deep-copied view of the active policy versions held by
// this process. It is suitable for operational readiness diagnostics only.
func (c *Cache) Snapshots() []CompiledSnapshot {
	state := c.state.Load()
	snapshots := make([]CompiledSnapshot, 0, len(state.snapshots))
	for _, snapshot := range state.snapshots {
		snapshots = append(snapshots, cloneCompiledSnapshot(snapshot))
	}
	return snapshots
}

// Reconcile atomically replaces the complete cache from PostgreSQL. Readiness
// becomes true only after the first successful full load.
func (c *Cache) Reconcile(ctx context.Context) (err error) {
	started := time.Now()
	c.reconcileMu.Lock()
	defer c.reconcileMu.Unlock()
	defer func() {
		if err != nil {
			c.consecutiveFailures.Add(1)
			c.observer.ObservePolicyReconcile("error", time.Since(started))
			return
		}
		c.lastReconcileAt.Store(c.now().UTC().UnixNano())
		c.consecutiveFailures.Store(0)
		c.observer.ObservePolicyReconcile("success", time.Since(started))
	}()

	snapshots, err := c.repository.ActiveSnapshots(ctx)
	if err != nil {
		return fmt.Errorf("load active policy snapshots: %w", err)
	}
	next := make(map[string]CompiledSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		compiled, err := immutableSnapshot(snapshot)
		if err != nil {
			return err
		}
		if _, duplicate := next[compiled.PolicyID]; duplicate {
			return fmt.Errorf("duplicate active policy identifier %q", compiled.PolicyID)
		}
		next[compiled.PolicyID] = compiled
	}
	c.state.Store(&cacheState{snapshots: next})
	c.ready.Store(true)
	c.observer.SetActivePolicySnapshots(len(next))
	c.observer.SetPolicySnapshotVersions(snapshotVersions(next))
	return nil
}

func (c *Cache) periodicReconciliation(ctx context.Context) {
	ticker := time.NewTicker(c.reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Reconcile(ctx); err != nil && ctx.Err() == nil {
				log.Printf("[DEGRADED] periodic policy cache reconciliation failed: %v", err)
			}
		}
	}
}

func (c *Cache) subscriptionLoop(ctx context.Context) {
	for ctx.Err() == nil {
		if err := c.receiveSubscription(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[DEGRADED] policy activation subscription disconnected; retrying with full reconciliation: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (c *Cache) receiveSubscription(ctx context.Context) error {
	pubsub := c.redis.Subscribe(ctx, RedisActivationChannel)
	defer pubsub.Close()
	events := pubsub.ChannelWithSubscriptions()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case received, open := <-events:
			if !open {
				return errors.New("policy activation subscription closed")
			}
			switch message := received.(type) {
			case *redis.Subscription:
				// ChannelWithSubscriptions emits this event again after go-redis
				// reconnects and restores the subscription.
				if message.Kind == "subscribe" {
					if err := c.Reconcile(ctx); err != nil {
						// The periodic reconciler is the bounded retry path for a
						// failed PostgreSQL load. Keeping the healthy subscription
						// open avoids a tight reconnect loop consuming the readiness
						// failure threshold faster than its configured interval.
						log.Printf("[DEGRADED] full reconciliation after Redis subscribe failed; keeping subscription open: %v", err)
					}
				}
			case *redis.Message:
				if err := c.handleActivationMessage(ctx, message.Payload); err != nil {
					log.Printf("[DEGRADED] rejected policy activation notification: %v", err)
				}
			}
		}
	}
}

func (c *Cache) handleActivationMessage(ctx context.Context, payload string) error {
	result := "success"
	defer func() { c.observer.ObservePolicyActivationNotification(result) }()
	var event ActivationEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		result = "rejected"
		return fmt.Errorf("decode activation notification: %w", err)
	}
	if event.PolicyID == "" || event.Version <= 0 {
		result = "rejected"
		return fmt.Errorf("invalid activation notification: %+v", event)
	}
	policyName, tenant, err := parsePolicyIdentifier(event.PolicyID)
	if err != nil {
		result = "rejected"
		return err
	}
	snapshot, err := c.repository.ActiveSnapshot(ctx, policyName, tenant)
	if err != nil {
		result = "reload_failed"
		return fmt.Errorf("reload activated policy %q: %w", event.PolicyID, err)
	}
	compiled, err := immutableSnapshot(snapshot)
	if err != nil {
		result = "rejected"
		return err
	}
	if compiled.Version < event.Version {
		result = "behind"
		return fmt.Errorf("PostgreSQL policy %q version %d is behind notification version %d", event.PolicyID, compiled.Version, event.Version)
	}
	c.swapOne(event.PolicyID, compiled)
	c.observer.SetActivePolicySnapshots(len(c.state.Load().snapshots))
	c.observer.SetPolicySnapshotVersions(snapshotVersions(c.state.Load().snapshots))
	return nil
}

func snapshotVersions(snapshots map[string]CompiledSnapshot) []SnapshotVersion {
	versions := make([]SnapshotVersion, 0, len(snapshots))
	for policyID, snapshot := range snapshots {
		versions = append(versions, SnapshotVersion{PolicyID: policyID, Version: snapshot.Version})
	}
	return versions
}

func (c *Cache) swapOne(policyID string, snapshot CompiledSnapshot) {
	c.reconcileMu.Lock()
	defer c.reconcileMu.Unlock()
	current := c.state.Load()
	next := make(map[string]CompiledSnapshot, len(current.snapshots)+1)
	for key, value := range current.snapshots {
		next[key] = value
	}
	next[policyID] = snapshot
	c.state.Store(&cacheState{snapshots: next})
}

func immutableSnapshot(snapshot PolicySnapshot) (CompiledSnapshot, error) {
	if snapshot.Status != StatusActive || snapshot.Version == nil || snapshot.CompiledAt == nil || snapshot.ActivatedAt == nil || snapshot.IntegrityHash == "" {
		return CompiledSnapshot{}, fmt.Errorf("policy snapshot %d is not a complete active snapshot", snapshot.ID)
	}
	policyID := PolicyIdentifier(snapshot.PolicyName, snapshot.Tenant)
	return CompiledSnapshot{
		PolicyID:         policyID,
		DatabasePolicyID: snapshot.PolicyID,
		Version:          *snapshot.Version,
		Definition:       cloneDefinition(snapshot.Definition),
		IntegrityHash:    snapshot.IntegrityHash,
		CompiledAt:       *snapshot.CompiledAt,
		ActivatedAt:      *snapshot.ActivatedAt,
	}, nil
}

func cloneCompiledSnapshot(snapshot CompiledSnapshot) CompiledSnapshot {
	snapshot.Definition = cloneDefinition(snapshot.Definition)
	return snapshot
}

func cloneDefinition(definition PolicyDefinition) PolicyDefinition {
	clone := definition
	if definition.Scope.Tenant != nil {
		tenant := *definition.Scope.Tenant
		clone.Scope.Tenant = &tenant
	}
	clone.Request.CustomPatternIDs = append([]string(nil), definition.Request.CustomPatternIDs...)
	clone.Request.AllowlistIDs = append([]string(nil), definition.Request.AllowlistIDs...)
	clone.Request.BlocklistIDs = append([]string(nil), definition.Request.BlocklistIDs...)
	clone.Request.CustomValidators = append([]ValidatorReference(nil), definition.Request.CustomValidators...)
	clone.Request.CompiledRules.CustomPatterns = append([]CompiledPattern(nil), definition.Request.CompiledRules.CustomPatterns...)
	clone.Request.CompiledRules.Allowlist = append([]CompiledList(nil), definition.Request.CompiledRules.Allowlist...)
	clone.Request.CompiledRules.Blocklist = append([]CompiledList(nil), definition.Request.CompiledRules.Blocklist...)
	clone.Request.CompiledRules.Validators = append([]CompiledValidator(nil), definition.Request.CompiledRules.Validators...)
	clone.Response.CustomPatternIDs = append([]string(nil), definition.Response.CustomPatternIDs...)
	clone.Response.CustomValidators = append([]ValidatorReference(nil), definition.Response.CustomValidators...)
	clone.Response.CompiledRules.CustomPatterns = append([]CompiledPattern(nil), definition.Response.CompiledRules.CustomPatterns...)
	clone.Response.CompiledRules.Validators = append([]CompiledValidator(nil), definition.Response.CompiledRules.Validators...)
	return clone
}

func PolicyIdentifier(policyName string, tenant *string) string {
	if tenant == nil {
		return policyName
	}
	return "tenant/" + url.PathEscape(*tenant) + "/policy/" + url.PathEscape(policyName)
}

func parsePolicyIdentifier(identifier string) (string, *string, error) {
	if !strings.HasPrefix(identifier, "tenant/") {
		return identifier, nil, nil
	}
	parts := strings.SplitN(strings.TrimPrefix(identifier, "tenant/"), "/policy/", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid tenant policy identifier %q", identifier)
	}
	tenant, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", nil, fmt.Errorf("decode tenant policy identifier: %w", err)
	}
	policyName, err := url.PathUnescape(parts[1])
	if err != nil {
		return "", nil, fmt.Errorf("decode policy identifier: %w", err)
	}
	return policyName, &tenant, nil
}
