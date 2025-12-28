package models

import "gorm.io/gorm"

// DetectRequest represents the incoming request payload for PII detection
type DetectRequest struct {
	Text           string   `json:"text"`
	Mode           string   `json:"mode,omitempty"`
	RID            string   `json:"rid,omitempty"`
	ExpectedFormat string   `json:"expected_format,omitempty"`
	Guardrails     []string `json:"guardrails,omitempty"`
}

// DetectionResult represents a single detected PII entity
type DetectionResult struct {
	Type                  string                 `json:"type"`
	Value                 string                 `json:"value"`
	Placeholder           string                 `json:"placeholder"`
	Start                 int                    `json:"start"`
	End                   int                    `json:"end"`
	ConfidenceScore       Confidence             `json:"confidence_score"`
	ConfidenceExplanation *ConfidenceExplanation `json:"confidence_explanation,omitempty"`
}

// DetectResponse represents the response payload containing redacted text and detections
type DetectResponse struct {
	RedactedText      string            `json:"redacted_text,omitempty"`
	Detections        []DetectionResult `json:"detections,omitempty"`
	ValidatorResults  []ValidatorResult `json:"validator_results,omitempty"`
	Breakdown         map[string]int    `json:"breakdown"`
	Blocked           bool              `json:"blocked"`
	ContainsPII       bool              `json:"contains_pii"`
	OverallConfidence Confidence        `json:"overall_confidence"`
	Message           string            `json:"message,omitempty"`
}

// ValidatorResult represents confidence-scored validator outcome
type ValidatorResult struct {
	Name            string     `json:"name"`
	Type            string     `json:"type"`
	Passed          bool       `json:"passed"`
	ConfidenceScore Confidence `json:"confidence_score"`
}

// Pattern represents a regex pattern stored in the database
type Pattern struct {
	gorm.Model
	Name        string `gorm:"uniqueIndex:idx_patterns_name;not null"`
	Regex       string `gorm:"not null"`
	Description string
	Category    string `gorm:"default:'PII'"` // PII, SECRET, INJECTION, TOPIC
	IsActive    bool   `gorm:"default:true"`

	// Enterprise policy overrides (optional)
	BlockThreshold *float64
	AllowThreshold *float64
}

// FormatValidator represents a dynamic validation rule
type FormatValidator struct {
	gorm.Model
	Name             string `gorm:"uniqueIndex:idx_format_validators_name;not null" json:"name"`
	Type             string `gorm:"not null" json:"type"` // BUILTIN, REGEX, SCHEMA, AI_PROMPT
	Rule             string `json:"rule"`                 // Regex, Prompt text, or JSON Schema
	Description      string `json:"description"`
	ExpectedResponse string `json:"expected_response"` // Dynamic expectation (e.g. "YES", "SAFE", "1")
}

// GuardrailTemplate represents a portable collection of rules
type GuardrailTemplate struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Validators  []FormatValidator `json:"validators"`
	Patterns    []Pattern         `json:"patterns"`
}

// AllowlistItem represents a value that should be ignored during detection
type AllowlistItem struct {
	gorm.Model
	Value       string `gorm:"uniqueIndex:idx_allowlist_value;not null" json:"value"`
	Description string `json:"description"`
}

// TableName overrides the table name used by AllowlistItem to `allowlist`
func (AllowlistItem) TableName() string {
	return "allowlist"
}

// BlacklistItem represents a value that should be strictly blocked
type BlacklistItem struct {
	gorm.Model
	Value       string `gorm:"uniqueIndex:idx_blocklist_value;not null" json:"value"`
	Description string `json:"description"`
}

// TableName overrides the table name used by BlacklistItem to `blocklist`
func (BlacklistItem) TableName() string {
	return "blocklist"
}
