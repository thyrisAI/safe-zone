package guardrails

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"thyris-sz/internal/ai"
	"thyris-sz/internal/config"
	"thyris-sz/internal/models"
)

// GuardrailService is the transport-independent inspection boundary shared by
// HTTP adapters and gateway processors.
type GuardrailService interface {
	Inspect(ctx context.Context, input InspectInput) (InspectResult, error)
}

type RuleAction string

const (
	RuleActionAllow     RuleAction = "ALLOW"
	RuleActionMask      RuleAction = "MASK"
	RuleActionBlock     RuleAction = "BLOCK"
	RuleActionAuditOnly RuleAction = "AUDIT_ONLY"
)

func (action RuleAction) Validate() error {
	switch action {
	case RuleActionAllow, RuleActionMask, RuleActionBlock, RuleActionAuditOnly:
		return nil
	default:
		return fmt.Errorf("invalid guardrail action %q", action)
	}
}

type ValidatorKind string

const (
	ValidatorRegex   ValidatorKind = "REGEX"
	ValidatorSchema  ValidatorKind = "SCHEMA"
	ValidatorAI      ValidatorKind = "AI_PROMPT"
	ValidatorBuiltin ValidatorKind = "BUILTIN"
)

// CompiledPolicyRules contains immutable, already-resolved rule material. IDs
// are retained for auditability; inspection never performs floating lookups.
type CompiledPolicyRules struct {
	PolicyID              string
	Version               int
	PIIAction             RuleAction
	SecretAction          RuleAction
	PromptInjectionAction RuleAction
	CustomPatterns        []CompiledPatternRule
	Allowlist             []CompiledListRule
	Blocklist             []CompiledListRule
	Validators            []CompiledValidatorRule
}

type CompiledPatternRule struct {
	ID       string
	Name     string
	Category string
	Regex    string
	Action   RuleAction
}

type CompiledListRule struct {
	ID    string
	Value string
}

type CompiledValidatorRule struct {
	ID               string
	Version          int
	Name             string
	Kind             ValidatorKind
	Rule             string
	ExpectedResponse string
	Action           RuleAction
}

// builtinPIIPatterns are always enforced by the compiled-policy path. They
// deliberately live in code rather than the mutable pattern repository so a
// BYG request cannot lose baseline PII protection due to a policy/cache race.
var builtinPIIPatterns = []struct {
	name  string
	regex string
}{
	{name: "EMAIL", regex: `[A-Za-z0-9.!#$%&'*+/=?^_\x60{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+`},
	{name: "CREDIT_CARD", regex: `(?:[0-9][ -]?){13,19}`},
	{name: "PHONE", regex: `\+?[0-9][0-9 ()-]{7,}[0-9]`},
}

type InspectInput struct {
	Text           string
	RID            string
	Mode           string
	ExpectedFormat string
	Guardrails     []string
	Policy         *CompiledPolicyRules
}

// Finding contains only safe rule metadata and offsets. It intentionally does
// not contain the raw matched value.
type Finding struct {
	Rule             string
	Category         string
	Action           RuleAction
	Start            int
	End              int
	Placeholder      string
	Confidence       float64
	ConfidenceSource string
	RegexConfidence  float64
	AIConfidence     float64
	PatternActive    bool
	Validator        bool
	ValidatorPassed  bool
}

type InspectResult struct {
	Action            RuleAction
	Findings          []Finding
	DetectionCount    int
	SafeContent       string
	Blocked           bool
	ContainsSensitive bool
	Categories        []string
	Breakdown         map[string]int
	OverallConfidence float64
	Message           string
}

type Service struct {
	detector *Detector
}

func NewGuardrailService(detector *Detector) (*Service, error) {
	if detector == nil {
		return nil, errors.New("guardrail detector is required")
	}
	return &Service{detector: detector}, nil
}

func (s *Service) Inspect(ctx context.Context, input InspectInput) (InspectResult, error) {
	if err := ctx.Err(); err != nil {
		return InspectResult{}, err
	}
	if input.Policy == nil {
		return inspectLegacy(s.detector.Detect(models.DetectRequest{
			Text: input.Text, Mode: input.Mode, RID: input.RID,
			ExpectedFormat: input.ExpectedFormat, Guardrails: input.Guardrails,
		})), nil
	}
	return inspectCompiled(ctx, input)
}

