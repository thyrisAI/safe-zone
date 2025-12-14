package unit

import (
	"os"
	"strings"
	"testing"

	"thyris-sz/internal/guardrails"
)

// --- resolveAction tests ---

func TestResolveAction_BlockWhenAboveBlockThreshold(t *testing.T) {
	action := guardrails.TestResolveActionForUnit(0.9, 0.3, 0.85)
	if action != "BLOCK" {
		t.Fatalf("expected BLOCK, got %s", action)
	}
}

func TestResolveAction_AllowWhenBelowAllowThreshold(t *testing.T) {
	action := guardrails.TestResolveActionForUnit(0.1, 0.3, 0.85)
	if action != "ALLOW" {
		t.Fatalf("expected ALLOW, got %s", action)
	}
}

func TestResolveAction_MaskWhenBetweenThresholds(t *testing.T) {
	action := guardrails.TestResolveActionForUnit(0.5, 0.3, 0.85)
	if action != "MASK" {
		t.Fatalf("expected MASK, got %s", action)
	}
}

// --- rounding tests ---

func TestRoundConfidence_RoundsToTwoDecimals(t *testing.T) {
	if got := guardrails.TestRoundConfidenceForUnit(0.944); got != 0.94 {
		t.Fatalf("expected 0.94, got %v", got)
	}
	if got := guardrails.TestRoundConfidenceForUnit(0.945); got != 0.95 {
		t.Fatalf("expected 0.95, got %v", got)
	}
}

// --- threshold tests ---

func TestGetAllowThreshold_Default(t *testing.T) {
	os.Unsetenv("CONFIDENCE_ALLOW_THRESHOLD")
	if th := guardrails.TestGetAllowThresholdForUnit(); th != 0.30 {
		t.Fatalf("expected default 0.30, got %v", th)
	}
}

func TestGetAllowThreshold_FromEnv(t *testing.T) {
	_ = os.Setenv("CONFIDENCE_ALLOW_THRESHOLD", "0.42")
	defer os.Unsetenv("CONFIDENCE_ALLOW_THRESHOLD")
	if th := guardrails.TestGetAllowThresholdForUnit(); th != 0.42 {
		t.Fatalf("expected 0.42, got %v", th)
	}
}

func TestGetBlockThreshold_Default(t *testing.T) {
	os.Unsetenv("CONFIDENCE_BLOCK_THRESHOLD")
	if th := guardrails.TestGetBlockThresholdForUnit(); th != 0.85 {
		t.Fatalf("expected default 0.85, got %v", th)
	}
}

func TestGetBlockThreshold_FromEnv(t *testing.T) {
	_ = os.Setenv("CONFIDENCE_BLOCK_THRESHOLD", "0.91")
	defer os.Unsetenv("CONFIDENCE_BLOCK_THRESHOLD")
	if th := guardrails.TestGetBlockThresholdForUnit(); th != 0.91 {
		t.Fatalf("expected 0.91, got %v", th)
	}
}

func TestGetCategoryThreshold_UsesCategorySpecificEnv(t *testing.T) {
	_ = os.Setenv("CONFIDENCE_PII_THRESHOLD", "0.77")
	defer os.Unsetenv("CONFIDENCE_PII_THRESHOLD")
	if th := guardrails.GetCategoryThreshold("PII"); th != 0.77 {
		t.Fatalf("expected 0.77, got %v", th)
	}
}

// --- utils tests ---

func TestApplyRegexHitWeight_IncreasesWithHits(t *testing.T) {
	base := 0.5

	if s := guardrails.ApplyRegexHitWeight(base, 1); s != base {
		t.Fatalf("expected base for 1 hit, got %v", s)
	}

	if s := guardrails.ApplyRegexHitWeight(base, 2); s <= base {
		t.Fatalf("expected score > base for 2 hits, got %v", s)
	}

	if s := guardrails.ApplyRegexHitWeight(base, 3); s <= guardrails.ApplyRegexHitWeight(base, 2) {
		t.Fatalf("expected score for 3 hits > score for 2 hits, got %v", s)
	}

	if s := guardrails.ApplyRegexHitWeight(0.9, 10); s > 1 {
		t.Fatalf("expected score to be clamped at 1, got %v", s)
	}
}

