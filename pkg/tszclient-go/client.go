package tszclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config holds client configuration for talking to a TSZ gateway.
//
// BaseURL should point to the TSZ HTTP endpoint, for example:
//   - http://localhost:8080
//   - https://tsz-gateway.your-company.com
//
// Optional HTTPClient can be provided to customize timeouts, proxies, etc.
// If nil, a default client with 60s timeout will be used.
type Config struct {
	BaseURL    string
	APIKey     string // Optional Admin API Key
	HTTPClient *http.Client
}

// Client is a lightweight TSZ API client.
type Client struct {
	baseURL    *url.URL
	apiKey     string
	httpClient *http.Client
}

// New creates a new TSZ client with the given configuration.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("BaseURL is required")
	}

	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid BaseURL: %w", err)
	}

	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}

	return &Client{
		baseURL:    u,
		apiKey:     cfg.APIKey,
		httpClient: hc,
	}, nil
}

// DetectRequest mirrors the TSZ /detect request payload.
type DetectRequest struct {
	Text           string   `json:"text"`
	RID            string   `json:"rid,omitempty"`
	ExpectedFormat string   `json:"expected_format,omitempty"`
	Guardrails     []string `json:"guardrails,omitempty"`
}

// DetectionResult is a single detection in the TSZ response.
type DetectionResult struct {
	Type                  string                 `json:"type"`
	Value                 string                 `json:"value"`
	Placeholder           string                 `json:"placeholder"`
	Start                 int                    `json:"start"`
	End                   int                    `json:"end"`
	ConfidenceScore       string                 `json:"confidence_score"`
	ConfidenceExplanation map[string]interface{} `json:"confidence_explanation,omitempty"`
}

// ValidatorResult mirrors TSZ validator results.
type ValidatorResult struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Passed          bool   `json:"passed"`
	ConfidenceScore string `json:"confidence_score"`
}

// DetectResponse mirrors the TSZ /detect response payload.
type DetectResponse struct {
	RedactedText      string            `json:"redacted_text,omitempty"`
	Detections        []DetectionResult `json:"detections,omitempty"`
	ValidatorResults  []ValidatorResult `json:"validator_results,omitempty"`
	Breakdown         map[string]int    `json:"breakdown"`
	Blocked           bool              `json:"blocked"`
	ContainsPII       bool              `json:"contains_pii"`
	OverallConfidence string            `json:"overall_confidence"`
	Message           string            `json:"message,omitempty"`
}

// APIError represents an HTTP/API level error returned by TSZ.
type APIError struct {
	StatusCode int
	Body       []byte
}

func (e *APIError) Error() string {
	if len(e.Body) == 0 {
		return fmt.Sprintf("tsz api error: status=%d", e.StatusCode)
	}
	return fmt.Sprintf("tsz api error: status=%d body=%s", e.StatusCode, string(e.Body))
}

// Detect calls the /detect endpoint of TSZ.
func (c *Client) Detect(ctx context.Context, req DetectRequest) (*DetectResponse, error) {
	return postJSON[DetectResponse](ctx, c, "/detect", req, nil)
}

// DetectOption configures a DetectRequest for helper methods such as DetectText.
type DetectOption func(*DetectRequest)

// WithGuardrails appends one or more guardrail identifiers to the request.
//
// Example usage:
//
//	resp, err := client.DetectText(
//	    ctx,
//	    "Contact me at john@example.com",
//	    tszclient.WithGuardrails("TOXIC_LANGUAGE", "FINANCIAL_DATA"),
//	)
func WithGuardrails(guardrails ...string) DetectOption {
	return func(r *DetectRequest) {
		if len(guardrails) == 0 {
			return
		}
		r.Guardrails = append(r.Guardrails, guardrails...)
	}
}

// WithRID sets the RID on the DetectRequest.
func WithRID(rid string) DetectOption {
	return func(r *DetectRequest) {
		r.RID = rid
	}
}

// WithExpectedFormat sets the ExpectedFormat field on the DetectRequest.
func WithExpectedFormat(format string) DetectOption {
	return func(r *DetectRequest) {
		r.ExpectedFormat = format
	}
}

// DetectText is a small convenience wrapper around Detect.
//
// It builds a DetectRequest from a plain text string and applies any
// additional DetectOption helpers before delegating to Detect.
func (c *Client) DetectText(ctx context.Context, text string, opts ...DetectOption) (*DetectResponse, error) {
	req := DetectRequest{Text: text}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&req)
	}
	return c.Detect(ctx, req)
}

// ChatCompletionRequest is a minimal OpenAI-style chat completion request
// used when calling the TSZ LLM gateway (/v1/chat/completions).
type ChatCompletionRequest struct {
	Model    string                   `json:"model"`
	Messages []map[string]interface{} `json:"messages"`
	Stream   bool                     `json:"stream,omitempty"`
	// Extra fields are allowed; use Extra to pass vendor-specific options.
	Extra map[string]interface{} `json:"-"`
}

// ChatCompletionResponse is kept generic; callers can unmarshal into
// a strongly-typed struct if they wish, but for most use cases this
// map-based representation is sufficient.
type ChatCompletionResponse map[string]interface{}

// ChatCompletions calls the OpenAI-compatible LLM gateway
// (/v1/chat/completions) exposed by TSZ.
//
// Optional headers can be provided to control TSZ behaviour, for example:
//   - X-TSZ-RID
//   - X-TSZ-Guardrails
func (c *Client) ChatCompletions(
	ctx context.Context,
	req ChatCompletionRequest,
	headers map[string]string,
) (ChatCompletionResponse, error) {

	// Build payload map so we can merge Extra fields if provided
	payload := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
	}
	if req.Stream {
		payload["stream"] = true
	}
	for k, v := range req.Extra {
		payload[k] = v
	}

	resp, err := postJSON[ChatCompletionResponse](ctx, c, "/v1/chat/completions", payload, headers)
	if err != nil {
		return nil, err
	}
	return *resp, nil
}

// postJSON is a small helper for POSTing JSON and decoding the JSON response
// into a target type.
func postJSON[T any](
	ctx context.Context,
	c *Client,
	path string,
	body interface{},
	headers map[string]string,
) (*T, error) {
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + path

	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-ADMIN-KEY", c.apiKey)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: respBody}
	}

	var out T
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("failed to decode response body: %w", err)
	}

	return &out, nil
}

// getJSON is a helper for GET requests decoding JSON.
func getJSON[T any](
	ctx context.Context,
	c *Client,
	path string,
) (*T, error) {
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-ADMIN-KEY", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: respBody}
	}

	var out T
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("failed to decode response body: %w", err)
	}

	return &out, nil
}

// deleteRequest is a helper for DELETE requests.
func deleteRequest(
	ctx context.Context,
	c *Client,
	path string,
) error {
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + path

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if c.apiKey != "" {
		req.Header.Set("X-ADMIN-KEY", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{StatusCode: resp.StatusCode, Body: body}
	}

	return nil
}
