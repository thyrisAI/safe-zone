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

	// Streaming / gateway settings
	// Maximum size of the in-memory buffer used for streaming output guardrails (in bytes).
	// If zero or negative, no explicit limit is enforced.
	StreamMaxBufferBytes int
	// Behaviour when streaming events cannot be parsed or other non-guardrail errors occur.
	// Supported values: "LENIENT" (default), "STRICT".
	StreamFailMode string
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
		AIModelURL:       getEnv("THYRIS_AI_MODEL_URL", "http://localhost:11434/v1"),
		AIAPIKey:         getEnv("THYRIS_AI_API_KEY", "ollama"), // Default to 'ollama' for local instances
		AIModelName:      getEnv("THYRIS_AI_MODEL", "llama3"),
		Features: FeatureFlags{
			SemanticAnalysisEnabled: getEnvAsBool("FEATURE_AI_SEMANTIC_ANALYSIS", true),
			SchemaValidationEnabled: getEnvAsBool("FEATURE_JSON_SCHEMA_VALIDATION", true),
		},
		StreamMaxBufferBytes: getEnvAsInt("STREAM_MAX_BUFFER_BYTES", 262144),
		StreamFailMode:       strings.ToUpper(getEnv("STREAM_FAIL_MODE", "LENIENT")),
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
