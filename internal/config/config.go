package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	DBDSN            string
	RedisURL         string
	PIIMode          string
	ServerPort       string
	AIModelURL       string
	AIAPIKey         string
	AIModelName      string
	Features         FeatureFlags
	GatewayBlockMode string
	AppMode          string

	// AI Provider settings
	// Supported values: "OPENAI_COMPATIBLE" (default), "BEDROCK"
	AIProvider string

	// AWS Bedrock settings (only used when AIProvider is "BEDROCK")
	// Region is required when using Bedrock (e.g., "us-east-1", "eu-central-1")
	BedrockRegion string
	// EndpointOverride is optional; use for custom endpoints (VPC endpoints, testing)
	BedrockEndpointOverride string
	// ModelID is the Bedrock model identifier (e.g., "anthropic.claude-3-sonnet-20240229-v1:0")
	BedrockModelID string

	// Streaming / gateway settings
	// Maximum size of the in-memory buffer used for streaming output guardrails (in bytes).
	// If zero or negative, no explicit limit is enforced.
	StreamMaxBufferBytes int
	// Behaviour when streaming events cannot be parsed or other non-guardrail errors occur.
	// Supported values: "LENIENT" (default), "STRICT".
	StreamFailMode string

	// ===== MILESTONE 1: HTTP SECURITY HARDENING =====

	// Security Headers
	SecurityHeadersEnabled bool
	// CORS Configuration
	CORSEnabled        bool
	CORSAllowedOrigins string // CSV: "https://api.example.com,https://example.com"
	CORSAllowedMethods string // CSV: "GET,POST,OPTIONS" (default)
	CORSAllowedHeaders string // CSV: "Content-Type,Authorization" (default)
	CORSMaxAge         string // seconds (default: "300")

	// Request Limits
	MaxRequestSizeBytes int64 // bytes (default: 10485760 = 10MB)

	// Timeouts
	HandlerTimeoutDetectSeconds int // /detect endpoint timeout (default: 30)
	HandlerTimeoutChatSeconds   int // /v1/chat/completions endpoint timeout (default: 300)
	HTTPReadTimeoutSeconds      int // HTTP read timeout (default: 15)
	HTTPWriteTimeoutSeconds     int // HTTP write timeout (default: 15)
	HTTPIdleTimeoutSeconds      int // HTTP idle/keep-alive timeout (default: 60)
	HTTPMaxHeaderBytes          int // HTTP max header bytes (default: 1MB)

	// Input Validation
	ValidateContentType bool // Validate Content-Type: application/json on POST/PUT/PATCH (default: true)

	// Authentication & Authorization
	AuthEnabled            bool
	AuthTokenPermissions   string // CSV: token=perm1|perm2|perm3
	AuthPublicPaths        string // CSV: /healthz,/ready
	AuthRequireBearerToken bool

	// Rate limiting
	RateLimitEnabled           bool
	RateLimitRequestsPerSecond int
	RateLimitBurst             int
	RateLimitDetectPerMinute   int
	RateLimitChatPerMinute     int
	RateLimitPatternsPerMinute int
	RateLimitAdminPerMinute    int
}

type FeatureFlags struct {
	SemanticAnalysisEnabled bool
	SchemaValidationEnabled bool
}

var AppConfig *Config

