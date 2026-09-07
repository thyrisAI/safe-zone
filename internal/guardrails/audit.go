package guardrails

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

type AuditStage string

const (
	AuditStageRequest  AuditStage = "request"
	AuditStageResponse AuditStage = "response"
)

func (stage AuditStage) Validate() error {
	switch stage {
	case AuditStageRequest, AuditStageResponse:
		return nil
	default:
		return fmt.Errorf("invalid audit stage %q", stage)
	}
}

// AuditEvent is gateway-neutral and contains aggregate security metadata only.
// It must never contain request content or raw matched values.
type AuditEvent struct {
	Timestamp time.Time `json:"timestamp"`
	// EventType distinguishes guardrail decisions from operational lifecycle
	// signals. Empty remains compatible with older decision-event consumers.
	EventType     string `json:"event_type,omitempty"`
	RID           string `json:"rid"`
	RequestID     string `json:"request_id"`
	TraceID       string `json:"trace_id,omitempty"`
	Adapter       string `json:"adapter"`
	Target        string `json:"target"`
	Gateway       string `json:"gateway"`
	Route         string `json:"route"`
	Tenant        string `json:"tenant,omitempty"`
	PolicyID      string `json:"policy_id"`
	PolicyVersion int    `json:"policy_version"`
	// Reason is a bounded operational classification, never detector output or
	// request/response content.
	Reason             string     `json:"reason,omitempty"`
	Stage              AuditStage `json:"stage"`
	Action             RuleAction `json:"action"`
	Categories         []string   `json:"categories,omitempty"`
	DetectionCount     int        `json:"detection_count"`
	ProcessorLatencyMS int64      `json:"processor_latency_ms"`
	Degraded           bool       `json:"degraded,omitempty"`
}

// BuildAuditTarget creates a stable, safe identifier for the protected
// gateway destination. Components are escaped so values cannot make the
// target ambiguous in logs or downstream audit sinks.
func BuildAuditTarget(gateway, tenant, route string) string {
	return fmt.Sprintf("gateway=%s;tenant=%s;route=%s",
		url.QueryEscape(gateway), url.QueryEscape(tenant), url.QueryEscape(route))
}

type Auditor interface {
	Audit(ctx context.Context, event AuditEvent) error
}

// NoopAuditor is the safe default until an audit sink is configured.
type NoopAuditor struct{}

func (NoopAuditor) Audit(context.Context, AuditEvent) error { return nil }
