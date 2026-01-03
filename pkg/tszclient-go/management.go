package tszclient

import (
	"context"
	"fmt"
)

// --- Models ---

// Pattern represents a regex-based detection rule.
type Pattern struct {
	ID             int     `json:"ID,omitempty"`
	Name           string  `json:"Name"`
	Regex          string  `json:"Regex"`
	Description    string  `json:"Description,omitempty"`
	Category       string  `json:"Category,omitempty"`
	IsActive       bool    `json:"IsActive"`
	BlockThreshold float64 `json:"BlockThreshold,omitempty"`
	AllowThreshold float64 `json:"AllowThreshold,omitempty"`
	CreatedAt      string  `json:"CreatedAt,omitempty"`
	UpdatedAt      string  `json:"UpdatedAt,omitempty"`
}

// AllowlistItem represents a value that should be ignored during detection.
type AllowlistItem struct {
	ID          int    `json:"ID,omitempty"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// BlacklistItem represents a value that should be strictly blocked.
type BlacklistItem struct {
	ID          int    `json:"ID,omitempty"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// FormatValidator represents a dynamic validation rule (Regex, AI Prompt, JSON Schema).
type FormatValidator struct {
	ID               int    `json:"ID,omitempty"`
	Name             string `json:"name"`
	Type             string `json:"type"` // BUILTIN, REGEX, SCHEMA, AI_PROMPT
	Rule             string `json:"rule"`
	Description      string `json:"description,omitempty"`
	ExpectedResponse string `json:"expected_response,omitempty"`
}

// TemplateDefinition defines the structure of a guardrail template.
type TemplateDefinition struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Patterns    []Pattern         `json:"patterns,omitempty"`
	Validators  []FormatValidator `json:"validators,omitempty"`
}

// TemplateImportRequest is the payload for importing a template.
type TemplateImportRequest struct {
	Template TemplateDefinition `json:"template"`
}

// --- Methods ---

// ListPatterns returns all detection patterns.
func (c *Client) ListPatterns(ctx context.Context) ([]Pattern, error) {
	resp, err := getJSON[[]Pattern](ctx, c, "/patterns")
	if err != nil {
		return nil, err
	}
	return *resp, nil
}

// CreatePattern adds a new detection pattern.
func (c *Client) CreatePattern(ctx context.Context, p Pattern) (*Pattern, error) {
	return postJSON[Pattern](ctx, c, "/patterns", p, nil)
}

// DeletePattern removes a detection pattern by ID.
func (c *Client) DeletePattern(ctx context.Context, id int) error {
	return deleteRequest(ctx, c, fmt.Sprintf("/patterns/%d", id))
}

// ListAllowlist returns all allowlist items.
func (c *Client) ListAllowlist(ctx context.Context) ([]AllowlistItem, error) {
	resp, err := getJSON[[]AllowlistItem](ctx, c, "/allowlist")
	if err != nil {
		return nil, err
	}
	return *resp, nil
}

// CreateAllowlistItem adds a new item to the allowlist.
func (c *Client) CreateAllowlistItem(ctx context.Context, item AllowlistItem) (*AllowlistItem, error) {
	return postJSON[AllowlistItem](ctx, c, "/allowlist", item, nil)
}

// DeleteAllowlistItem removes an item from the allowlist by ID.
func (c *Client) DeleteAllowlistItem(ctx context.Context, id int) error {
	return deleteRequest(ctx, c, fmt.Sprintf("/allowlist/%d", id))
}

// ListBlocklist returns all blocklist items.
func (c *Client) ListBlocklist(ctx context.Context) ([]BlacklistItem, error) {
	resp, err := getJSON[[]BlacklistItem](ctx, c, "/blacklist")
	if err != nil {
		return nil, err
	}
	return *resp, nil
}

// CreateBlocklistItem adds a new item to the blocklist.
func (c *Client) CreateBlocklistItem(ctx context.Context, item BlacklistItem) (*BlacklistItem, error) {
	return postJSON[BlacklistItem](ctx, c, "/blacklist", item, nil)
}

// DeleteBlocklistItem removes an item from the blocklist by ID.
func (c *Client) DeleteBlocklistItem(ctx context.Context, id int) error {
	return deleteRequest(ctx, c, fmt.Sprintf("/blacklist/%d", id))
}

// ListValidators returns all format validators.
func (c *Client) ListValidators(ctx context.Context) ([]FormatValidator, error) {
	resp, err := getJSON[[]FormatValidator](ctx, c, "/validators")
	if err != nil {
		return nil, err
	}
	return *resp, nil
}

// CreateValidator adds a new format validator.
func (c *Client) CreateValidator(ctx context.Context, v FormatValidator) (*FormatValidator, error) {
	return postJSON[FormatValidator](ctx, c, "/validators", v, nil)
}

// DeleteValidator removes a format validator by ID.
func (c *Client) DeleteValidator(ctx context.Context, id int) error {
	return deleteRequest(ctx, c, fmt.Sprintf("/validators/%d", id))
}

// ImportTemplate imports a guardrail template (patterns and validators).
func (c *Client) ImportTemplate(ctx context.Context, template TemplateDefinition) error {
	req := TemplateImportRequest{Template: template}
	// The endpoint returns 200 OK with a message map, we can ignore the body if needed or return it.
	// For now, checking error is sufficient.
	_, err := postJSON[map[string]interface{}](ctx, c, "/templates/import", req, nil)
	return err
}
