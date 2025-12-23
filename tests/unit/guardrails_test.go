package unit

import (
	"fmt"
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

// --- Additional edge case tests ---

func TestResolveAction_EdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		score  float64
		allow  float64
		block  float64
		expect string
	}{
		{"Score equals allow threshold", 0.3, 0.3, 0.85, "MASK"},
		{"Score equals block threshold", 0.85, 0.3, 0.85, "BLOCK"},
		{"Negative score", -0.1, 0.3, 0.85, "ALLOW"},
		{"Score above 1.0", 1.5, 0.3, 0.85, "BLOCK"},
		{"Invalid thresholds (allow > block)", 0.5, 0.9, 0.1, "MASK"}, // Should still work
		{"Zero thresholds", 0.5, 0.0, 0.0, "BLOCK"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := guardrails.TestResolveActionForUnit(tt.score, tt.allow, tt.block)
			if action != tt.expect {
				t.Fatalf("expected %s, got %s for score=%.2f, allow=%.2f, block=%.2f",
					tt.expect, action, tt.score, tt.allow, tt.block)
			}
		})
	}
}

func TestRoundConfidence_EdgeCases(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
	}{
		{0.0, 0.0},
		{1.0, 1.0},
		{0.999, 1.0},
		{0.001, 0.0},
		{0.125, 0.13}, // Rounds up
		{0.124, 0.12}, // Rounds down
		{-0.1, -0.1},  // Negative values
		{1.1, 1.1},    // Values above 1
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%.3f", tt.input), func(t *testing.T) {
			result := guardrails.TestRoundConfidenceForUnit(tt.input)
			if result != tt.expected {
				t.Fatalf("expected %.2f, got %.2f for input %.3f", tt.expected, result, tt.input)
			}
		})
	}
}

func TestApplyRegexHitWeight_ExtensiveCases(t *testing.T) {
	tests := []struct {
		name     string
		base     float64
		hits     int
		minScore float64
		maxScore float64
	}{
		{"Zero base", 0.0, 5, 0.0, 1.0},
		{"High base with many hits", 0.95, 10, 0.95, 1.0},
		{"Medium base progression", 0.5, 1, 0.5, 0.5},
		{"Medium base with hits", 0.5, 3, 0.5, 1.0},
		{"Negative hits (edge case)", 0.5, -1, 0.0, 1.0},
		{"Zero hits", 0.5, 0, 0.0, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := guardrails.ApplyRegexHitWeight(tt.base, tt.hits)
			if result < tt.minScore || result > tt.maxScore {
				t.Fatalf("expected result between %.2f and %.2f, got %.2f",
					tt.minScore, tt.maxScore, result)
			}
			if result > 1.0 {
				t.Fatalf("result should never exceed 1.0, got %.2f", result)
			}
		})
	}
}

func TestComputeConfidence_ComplexScenarios(t *testing.T) {
	tests := []struct {
		name string
		ctx  guardrails.ConfidenceContext
		min  float64
		max  float64
	}{
		{
			"Blacklist hit overrides everything",
			guardrails.ConfidenceContext{
				BlacklistHit:    true,
				AllowlistHit:    true,
				Source:          "AI",
				PatternCategory: "PII",
				PatternActive:   true,
			},
			1.0, 1.0,
		},
		{
			"Allowlist hit with AI source",
			guardrails.ConfidenceContext{
				AllowlistHit:    true,
				Source:          "AI",
				PatternCategory: "SECRET",
				PatternActive:   true,
			},
			0.0, 0.5,
		},
		{
			"Inactive pattern",
			guardrails.ConfidenceContext{
				Source:          "REGEX",
				PatternCategory: "PII",
				PatternActive:   false,
			},
			0.0, 1.0,
		},
		{
			"Unknown source",
			guardrails.ConfidenceContext{
				Source:          "UNKNOWN",
				PatternCategory: "PII",
				PatternActive:   true,
			},
			0.0, 1.0,
		},
		{
			"Empty category",
			guardrails.ConfidenceContext{
				Source:          "REGEX",
				PatternCategory: "",
				PatternActive:   true,
			},
			0.0, 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := guardrails.ComputeConfidence(tt.ctx)
			if result < tt.min || result > tt.max {
				t.Fatalf("expected confidence between %.2f and %.2f, got %.2f",
					tt.min, tt.max, result)
			}
		})
	}
}

func TestValidators_ExtensiveJSON(t *testing.T) {
	tests := []struct {
		name  string
		json  string
		valid bool
	}{
		{"Empty object", "{}", true},
		{"Empty array", "[]", true},
		{"Nested object", `{"a":{"b":{"c":123}}}`, true},
		{"Array with objects", `[{"id":1},{"id":2}]`, true},
		{"Special characters", `{"unicode":"🚀","emoji":"😀"}`, true},
		{"Numbers", `{"int":42,"float":3.14,"exp":1e10}`, true},
		{"Booleans and null", `{"bool":true,"null":null,"false":false}`, true},
		{"Unclosed brace", `{"key":"value"`, false},
		{"Trailing comma", `{"key":"value",}`, false},
		{"Single quotes", `{'key':'value'}`, false},
		{"Unquoted keys", `{key:"value"}`, false},
		{"Invalid escape", `{"key":"invalid\escape"}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := guardrails.TestIsValidJSONForUnit(tt.json)
			if result != tt.valid {
				t.Fatalf("expected %v for JSON: %s", tt.valid, tt.json)
			}
		})
	}
}

func TestValidators_ExtensiveXML(t *testing.T) {
	tests := []struct {
		name  string
		xml   string
		valid bool
	}{
		{"Self-closing tag", "<tag/>", true},
		{"With attributes", `<tag attr="value">content</tag>`, true},
		{"Nested tags", "<root><child><grandchild/></child></root>", true},
		{"CDATA section", "<root><![CDATA[Some data]]></root>", true},
		{"XML declaration", `<?xml version="1.0"?><root/>`, true},
		{"Comments", "<!-- comment --><root/>", true},
		{"Unclosed tag", "<root><child></root>", false},
		{"Mismatched tags", "<root></child>", false},
		{"Invalid characters", "<root>invalid\x00char</root>", false},
		{"Unquoted attributes", "<tag attr=value>content</tag>", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := guardrails.TestIsValidXMLForUnit(tt.xml)
			if result != tt.valid {
				t.Fatalf("expected %v for XML: %s", tt.valid, tt.xml)
			}
		})
	}
}