func LoadConfig() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, relying on environment variables")
	}

	AppConfig = &Config{
		DBDSN:            getEnv("DB_DSN", "postgres://postgres:postgres@localhost:5432/thyris?sslmode=disable&TimeZone=Europe/Istanbul"),
		RedisURL:         getEnv("REDIS_URL", "redis://:thyrisredis@localhost:6379/0"),
		PIIMode:          getEnv("PII_MODE", "MASK"),
		ServerPort:       getEnv("SERVER_PORT", "8080"),
		GatewayBlockMode: strings.ToUpper(getEnv("GATEWAY_BLOCK_MODE", "BLOCK")),
		AppMode:          strings.ToUpper(getEnv("APP_MODE", "DEV")),
		AIModelURL:       getEnv("AI_MODEL_URL", "http://localhost:11434/v1"),
		AIAPIKey:         getEnv("AI_API_KEY", "ollama"), // Default to 'ollama' for local instances
		AIModelName:      getEnv("AI_MODEL", "llama3"),

		// AI Provider: OPENAI_COMPATIBLE (default) or BEDROCK
		AIProvider: strings.ToUpper(getEnv("AI_PROVIDER", "OPENAI_COMPATIBLE")),

		// AWS Bedrock settings
		BedrockRegion:           getEnv("AWS_BEDROCK_REGION", ""),
		BedrockEndpointOverride: getEnv("AWS_BEDROCK_ENDPOINT_OVERRIDE", ""),
		BedrockModelID:          getEnv("AWS_BEDROCK_MODEL_ID", "anthropic.claude-3-sonnet-20240229-v1:0"),

		Features: FeatureFlags{
			SemanticAnalysisEnabled: getEnvAsBool("FEATURE_AI_SEMANTIC_ANALYSIS", true),
			SchemaValidationEnabled: getEnvAsBool("FEATURE_JSON_SCHEMA_VALIDATION", true),
		},
		StreamMaxBufferBytes: getEnvAsInt("STREAM_MAX_BUFFER_BYTES", 262144),
		StreamFailMode:       strings.ToUpper(getEnv("STREAM_FAIL_MODE", "LENIENT")),

		// ===== MILESTONE 1: HTTP SECURITY HARDENING =====
		SecurityHeadersEnabled: getEnvAsBool("SECURITY_HEADERS_ENABLED", true),

		// CORS Configuration
		CORSEnabled:        getEnvAsBool("CORS_ENABLED", true),
		CORSAllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", ""), // Empty = deny-all (fail-secure)
		CORSAllowedMethods: getEnv("CORS_ALLOWED_METHODS", "GET,POST,OPTIONS"),
		CORSAllowedHeaders: getEnv("CORS_ALLOWED_HEADERS", "Content-Type,Authorization"),
		CORSMaxAge:         getEnv("CORS_MAX_AGE", "300"),

		// Request Limits
		MaxRequestSizeBytes: int64(getEnvAsInt("MAX_REQUEST_SIZE_BYTES", 10485760)), // 10 MB default

		// Timeouts
		HandlerTimeoutDetectSeconds: getEnvAsInt("HANDLER_TIMEOUT_DETECT_SECONDS", 30),
		HandlerTimeoutChatSeconds:   getEnvAsInt("HANDLER_TIMEOUT_CHAT_SECONDS", 300),
		HTTPReadTimeoutSeconds:      getEnvAsInt("HTTP_READ_TIMEOUT_SECONDS", 15),
		HTTPWriteTimeoutSeconds:     getEnvAsInt("HTTP_WRITE_TIMEOUT_SECONDS", 15),
		HTTPIdleTimeoutSeconds:      getEnvAsInt("HTTP_IDLE_TIMEOUT_SECONDS", 60),
		HTTPMaxHeaderBytes:          getEnvAsInt("HTTP_MAX_HEADER_BYTES", 1<<20),

		// Input Validation
		ValidateContentType: getEnvAsBool("VALIDATE_CONTENT_TYPE", true),

		// Authentication & Authorization
		AuthEnabled:            getEnvAsBool("AUTH_ENABLED", false),
		AuthTokenPermissions:   getEnv("AUTH_TOKEN_PERMISSIONS", ""),
		AuthPublicPaths:        getEnv("AUTH_PUBLIC_PATHS", "/healthz,/ready,/auth/login,/auth/logout,/auth/me"),
		AuthRequireBearerToken: getEnvAsBool("AUTH_REQUIRE_BEARER_TOKEN", true),

		// Rate limiting
		RateLimitEnabled:           getEnvAsBool("RATELIMIT_ENABLED", true),
		RateLimitRequestsPerSecond: getEnvAsInt("RATELIMIT_REQUESTS_PER_SECOND", 100),
		RateLimitBurst:             getEnvAsInt("RATELIMIT_BURST", 1000),
		RateLimitDetectPerMinute:   getEnvAsInt("RATELIMIT_DETECT_PER_MINUTE", 1000),
		RateLimitChatPerMinute:     getEnvAsInt("RATELIMIT_CHAT_PER_MINUTE", 100),
		RateLimitPatternsPerMinute: getEnvAsInt("RATELIMIT_PATTERNS_PER_MINUTE", 50),
		RateLimitAdminPerMinute:    getEnvAsInt("RATELIMIT_ADMIN_PER_MINUTE", 10),
	}
}

func getEnvAsBool(key string, fallback bool) bool {
	val := getEnv(key, "")
	if val == "true" || val == "1" || val == "TRUE" {
		return true
	}
	if val == "false" || val == "0" || val == "FALSE" {
		return false
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	val := getEnv(key, "")
	if val == "" {
		return fallback
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		log.Printf("Invalid int value for %s: %s (using fallback %d)", key, val, fallback)
		return fallback
	}
	return i
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func GetDSN() string {
	return AppConfig.DBDSN
}

func GetRedisURL() string {
	return AppConfig.RedisURL
}
