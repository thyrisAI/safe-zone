package policy

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestCacheLoadsPublishesAndReconcilesWithExistingRedisClient(t *testing.T) {
	redisURL := os.Getenv("TSZ_POLICY_TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("TSZ_POLICY_TEST_REDIS_URL is not set")
	}
	_, repository := openCompilerTestRepository(t)
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse Redis URL: %v", err)
	}
	redisClient := redis.NewClient(options)
	defer redisClient.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis ping: %v", err)
	}

	startupID := createCompiledSnapshot(t, ctx, repository, "startup-policy")
	startupActivator, err := NewActivator(repository, &recordingPublisher{})
	if err != nil {
		t.Fatalf("NewActivator() error = %v", err)
	}
	if err := startupActivator.Activate(ctx, startupID); err != nil {
		t.Fatalf("activate startup policy: %v", err)
	}

	cache, err := NewCache(repository, redisClient, time.Hour)
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	if cache.Ready() {
		t.Fatal("cache ready before startup full load")
	}
	if err := cache.Start(ctx); err != nil {
		t.Fatalf("Cache.Start() error = %v", err)
	}
	if !cache.Ready() {
		t.Fatal("cache not ready after successful startup full load")
	}
	if snapshot, found := cache.Get("startup-policy"); !found || snapshot.Version != 1 {
		t.Fatalf("startup cache snapshot = %+v found=%v", snapshot, found)
	}

	waitForSubscriber(t, ctx, redisClient)
	publisher, err := NewRedisActivationPublisher(redisClient)
	if err != nil {
		t.Fatalf("NewRedisActivationPublisher() error = %v", err)
	}
	notifiedID := createCompiledSnapshot(t, ctx, repository, "notified-policy")
	notifiedActivator, err := NewActivator(repository, publisher)
	if err != nil {
		t.Fatalf("NewActivator() with Redis error = %v", err)
	}
	if err := notifiedActivator.Activate(ctx, notifiedID); err != nil {
		t.Fatalf("activate notified policy: %v", err)
	}
	waitForCachedVersion(t, ctx, cache, "notified-policy", 1)
	notifiedV2 := createCompiledSnapshot(t, ctx, repository, "notified-policy")
	if err := notifiedActivator.Activate(ctx, notifiedV2); err != nil {
		t.Fatalf("activate notified v2 policy: %v", err)
	}
	waitForCachedVersion(t, ctx, cache, "notified-policy", 2)
	v2Snapshot, err := repository.SnapshotByID(ctx, notifiedV2)
	if err != nil {
		t.Fatalf("load notified v2 snapshot: %v", err)
	}
	if err := notifiedActivator.Rollback(ctx, v2Snapshot.PolicyID); err != nil {
		t.Fatalf("rollback notified policy: %v", err)
	}
	waitForCachedVersion(t, ctx, cache, "notified-policy", 1)

	reconnectedID := createCompiledSnapshot(t, ctx, repository, "reconnected-policy")
	reconnectedActivator, err := NewActivator(repository, &recordingPublisher{})
	if err != nil {
		t.Fatalf("NewActivator() reconnect error = %v", err)
	}
	if err := reconnectedActivator.Activate(ctx, reconnectedID); err != nil {
		t.Fatalf("activate reconnect policy: %v", err)
	}
	if _, found := cache.Get("reconnected-policy"); found {
		t.Fatal("unpublished policy unexpectedly reached cache before reconnect")
	}
	if err := redisClient.Do(ctx, "CLIENT", "KILL", "TYPE", "pubsub").Err(); err != nil {
		t.Fatalf("kill Redis Pub/Sub connection: %v", err)
	}
	waitForCachedVersion(t, ctx, cache, "reconnected-policy", 1)
}

func waitForSubscriber(t *testing.T, ctx context.Context, client *redis.Client) {
	t.Helper()
	for {
		result, err := client.PubSubNumSub(ctx, RedisActivationChannel).Result()
		if err != nil {
			t.Fatalf("PUBSUB NUMSUB: %v", err)
		}
		if result[RedisActivationChannel] > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for policy Redis subscriber")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func waitForCachedVersion(t *testing.T, ctx context.Context, cache *Cache, policyID string, version int) {
	t.Helper()
	for {
		if snapshot, found := cache.Get(policyID); found && snapshot.Version == version {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for cached policy %s version %d", policyID, version)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
