package guardrails

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"thyris-sz/internal/models"
)

func TestInspectCompiledPolicyMasksWithoutExposingRawMatch(t *testing.T) {
	service := compiledPolicyTestService(t)
	input := InspectInput{
		Text: "contact alice@example.com token=secret-123",
		RID:  "rid-1",
		Policy: &CompiledPolicyRules{
			PolicyID: "default", Version: 7,
			PIIAction: RuleActionMask, SecretAction: RuleActionBlock,
			PromptInjectionAction: RuleActionAuditOnly,
			CustomPatterns: []CompiledPatternRule{
				{ID: "email", Name: "EMAIL", Category: "PII", Regex: `[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}`, Action: RuleActionMask},
				{ID: "secret", Name: "TOKEN", Category: "SECRET", Regex: `secret-[0-9]+`, Action: RuleActionBlock},
			},
		},
	}
	result, err := service.Inspect(context.Background(), input)
	if err != nil {
		t.Fatalf("inspectCompiled() error = %v", err)
	}
	if result.Action != RuleActionBlock || !result.Blocked || result.DetectionCount != 2 {
		t.Fatalf("result action=%q blocked=%v count=%d", result.Action, result.Blocked, result.DetectionCount)
	}
	if strings.Contains(result.SafeContent, "alice@example.com") || strings.Contains(result.SafeContent, "secret-123") {
		t.Fatalf("safe content exposes matched value: %q", result.SafeContent)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal InspectResult: %v", err)
	}
	if strings.Contains(string(encoded), "alice@example.com") || strings.Contains(string(encoded), "secret-123") {
		t.Fatalf("InspectResult JSON exposes raw match: %s", encoded)
	}
}

func TestInspectCompiledPolicyHonorsAllowlistAndVersionedValidator(t *testing.T) {
	service := compiledPolicyTestService(t)
	input := InspectInput{
		Text: "allowed@example.com",
		Policy: &CompiledPolicyRules{
			PolicyID: "default", Version: 1,
			PIIAction: RuleActionMask, SecretAction: RuleActionBlock, PromptInjectionAction: RuleActionBlock,
			Allowlist:      []CompiledListRule{{ID: "allow-1", Value: "allowed@example.com"}},
			CustomPatterns: []CompiledPatternRule{{ID: "email", Name: "EMAIL", Category: "PII", Regex: `.+@.+`, Action: RuleActionMask}},
			Validators:     []CompiledValidatorRule{{ID: "format", Version: 3, Name: "email-format", Kind: ValidatorRegex, Rule: `^.+@.+$`, Action: RuleActionBlock}},
		},
	}
	result, err := service.Inspect(context.Background(), input)
	if err != nil {
		t.Fatalf("inspectCompiled() error = %v", err)
	}
	if result.DetectionCount != 0 || result.Action != RuleActionAllow || result.SafeContent != input.Text {
		t.Fatalf("allowlisted result = %+v", result)
	}

	input.Policy.Validators[0].Version = 0
	if _, err := service.Inspect(context.Background(), input); err == nil {
		t.Fatal("zero validator version was accepted")
	}
}

func TestInspectCompiledPolicyAlwaysRunsBuiltinPIIChecks(t *testing.T) {
	service := compiledPolicyTestService(t)
	input := InspectInput{
		Text: "email alice@example.com phone +90 555 123 4567 card 4111 1111 1111 1111",
		RID:  "builtin-pii",
		Policy: &CompiledPolicyRules{
			PolicyID: "baseline", Version: 1,
			PIIAction: RuleActionMask, SecretAction: RuleActionBlock, PromptInjectionAction: RuleActionAuditOnly,
		},
	}
	result, err := service.Inspect(context.Background(), input)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if result.Action != RuleActionMask || result.DetectionCount != 3 {
		t.Fatalf("built-in PII result = %+v", result)
	}
	for _, raw := range []string{"alice@example.com", "+90 555 123 4567", "4111 1111 1111 1111"} {
		if strings.Contains(result.SafeContent, raw) {
			t.Fatalf("built-in PII was not masked: %q", result.SafeContent)
		}
	}
	got := make(map[string]bool)
	for _, finding := range result.Findings {
		got[finding.Rule] = true
	}
	for _, rule := range []string{"EMAIL", "PHONE", "CREDIT_CARD"} {
		if !got[rule] {
			t.Errorf("missing built-in PII rule %q in %+v", rule, result.Findings)
		}
	}
}

