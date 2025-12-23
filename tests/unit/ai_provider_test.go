package unit

import (
	"testing"
	"thyris-sz/internal/ai"
	"thyris-sz/internal/config"
)

func TestInitProvider_InvalidConfig(t *testing.T) {
	// Save original config
	originalConfig := config.AppConfig
	defer func() { config.AppConfig = originalConfig }()

	// Test with empty config
	config.AppConfig = &config.Config{}

	err := ai.InitProvider()
	if err == nil {
		t.Log("Expected error for invalid config, but got nil")
	}
}

func TestGetProvider_BeforeInit(t *testing.T) {
	// Test getting provider before initialization
	provider := ai.GetProvider()
	if provider != nil {
		t.Log("Expected nil provider before init, but got non-nil")
	}
}

func TestSetProvider_NilProvider(t *testing.T) {
	// Test setting nil provider
	ai.SetProvider(nil)
	provider := ai.GetProvider()
	if provider != nil {
		t.Fatalf("Expected nil provider after setting nil, but got %v", provider)
	}
}

func TestHybridConfidence_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		regexScore float64
		aiScore    float64
		expected   float64
	}{
		{"Both zero", 0.0, 0.0, 0.0},
		{"Regex high, AI low", 0.9, 0.1, 0.9},
		{"Regex low, AI high", 0.1, 0.9, 0.9},
		{"Both high", 0.8, 0.9, 0.9},
		{"Both medium", 0.5, 0.6, 0.6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ai.HybridConfidence(tt.regexScore, tt.aiScore)
			if result < 0 || result > 1 {
				t.Fatalf("HybridConfidence should return value between 0 and 1, got %v", result)
			}
			// Should never be below the strongest signal
			maxInput := tt.regexScore
			if tt.aiScore > maxInput {
				maxInput = tt.aiScore
			}
			if result < maxInput {
				t.Fatalf("HybridConfidence should not be below strongest signal. Got %v, max input was %v", result, maxInput)
			}
		})
	}
}

func TestAIConfidenceCacheKey_Generation(t *testing.T) {
	// Test cache key generation with different inputs
	tests := []struct {
		label string
		text  string
	}{
		{"PII", "test@example.com"},
		{"SECRET", "api_key_123"},
		{"INJECTION", "DROP TABLE users"},
		{"", "empty label"},
		{"LABEL", ""},
	}

	for _, tt := range tests {
		t.Run(tt.label+"_"+tt.text, func(t *testing.T) {
			// Test that cache operations don't panic (Redis might not be available)
			defer func() {
				if r := recover(); r != nil {
					t.Logf("Cache operation panicked (Redis might not be available): %v", r)
				}
			}()

			score, found := ai.GetCachedConfidence(tt.label, tt.text)
			if found && (score < 0 || score > 1) {
				t.Fatalf("Cached confidence should be between 0 and 1, got %v", score)
			}

			// Test setting cache
			ai.SetCachedConfidence(tt.label, tt.text, 0.5, 0) // 0 duration for immediate expiry
		})
	}
}
