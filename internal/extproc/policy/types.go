package policy

import "time"

type SnapshotStatus string

const (
	StatusDraft      SnapshotStatus = "draft"
	StatusValidated  SnapshotStatus = "validated"
	StatusCompiled   SnapshotStatus = "compiled"
	StatusStaged     SnapshotStatus = "staged"
	StatusActive     SnapshotStatus = "active"
	StatusSuperseded SnapshotStatus = "superseded"
	StatusRolledBack SnapshotStatus = "rolled_back"
)

type Policy struct {
	ID        int64
	Name      string
	Tenant    *string
	CreatedAt time.Time
}

type PolicySnapshot struct {
	ID            int64
	PolicyID      int64
	PolicyName    string
	Tenant        *string
	Version       *int
	Status        SnapshotStatus
	Definition    PolicyDefinition
	IntegrityHash string
	CompiledAt    *time.Time
	ActivatedAt   *time.Time
}

type Action string

const (
	ActionAllow     Action = "ALLOW"
	ActionMask      Action = "MASK"
	ActionBlock     Action = "BLOCK"
	ActionAuditOnly Action = "AUDIT_ONLY"
)

func (action Action) Valid() bool {
	switch action {
	case ActionAllow, ActionMask, ActionBlock, ActionAuditOnly:
		return true
	default:
		return false
	}
}

type FailureMode string

const (
	FailureModeClosed FailureMode = "closed"
	FailureModeOpen   FailureMode = "open"
)

type PolicyDefinition struct {
	Scope         Scope             `json:"scope"`
	Request       RequestPolicy     `json:"request"`
	Response      ResponsePolicy    `json:"response"`
	FailurePolicy FailurePolicy     `json:"failure_policy"`
	Limits        Limits            `json:"limits"`
	Audit         AuditSettings     `json:"audit"`
	Telemetry     TelemetrySettings `json:"telemetry"`
	// TemplateRefs records the immutable template versions resolved into this
	// compiled snapshot. It is retained for auditability and integrity hashing.
	TemplateRefs []TemplateReference `json:"template_refs,omitempty"`
}

// TemplateReference pins a reusable guardrail template to one immutable
// version. Template contents are resolved during compilation, never at
// request time.
type TemplateReference struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// TemplateDefinition is the immutable rule material stored in a template
// snapshot. Actions remain policy-owned; a template contributes references.
type TemplateDefinition struct {
	Request  TemplateRequestRules  `json:"request,omitempty"`
	Response TemplateResponseRules `json:"response,omitempty"`
}

type TemplateRequestRules struct {
	CustomPatternIDs []string             `json:"custom_pattern_ids,omitempty"`
	AllowlistIDs     []string             `json:"allowlist_ids,omitempty"`
	BlocklistIDs     []string             `json:"blocklist_ids,omitempty"`
	CustomValidators []ValidatorReference `json:"custom_validators,omitempty"`
}

type TemplateResponseRules struct {
	CustomPatternIDs []string             `json:"custom_pattern_ids,omitempty"`
	CustomValidators []ValidatorReference `json:"custom_validators,omitempty"`
}

type Scope struct {
	Tenant      *string `json:"tenant,omitempty"`
	Environment string  `json:"environment,omitempty"`
	Gateway     string  `json:"gateway,omitempty"`
	Route       string  `json:"route,omitempty"`
}

type RequestPolicy struct {
	PII              Action               `json:"pii"`
	Secret           Action               `json:"secret"`
	PromptInjection  Action               `json:"prompt_injection"`
	CustomPatternIDs []string             `json:"custom_pattern_ids"`
	AllowlistIDs     []string             `json:"allowlist_ids"`
	BlocklistIDs     []string             `json:"blocklist_ids"`
	CustomValidators []ValidatorReference `json:"custom_validators"`
	// CompiledRules contains immutable rule material resolved at compile time.
	// Request processing must not look up mutable pattern/list/validator rows.
	CompiledRules CompiledRequestRules `json:"compiled_rules,omitempty"`
}

type CompiledRequestRules struct {
	CustomPatterns []CompiledPattern   `json:"custom_patterns,omitempty"`
	Allowlist      []CompiledList      `json:"allowlist,omitempty"`
	Blocklist      []CompiledList      `json:"blocklist,omitempty"`
	Validators     []CompiledValidator `json:"validators,omitempty"`
}

type CompiledPattern struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Regex    string `json:"regex"`
	Action   Action `json:"action"`
}

type CompiledList struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

type CompiledValidator struct {
	ID               string `json:"id"`
	Version          int    `json:"version"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	Rule             string `json:"rule"`
	ExpectedResponse string `json:"expected_response"`
	Action           Action `json:"action"`
}

type ResponsePolicy struct {
	Enabled          bool                 `json:"enabled"`
	PII              Action               `json:"pii"`
	Secret           Action               `json:"secret"`
	UnsafeContent    Action               `json:"unsafe_content"`
	CustomPatternIDs []string             `json:"custom_pattern_ids"`
	CustomValidators []ValidatorReference `json:"custom_validators"`
}

// ValidatorReference pins a validator definition to a specific immutable
// version. A policy snapshot must never resolve a floating validator version.
type ValidatorReference struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type FailurePolicy struct {
	Request  FailureMode `json:"request"`
	Response FailureMode `json:"response"`
}

type Limits struct {
	MaxBodyBytes        int64 `json:"max_body_bytes"`
	ProcessingTimeoutMS int64 `json:"processing_timeout_ms"`
	MaxDetections       int   `json:"max_detections"`
}

type AuditSettings struct {
	Enabled           bool `json:"enabled"`
	IncludeCategories bool `json:"include_categories"`
}

type TelemetrySettings struct {
	Enabled        bool    `json:"enabled"`
	MetricsEnabled bool    `json:"metrics_enabled"`
	TracingEnabled bool    `json:"tracing_enabled"`
	SampleRate     float64 `json:"sample_rate"`
}