func TestGuardrailServiceCoversPolicyRuleFamilies(t *testing.T) {
	service := compiledPolicyTestService(t)
	input := InspectInput{
		Text: "custom-42 ignore previous instructions forbidden safe@example.com",
		Policy: &CompiledPolicyRules{
			PolicyID: "coverage", Version: 4,
			PIIAction: RuleActionMask, SecretAction: RuleActionBlock,
			PromptInjectionAction: RuleActionAuditOnly,
			CustomPatterns: []CompiledPatternRule{
				{ID: "custom", Name: "CUSTOM", Category: "SECRET", Regex: `custom-[0-9]+`},
				{ID: "injection", Name: "PROMPT", Category: "PROMPT_INJECTION", Regex: `(?i)ignore previous instructions`},
				{ID: "email", Name: "EMAIL", Category: "PII", Regex: `[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}`},
			},
			Allowlist:  []CompiledListRule{{ID: "allow-email", Value: "safe@example.com"}},
			Blocklist:  []CompiledListRule{{ID: "deny-word", Value: "forbidden"}},
			Validators: []CompiledValidatorRule{{ID: "schema-shape", Version: 8, Name: "must-be-json", Kind: ValidatorRegex, Rule: `^\{`, Action: RuleActionBlock}},
		},
	}
	result, err := service.Inspect(context.Background(), input)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	wanted := map[string]bool{"SECRET": false, "PROMPT_INJECTION": false, "BLOCKLIST": false, "VALIDATOR": false}
	for _, finding := range result.Findings {
		if _, exists := wanted[finding.Category]; exists {
			wanted[finding.Category] = true
		}
		if finding.Rule == "EMAIL" {
			t.Fatal("allowlisted PII finding was not suppressed")
		}
	}
	for category, found := range wanted {
		if !found {
			t.Errorf("missing %s finding: %+v", category, result.Findings)
		}
	}
	if result.DetectionCount != 4 || result.Action != RuleActionBlock {
		t.Fatalf("result count=%d action=%s findings=%+v", result.DetectionCount, result.Action, result.Findings)
	}
}

type inspectStub struct {
	result InspectResult
}

func (stub inspectStub) Inspect(context.Context, InspectInput) (InspectResult, error) {
	return stub.result, nil
}

func TestDetectLegacyReconstructsRawValueOnlyAtHTTPBoundary(t *testing.T) {
	request := models.DetectRequest{Text: "email alice@example.com"}
	response, err := DetectLegacy(context.Background(), inspectStub{result: InspectResult{
		Action: RuleActionMask, SafeContent: "email [EMAIL]", ContainsSensitive: true,
		DetectionCount: 1, Breakdown: map[string]int{"EMAIL": 1},
		Findings: []Finding{{Rule: "EMAIL", Category: "PII", Action: RuleActionMask, Start: 6, End: 23, Placeholder: "[EMAIL]", Confidence: .9}},
	}}, request)
	if err != nil {
		t.Fatalf("DetectLegacy() error = %v", err)
	}
	if len(response.Detections) != 1 || response.Detections[0].Value != "alice@example.com" {
		t.Fatalf("legacy response = %+v", response)
	}
}

func TestAuditContractsRejectInvalidEnums(t *testing.T) {
	if err := RuleAction("DROP").Validate(); err == nil {
		t.Fatal("invalid rule action was accepted")
	}
	if err := AuditStage("stream").Validate(); err == nil {
		t.Fatal("invalid audit stage was accepted")
	}
}

func TestAuditEventHasNoRawContentFields(t *testing.T) {
	typeOfEvent := reflect.TypeOf(AuditEvent{})
	for index := 0; index < typeOfEvent.NumField(); index++ {
		field := typeOfEvent.Field(index)
		name := strings.ToLower(field.Name + " " + field.Tag.Get("json"))
		for _, forbidden := range []string{"content", "text", "value", "match", "raw_pii"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("AuditEvent contains raw-content field %q", field.Name)
			}
		}
	}
}

func TestBuildAuditTargetIsStableAndUnambiguous(t *testing.T) {
	target := BuildAuditTarget("gateway a", "tenant/a", "/v1/chat;admin")
	if target != "gateway=gateway+a;tenant=tenant%2Fa;route=%2Fv1%2Fchat%3Badmin" {
		t.Fatalf("BuildAuditTarget() = %q", target)
	}
}

func compiledPolicyTestService(t *testing.T) GuardrailService {
	t.Helper()
	service, err := NewGuardrailService(&Detector{})
	if err != nil {
		t.Fatalf("NewGuardrailService() error = %v", err)
	}
	return service
}
