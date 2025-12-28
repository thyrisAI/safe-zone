package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// OpenAIConfig holds configuration for the OpenAI-compatible provider.
type OpenAIConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

// OpenAIProvider implements ChatProvider for OpenAI-compatible endpoints.
// This includes OpenAI, Ollama, Azure OpenAI, and other compatible services.
type OpenAIProvider struct {
	config OpenAIConfig
	client *http.Client
}

// NewOpenAIProvider creates a new OpenAI-compatible provider.
func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	return &OpenAIProvider{
		config: cfg,
		client: &http.Client{Timeout: timeout},
	}
}

// Name returns the provider name.
func (p *OpenAIProvider) Name() string {
	return "openai-compatible"
}

// SupportsStreaming returns true as OpenAI-compatible endpoints support streaming.
func (p *OpenAIProvider) SupportsStreaming() bool {
	return true
}

// Chat sends a non-streaming chat completion request.
func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// Build the request body
	body := p.buildRequestBody(req, false)

	httpReq, err := p.createHTTPRequest(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		log.Printf("[openai] Request failed: %v", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[openai] Non-200 response: %d - %s", resp.StatusCode, string(bodyBytes))
		return nil, fmt.Errorf("upstream returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &chatResp, nil
}

// ChatStream sends a streaming chat completion request.
func (p *OpenAIProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, <-chan error) {
	eventCh := make(chan StreamEvent, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		body := p.buildRequestBody(req, true)

		httpReq, err := p.createHTTPRequest(ctx, body)
		if err != nil {
			errCh <- fmt.Errorf("failed to create request: %w", err)
			return
		}

		resp, err := p.client.Do(httpReq)
		if err != nil {
			errCh <- fmt.Errorf("request failed: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("upstream returned status %d: %s", resp.StatusCode, string(bodyBytes))
			return
		}

		reader := bufio.NewReader(resp.Body)
		for {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			default:
			}

			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					errCh <- fmt.Errorf("error reading stream: %w", err)
				}
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}

			var event StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				log.Printf("[openai] Failed to parse SSE event: %v", err)
				continue
			}

			eventCh <- event
		}
	}()

	return eventCh, errCh
}

// buildRequestBody constructs the JSON request body for the OpenAI API.
func (p *OpenAIProvider) buildRequestBody(req ChatRequest, stream bool) map[string]interface{} {
	messages := make([]map[string]string, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}

	model := req.Model
	if model == "" {
		model = p.config.Model
	}

	body := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   stream,
	}

	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		body["top_p"] = req.TopP
	}

	// Merge extra fields
	for k, v := range req.Extra {
		if _, exists := body[k]; !exists {
			body[k] = v
		}
	}

	return body
}

// createHTTPRequest creates an HTTP request for the OpenAI API.
func (p *OpenAIProvider) createHTTPRequest(ctx context.Context, body map[string]interface{}) (*http.Request, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(p.config.BaseURL, "/") + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	return req, nil
}

// ForwardRequest forwards a raw OpenAI-compatible request to the upstream endpoint.
// This is used by the gateway to proxy requests with minimal transformation.
func (p *OpenAIProvider) ForwardRequest(ctx context.Context, payload map[string]interface{}) (*http.Response, error) {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	url := strings.TrimRight(p.config.BaseURL, "/") + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	// Use a client with ResponseHeaderTimeout to prevent hanging on initial connection,
	// but allow long-running body reading for streaming.
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
	}
	return client.Do(req)
}

// Ensure OpenAIProvider implements ChatProvider
var _ ChatProvider = (*OpenAIProvider)(nil)

// OpenAIForwarder is an interface for providers that can forward raw requests.
type OpenAIForwarder interface {
	ForwardRequest(ctx context.Context, payload map[string]interface{}) (*http.Response, error)
}

// Ensure OpenAIProvider implements OpenAIForwarder
var _ OpenAIForwarder = (*OpenAIProvider)(nil)

// AsOpenAIForwarder attempts to cast a ChatProvider to OpenAIForwarder.
// Returns nil if the provider does not support forwarding.
func AsOpenAIForwarder(p ChatProvider) OpenAIForwarder {
	if f, ok := p.(OpenAIForwarder); ok {
		return f
	}
	return nil
}

// ErrForwardingNotSupported is returned when a provider does not support request forwarding.
var ErrForwardingNotSupported = errors.New("provider does not support request forwarding")