func inspectLegacy(response models.DetectResponse) InspectResult {
	findings := make([]Finding, 0, len(response.Detections)+len(response.ValidatorResults))
	for _, detection := range response.Detections {
		action := RuleAction(resolveAction(float64(detection.ConfidenceScore), getAllowThreshold(), getBlockThreshold()))
		if detection.Type == "BLOCKLIST" {
			action = RuleActionBlock
		}
		findings = append(findings, Finding{
			Rule: detection.Type, Category: detection.Type, Action: action,
			Start: detection.Start, End: detection.End, Placeholder: detection.Placeholder,
			Confidence: float64(detection.ConfidenceScore),
		})
		if detection.ConfidenceExplanation != nil {
			finding := &findings[len(findings)-1]
			finding.Category = detection.ConfidenceExplanation.Category
			if finding.Category == "" {
				finding.Category = detection.Type
			}
			finding.ConfidenceSource = detection.ConfidenceExplanation.Source
			finding.RegexConfidence = float64(detection.ConfidenceExplanation.RegexScore)
			finding.AIConfidence = float64(detection.ConfidenceExplanation.AIScore)
			finding.PatternActive = detection.ConfidenceExplanation.PatternActive
		}
	}
	for _, validator := range response.ValidatorResults {
		action := RuleActionAllow
		if !validator.Passed {
			action = RuleActionBlock
		}
		findings = append(findings, Finding{
			Rule: validator.Name, Category: "VALIDATOR", Action: action,
			Confidence: float64(validator.ConfidenceScore), Validator: true,
			ValidatorPassed: validator.Passed,
		})
	}
	action := RuleActionAllow
	if response.Blocked {
		action = RuleActionBlock
	} else if len(response.Detections) > 0 && response.RedactedText != "" {
		action = RuleActionMask
	}
	return InspectResult{
		Action: action, Findings: findings, DetectionCount: len(response.Detections),
		SafeContent: response.RedactedText, Blocked: response.Blocked,
		ContainsSensitive: response.ContainsPII, Categories: sortedKeys(response.Breakdown),
		Breakdown: cloneBreakdown(response.Breakdown), OverallConfidence: float64(response.OverallConfidence),
		Message: response.Message,
	}
}