// --- confidence tests ---

func TestComputeConfidence_BlacklistHitAlwaysOne(t *testing.T) {
	ctx := guardrails.ConfidenceContext{BlacklistHit: true}
	if s := guardrails.ComputeConfidence(ctx); s != 1 {
		t.Fatalf("expected 1 for blacklist hit, got %v", s)
	}
}

func TestComputeConfidence_AllowlistLowersScore(t *testing.T) {
	ctx := guardrails.ConfidenceContext{AllowlistHit: true, Source: "REGEX", PatternCategory: "PII", PatternActive: true}
	s := guardrails.ComputeConfidence(ctx)
	if s >= 0.5 {
		t.Fatalf("expected low confidence for allowlist hit, got %v", s)
	}
}

func TestComputeConfidence_RespectsSourceAndCategory(t *testing.T) {
	ctxRegexPII := guardrails.ConfidenceContext{Source: "REGEX", PatternCategory: "PII", PatternActive: true}
	ctxAISec := guardrails.ConfidenceContext{Source: "AI", PatternCategory: "SECRET", PatternActive: true}

	if sRegex := guardrails.ComputeConfidence(ctxRegexPII); sRegex <= 0 {
		t.Fatalf("expected positive score for regex PII, got %v", sRegex)
	}

	if sAI := guardrails.ComputeConfidence(ctxAISec); sAI <= 0 {
		t.Fatalf("expected positive score for AI SECRET, got %v", sAI)
	}
}

// --- validators: builtin JSON/XML, regex, schema, AI prompt ---

func TestIsValidJSON_PositiveAndNegative(t *testing.T) {
	if !guardrails.TestIsValidJSONForUnit("{\"foo\": 123}") {
		t.Fatalf("expected valid JSON")
	}
	if guardrails.TestIsValidJSONForUnit("{foo:}") {
		t.Fatalf("expected invalid JSON")
	}
}

func TestIsValidXML_PositiveAndNegative(t *testing.T) {
	if !guardrails.TestIsValidXMLForUnit("<root><child>ok</child></root>") {
		t.Fatalf("expected valid XML")
	}
	if guardrails.TestIsValidXMLForUnit("<root><child></root>") {
		t.Fatalf("expected invalid XML")
	}
}

func TestIsValidSchema_SimpleMatch(t *testing.T) {
	jsonContent := `{"name": "Alice", "age": 30}`
	schemaContent := `{"type":"object","properties":{"name":{"type":"string"},"age":{"type":"integer"}},"required":["name","age"]}`

	ok, err := guardrails.TestIsValidSchemaForUnit(jsonContent, schemaContent)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !ok {
		t.Fatalf("expected schema validation to pass")
	}
}

// --- placeholder tests ---

func TestGeneratePlaceholder_WithRIDIncludesRIDAndPattern(t *testing.T) {
	ph := guardrails.TestGeneratePlaceholderForUnit("EMAIL", "RID-123")
	if ph == "" {
		t.Fatalf("expected non-empty placeholder")
	}
	if !strings.Contains(ph, "RID-123") || !strings.Contains(ph, "EMAIL") {
		t.Fatalf("expected placeholder to contain RID and pattern name, got %s", ph)
	}
}

func TestGeneratePlaceholder_WithoutRIDIncludesPatternOnly(t *testing.T) {
	ph := guardrails.TestGeneratePlaceholderForUnit("EMAIL", "")
	if ph == "" {
		t.Fatalf("expected non-empty placeholder")
	}
	if !strings.Contains(ph, "EMAIL") {
		t.Fatalf("expected placeholder to contain pattern name, got %s", ph)
	}
}
