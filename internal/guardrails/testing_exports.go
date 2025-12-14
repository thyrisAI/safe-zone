//go:build test
// +build test

package guardrails

import "thyris-sz/internal/models"

// This file exposes a minimal set of helpers intended ONLY for unit tests
// living under the top-level tests/ tree. These keep the production code
// encapsulated while allowing enterprise-grade test coverage.

func TestResolveActionForUnit(score, allow, block float64) string {
	return resolveAction(score, allow, block)
}

func TestRoundConfidenceForUnit(v float64) float64 {
	return roundConfidence(v)
}

func TestGetAllowThresholdForUnit() float64 {
	return getAllowThreshold()
}

func TestGetBlockThresholdForUnit() float64 {
	return getBlockThreshold()
}

func TestIsValidJSONForUnit(s string) bool {
	return isValidJSON(s)
}

func TestIsValidXMLForUnit(s string) bool {
	return isValidXML(s)
}

func TestIsValidSchemaForUnit(jsonContent, schemaContent string) (bool, error) {
	return isValidSchema(jsonContent, schemaContent)
}

func TestGeneratePlaceholderForUnit(patternName, rid string) string {
	return generatePlaceholder(patternName, rid)
}

// SIEM helper for unit tests
func TestPublishSecurityEventForUnit(ev models.SecurityEvent) {
	publishSecurityEvent(ev)
}