func inspectCompiled(ctx context.Context, input InspectInput) (InspectResult, error) {
	policy := input.Policy
	if strings.TrimSpace(policy.PolicyID) == "" || policy.Version <= 0 {
		return InspectResult{}, errors.New("compiled policy ID and positive version are required")
	}
	for _, action := range []RuleAction{policy.PIIAction, policy.SecretAction, policy.PromptInjectionAction} {
		if err := action.Validate(); err != nil {
			return InspectResult{}, err
		}
	}

	allow := make(map[string]struct{}, len(policy.Allowlist))
	for _, rule := range policy.Allowlist {
		allow[rule.Value] = struct{}{}
	}
	findings := make([]Finding, 0)
	for _, rule := range policy.Blocklist {
		if rule.Value == "" {
			continue
		}
		for offset := 0; offset < len(input.Text); {
			index := strings.Index(input.Text[offset:], rule.Value)
			if index < 0 {
				break
			}
			start := offset + index
			end := start + len(rule.Value)
			findings = append(findings, Finding{Rule: rule.ID, Category: "BLOCKLIST", Action: RuleActionBlock, Start: start, End: end, Placeholder: "[BLOCKED]", Confidence: 1})
			offset = end
		}
	}
	for _, rule := range policy.CustomPatterns {
		if err := ctx.Err(); err != nil {
			return InspectResult{}, err
		}
		action := rule.Action
		if action == "" {
			action = categoryAction(policy, rule.Category)
		}
		if err := action.Validate(); err != nil {
			return InspectResult{}, fmt.Errorf("pattern %q: %w", rule.ID, err)
		}
		compiled, err := getCachedRegex(rule.Regex)
		if err != nil {
			return InspectResult{}, fmt.Errorf("compile pattern %q: %w", rule.ID, err)
		}
		for _, match := range compiled.FindAllStringIndex(input.Text, -1) {
			if _, excluded := allow[input.Text[match[0]:match[1]]]; excluded {
				continue
			}
			name := rule.Name
			if name == "" {
				name = rule.ID
			}
			findings = append(findings, Finding{Rule: name, Category: normalizedCategory(rule.Category), Action: action, Start: match[0], End: match[1], Placeholder: generatePlaceholder(name, input.RID), Confidence: 1})
		}
	}
	// Baseline PII protection is independent from the policy's mutable custom
	// patterns. The policy still controls the enforcement outcome through its
	// immutable PII action and allowlist snapshot.
	for _, builtin := range builtinPIIPatterns {
		compiled, err := getCachedRegex(builtin.regex)
		if err != nil {
			return InspectResult{}, fmt.Errorf("compile built-in PII pattern %q: %w", builtin.name, err)
		}
		for _, match := range compiled.FindAllStringIndex(input.Text, -1) {
			if _, excluded := allow[input.Text[match[0]:match[1]]]; excluded {
				continue
			}
			findings = append(findings, Finding{
				Rule: builtin.name, Category: "PII", Action: policy.PIIAction,
				Start: match[0], End: match[1], Placeholder: generatePlaceholder(builtin.name, input.RID), Confidence: 1,
			})
		}
	}

	findings = nonOverlapping(findings)
	for _, validator := range policy.Validators {
		if validator.ID == "" || validator.Version <= 0 {
			return InspectResult{}, fmt.Errorf("validator references require an ID and positive immutable version")
		}
		if err := validator.Action.Validate(); err != nil {
			return InspectResult{}, fmt.Errorf("validator %q: %w", validator.ID, err)
		}
		passed, err := validateCompiled(ctx, input.Text, validator)
		if err != nil {
			passed = false
		}
		if !passed {
			name := validator.Name
			if name == "" {
				name = validator.ID
			}
			findings = append(findings, Finding{Rule: name, Category: "VALIDATOR", Action: validator.Action, Confidence: 1, Validator: true, ValidatorPassed: false})
		}
	}

	return buildCompiledResult(input.Text, findings), nil
}

func validateCompiled(ctx context.Context, text string, validator CompiledValidatorRule) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	switch validator.Kind {
	case ValidatorRegex:
		return regexp.MatchString(validator.Rule, text)
	case ValidatorSchema:
		if !config.AppConfig.Features.SchemaValidationEnabled {
			return true, nil
		}
		if !isValidJSON(text) {
			return false, nil
		}
		return isValidSchema(text, validator.Rule)
	case ValidatorAI:
		if !config.AppConfig.Features.SemanticAnalysisEnabled {
			return false, errors.New("AI validation is disabled")
		}
		ctx, span := otel.Tracer("thyris-sz/guardrails").Start(ctx, "tsz.semantic_model")
		defer span.End()
		span.SetAttributes(attribute.String("tsz.validator.kind", string(validator.Kind)))
		passed, err := ai.CheckWithAI(ctx, text, validator.Rule, validator.ExpectedResponse)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "semantic validation failed")
		}
		return passed, err
	case ValidatorBuiltin:
		switch strings.ToUpper(validator.Name) {
		case "JSON":
			return isValidJSON(text), nil
		case "XML":
			return isValidXML(text), nil
		default:
			return false, fmt.Errorf("unknown builtin validator %q", validator.Name)
		}
	default:
		return false, fmt.Errorf("unknown validator kind %q", validator.Kind)
	}
}

func buildCompiledResult(text string, findings []Finding) InspectResult {
	breakdown := make(map[string]int)
	categories := make(map[string]struct{})
	action := RuleActionAllow
	blocked := false
	detectionCount := 0
	for _, finding := range findings {
		breakdown[finding.Category]++
		categories[finding.Category] = struct{}{}
		detectionCount++
		action = strongerAction(action, finding.Action)
		blocked = blocked || finding.Action == RuleActionBlock
	}
	safeContent := maskFindings(text, findings)
	return InspectResult{Action: action, Findings: findings, DetectionCount: detectionCount, SafeContent: safeContent, Blocked: blocked, ContainsSensitive: detectionCount > 0, Categories: sortedSet(categories), Breakdown: breakdown}
}

