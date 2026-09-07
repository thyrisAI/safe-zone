package cache

import (
	"context"
	"encoding/json"
	"log"
	"thyris-sz/internal/config"
	"thyris-sz/internal/models"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client
var ctx = context.Background()

// Cache keys
const (
	KeyPatterns  = "patterns:active"
	KeyAllowlist = "allowlist:all"
	KeyBlocklist = "blocklist:all"
)

func InitRedis() {
	opt, err := redis.ParseURL(config.GetRedisURL())
	if err != nil {
		log.Fatalf("Invalid Redis URL: %v", err)
	}

	RDB = redis.NewClient(opt)
	// Redis command arguments may contain policy data. Span names retain the
	// operation while WithDBStatement(false) prevents command text/arguments
	// from becoming telemetry attributes.
	if err := redisotel.InstrumentTracing(RDB, redisotel.WithDBStatement(false)); err != nil {
		log.Printf("Warning: failed to instrument Redis tracing: %v", err)
	}

	if err := RDB.Ping(ctx).Err(); err != nil {
		log.Printf("Failed to connect to Redis: %v", err)
	} else {
		log.Println("Redis connection established")
	}
}

// SetPatterns caches the patterns
func SetPatterns(patterns []models.Pattern) error {
	data, err := json.Marshal(patterns)
	if err != nil {
		return err
	}
	return RDB.Set(ctx, KeyPatterns, data, 1*time.Hour).Err()
}

// GetPatterns retrieves patterns from cache
func GetPatterns() ([]models.Pattern, error) {
	val, err := RDB.Get(ctx, KeyPatterns).Result()
	if err != nil {
		return nil, err
	}

	var patterns []models.Pattern
	err = json.Unmarshal([]byte(val), &patterns)
	return patterns, err
}

// SetAllowlist caches the allowlist map
func SetAllowlist(allowlist map[string]bool) error {
	data, err := json.Marshal(allowlist)
	if err != nil {
		return err
	}
	return RDB.Set(ctx, KeyAllowlist, data, 1*time.Hour).Err()
}

// GetAllowlist retrieves allowlist from cache
func GetAllowlist() (map[string]bool, error) {
	val, err := RDB.Get(ctx, KeyAllowlist).Result()
	if err != nil {
		return nil, err
	}

	var allowlist map[string]bool
	err = json.Unmarshal([]byte(val), &allowlist)
	return allowlist, err
}

// SetBlocklist caches the blocklist map
func SetBlocklist(blocklist map[string]bool) error {
	data, err := json.Marshal(blocklist)
	if err != nil {
		return err
	}
	return RDB.Set(ctx, KeyBlocklist, data, 1*time.Hour).Err()
}

// GetBlocklist retrieves blocklist from cache
func GetBlocklist() (map[string]bool, error) {
	val, err := RDB.Get(ctx, KeyBlocklist).Result()
	if err != nil {
		return nil, err
	}

	var blocklist map[string]bool
	err = json.Unmarshal([]byte(val), &blocklist)
	return blocklist, err
}

// ClearCache clears specific cache key
func ClearCache(key string) {
	RDB.Del(ctx, key)
}
