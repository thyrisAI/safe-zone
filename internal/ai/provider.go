// Package ai provides AI provider abstractions for the TSZ gateway.
// It supports multiple backends (OpenAI-compatible, AWS Bedrock) through a unified interface.
package ai

import (
	"context"
	"errors"
	"fmt"
	"log"

	"thyris-sz/internal/config"
)

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents a request to a chat completion endpoint.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	TopP        float64       `json:"top_p,omitempty"`
	// Extra holds provider-specific fields that should be passed through.
	Extra map[string]interface{} `json:"-"`
}

// ChatChoice represents a single choice in a chat completion response.
type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatUsage represents token usage information.
type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse represents a response from a chat completion endpoint.
type ChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   ChatUsage    `json:"usage"`
}

// StreamEvent represents a single event in a streaming response.
type StreamEvent struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role    string `json:"role,omitempty"`
			Content string `json:"content,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// ChatProvider defines the interface for AI chat providers.
// Implementations must handle both streaming and non-streaming requests.
type ChatProvider interface {
	// Name returns the provider name for logging and identification.
	Name() string

	// Chat sends a non-streaming chat completion request and returns the response.
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)

	// ChatStream sends a streaming chat completion request.
	// It returns a channel that emits StreamEvent objects and an error channel.
	// The caller should read from both channels until they are closed.
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, <-chan error)

	// SupportsStreaming returns true if the provider supports streaming responses.
	SupportsStreaming() bool
}

// ProviderType represents the type of AI provider.
type ProviderType string

const (
	// ProviderOpenAICompatible represents OpenAI-compatible endpoints (OpenAI, Ollama, etc.)
	ProviderOpenAICompatible ProviderType = "OPENAI_COMPATIBLE"
	// ProviderBedrock represents AWS Bedrock native integration.
	ProviderBedrock ProviderType = "BEDROCK"
)

// ErrProviderNotConfigured is returned when the requested provider is not properly configured.
var ErrProviderNotConfigured = errors.New("AI provider not configured")

// ErrStreamingNotSupported is returned when streaming is requested but not supported.
var ErrStreamingNotSupported = errors.New("streaming not supported by this provider")

// ErrInvalidRequest is returned when the request is invalid.
var ErrInvalidRequest = errors.New("invalid chat request")

// globalProvider holds the singleton provider instance.
var globalProvider ChatProvider

// InitProvider initializes the global ChatProvider based on configuration.
// This should be called once during application startup after config is loaded.
func InitProvider() error {
	cfg := config.AppConfig
	if cfg == nil {
		return errors.New("config not loaded")
	}

	providerType := ProviderType(cfg.AIProvider)
	log.Printf("[ai] Initializing AI provider: %s", providerType)

	switch providerType {
	case ProviderBedrock:
		provider, err := NewBedrockProvider(BedrockConfig{
			Region:           cfg.BedrockRegion,
			EndpointOverride: cfg.BedrockEndpointOverride,
			ModelID:          cfg.BedrockModelID,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize Bedrock provider: %w", err)
		}
		globalProvider = provider
		log.Printf("[ai] Bedrock provider initialized: region=%s model=%s", cfg.BedrockRegion, cfg.BedrockModelID)

	case ProviderOpenAICompatible:
		fallthrough
	default:
		// Default to OpenAI-compatible provider
		provider := NewOpenAIProvider(OpenAIConfig{
			BaseURL: cfg.AIModelURL,
			APIKey:  cfg.AIAPIKey,
			Model:   cfg.AIModelName,
		})
		globalProvider = provider
		log.Printf("[ai] OpenAI-compatible provider initialized: url=%s model=%s", cfg.AIModelURL, cfg.AIModelName)
	}

	return nil
}

// GetProvider returns the global ChatProvider instance.
// Returns nil if InitProvider has not been called.
func GetProvider() ChatProvider {
	return globalProvider
}

// SetProvider sets the global ChatProvider instance.
// This is primarily useful for testing.
func SetProvider(p ChatProvider) {
	globalProvider = p
}