func maskFindings(text string, findings []Finding) string {
	maskable := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		if !finding.Validator && (finding.Action == RuleActionMask || finding.Action == RuleActionBlock) {
			maskable = append(maskable, finding)
		}
	}
	maskable = nonOverlapping(maskable)
	var result strings.Builder
	position := 0
	for _, finding := range maskable {
		if finding.Start < position || finding.End > len(text) {
			continue
		}
		result.WriteString(text[position:finding.Start])
		result.WriteString(finding.Placeholder)
		position = finding.End
	}
	result.WriteString(text[position:])
	return result.String()
}

func nonOverlapping(findings []Finding) []Finding {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Start == findings[j].Start {
			return findings[i].End > findings[j].End
		}
		return findings[i].Start < findings[j].Start
	})
	result := make([]Finding, 0, len(findings))
	end := 0
	for _, finding := range findings {
		if finding.Validator || finding.Start >= end {
			result = append(result, finding)
			if !finding.Validator {
				end = finding.End
			}
		}
	}
	return result
}

func categoryAction(policy *CompiledPolicyRules, category string) RuleAction {
	switch normalizedCategory(category) {
	case "SECRET":
		return policy.SecretAction
	case "PROMPT_INJECTION", "INJECTION":
		return policy.PromptInjectionAction
	default:
		return policy.PIIAction
	}
}

func normalizedCategory(category string) string {
	return strings.ToUpper(strings.TrimSpace(category))
}

func strongerAction(current, candidate RuleAction) RuleAction {
	priority := map[RuleAction]int{RuleActionAllow: 0, RuleActionAuditOnly: 1, RuleActionMask: 2, RuleActionBlock: 3}
	if priority[candidate] > priority[current] {
		return candidate
	}
	return current
}

func sortedKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedSet(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneBreakdown(source map[string]int) map[string]int {
	clone := make(map[string]int, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

// DetectLegacy adapts the transport-neutral result to the existing HTTP JSON
// contract. Raw values are reconstructed only at this boundary from offsets;
// they are never present in InspectResult or available to audit logging.
func DetectLegacy(ctx context.Context, service GuardrailService, request models.DetectRequest) (models.DetectResponse, error) {
	result, err := service.Inspect(ctx, InspectInput{Text: request.Text, RID: request.RID, Mode: request.Mode, ExpectedFormat: request.ExpectedFormat, Guardrails: request.Guardrails})
	if err != nil {
		return models.DetectResponse{}, err
	}
	detections := make([]models.DetectionResult, 0, len(result.Findings))
	validators := make([]models.ValidatorResult, 0, len(result.Findings))
	for _, finding := range result.Findings {
		if finding.Validator {
			validators = append(validators, models.ValidatorResult{Name: finding.Rule, Type: "VALIDATOR", Passed: finding.ValidatorPassed, ConfidenceScore: models.Confidence(roundConfidence(finding.Confidence))})
			continue
		}
		value := ""
		if finding.Start >= 0 && finding.End >= finding.Start && finding.End <= len(request.Text) {
			value = request.Text[finding.Start:finding.End]
		}
		var explanation *models.ConfidenceExplanation
		if finding.ConfidenceSource != "" {
			explanation = &models.ConfidenceExplanation{Source: finding.ConfidenceSource, Category: finding.Category, RegexScore: models.Confidence(roundConfidence(finding.RegexConfidence)), AIScore: models.Confidence(roundConfidence(finding.AIConfidence)), PatternActive: finding.PatternActive, FinalScore: models.Confidence(roundConfidence(finding.Confidence))}
		}
		detections = append(detections, models.DetectionResult{Type: finding.Rule, Value: value, Placeholder: finding.Placeholder, Start: finding.Start, End: finding.End, ConfidenceScore: models.Confidence(roundConfidence(finding.Confidence)), ConfidenceExplanation: explanation})
	}
	return models.DetectResponse{RedactedText: result.SafeContent, Detections: detections, ValidatorResults: validators, Breakdown: cloneBreakdown(result.Breakdown), Blocked: result.Blocked, ContainsPII: result.ContainsSensitive, OverallConfidence: models.Confidence(roundConfidence(result.OverallConfidence)), Message: result.Message}, nil
}
