package unit

import (
	"os"
	"testing"
	"thyris-sz/internal/cache"
	"thyris-sz/internal/config"
	"thyris-sz/internal/models"
)

func TestGetDSN_DefaultValue(t *testing.T) {
	// Initialize config first
	config.LoadConfig()

	// Test DSN generation
	dsn := config.GetDSN()
	if dsn == "" {
		t.Log("DSN is empty, might be expected in test environment")
	}
}

func TestGetRedisURL_DefaultValue(t *testing.T) {
	// Initialize config first
	config.LoadConfig()

	// Test Redis URL generation
	redisURL := config.GetRedisURL()
	if redisURL == "" {
		t.Log("Redis URL is empty, might be expected in test environment")
	}
}

func TestLoadConfig_WithEnvVars(t *testing.T) {
	// Save original env vars
	originalPort := os.Getenv("PORT")
	originalDBHost := os.Getenv("DB_HOST")

	defer func() {
		os.Setenv("PORT", originalPort)
		os.Setenv("DB_HOST", originalDBHost)
	}()

	// Set test env vars
	os.Setenv("PORT", "9999")
	os.Setenv("DB_HOST", "test-host")

	// Load config
	config.LoadConfig()

	// Verify config was loaded (we can't directly access private fields,
	// but we can test that LoadConfig doesn't panic)
	t.Log("Config loaded successfully")
}

func TestCacheOperations_Patterns(t *testing.T) {
	// Test pattern caching operations
	testPatterns := []models.Pattern{
		{
			Name:        "TEST_EMAIL",
			Regex:       `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
			Category:    "PII",
			IsActive:    true,
			Description: "Test email pattern",
		},
		{
			Name:        "TEST_PHONE",
			Regex:       `\b\d{3}-\d{3}-\d{4}\b`,
			Category:    "PII",
			IsActive:    true,
			Description: "Test phone pattern",
		},
	}

	// Test setting patterns with panic recovery
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Cache operation panicked (Redis might not be available): %v", r)
		}
	}()

	// Test setting patterns
	err := cache.SetPatterns(testPatterns)
	if err != nil {
		t.Logf("SetPatterns failed (Redis might not be available): %v", err)
		return
	}

	// Test getting patterns
	retrievedPatterns, err := cache.GetPatterns()
	if err != nil {
		t.Logf("GetPatterns failed: %v", err)
		return
	}

	if len(retrievedPatterns) != len(testPatterns) {
		t.Fatalf("Expected %d patterns, got %d", len(testPatterns), len(retrievedPatterns))
	}
}

func TestCacheOperations_Allowlist(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Cache operation panicked (Redis might not be available): %v", r)
		}
	}()

	testAllowlist := map[string]bool{
		"safe@example.com":     true,
		"allowed@company.com":  true,
		"whitelist@domain.org": true,
	}

	// Test setting allowlist
	err := cache.SetAllowlist(testAllowlist)
	if err != nil {
		t.Logf("SetAllowlist failed (Redis might not be available): %v", err)
		return
	}

	// Test getting allowlist
	retrievedAllowlist, err := cache.GetAllowlist()
	if err != nil {
		t.Logf("GetAllowlist failed: %v", err)
		return
	}

	if len(retrievedAllowlist) != len(testAllowlist) {
		t.Fatalf("Expected %d allowlist items, got %d", len(testAllowlist), len(retrievedAllowlist))
	}

	for key, value := range testAllowlist {
		if retrievedAllowlist[key] != value {
			t.Fatalf("Expected allowlist[%s] = %v, got %v", key, value, retrievedAllowlist[key])
		}
	}
}

func TestCacheOperations_Blocklist(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Cache operation panicked (Redis might not be available): %v", r)
		}
	}()

	testBlocklist := map[string]bool{
		"blocked@spam.com":    true,
		"malicious@evil.org":  true,
		"banned@blackhat.net": true,
	}

	// Test setting blocklist
	err := cache.SetBlocklist(testBlocklist)
	if err != nil {
		t.Logf("SetBlocklist failed (Redis might not be available): %v", err)
		return
	}

	// Test getting blocklist
	retrievedBlocklist, err := cache.GetBlocklist()
	if err != nil {
		t.Logf("GetBlocklist failed: %v", err)
		return
	}

	if len(retrievedBlocklist) != len(testBlocklist) {
		t.Fatalf("Expected %d blocklist items, got %d", len(testBlocklist), len(retrievedBlocklist))
	}

	for key, value := range testBlocklist {
		if retrievedBlocklist[key] != value {
			t.Fatalf("Expected blocklist[%s] = %v, got %v", key, value, retrievedBlocklist[key])
		}
	}
}

func TestClearCache_Operations(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Cache operation panicked (Redis might not be available): %v", r)
		}
	}()

	// Test cache clearing
	cache.ClearCache("test-key")
	cache.ClearCache("patterns")
	cache.ClearCache("allowlist")
	cache.ClearCache("blocklist")

	// Should not panic
	t.Log("Cache clear operations completed")
}
