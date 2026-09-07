package guardrails

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

// WebhookAuditor delivers the transport-safe AuditEvent used by BYG to an
// operator-configured audit or SIEM endpoint. AuditEvent intentionally has no
// request body, response body, raw detection value, or credential fields.
type WebhookAuditor struct {
	endpoint string
	client   *http.Client
}

// NewWebhookAuditor validates an HTTP(S) endpoint before it can be used.
func NewWebhookAuditor(endpoint string) (*WebhookAuditor, error) {
	endpoint = strings.TrimSpace(endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid audit webhook endpoint")
	}
	return &WebhookAuditor{endpoint: endpoint, client: &http.Client{Timeout: 2 * time.Second}}, nil
}

// Audit sends one JSON event and treats every non-2xx response as a delivery
// error. The caller applies the policy's configured audit failure behavior.
func (a *WebhookAuditor) Audit(ctx context.Context, event AuditEvent) error {
	if a == nil || a.client == nil {
		return fmt.Errorf("audit webhook is not configured")
	}
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode audit event: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create audit webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("deliver audit event: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("audit webhook returned HTTP %d", response.StatusCode)
	}
	return nil
}
