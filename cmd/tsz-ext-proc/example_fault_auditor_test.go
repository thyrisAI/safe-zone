package main

import (
	"context"
	"testing"

	"thyris-sz/internal/guardrails"
)

func TestExampleFaultAuditorFailsRequestStageOnly(t *testing.T) {
	auditor := exampleFaultAuditor{}
	if err := auditor.Audit(context.Background(), guardrails.AuditEvent{Stage: guardrails.AuditStageRequest}); err == nil {
		t.Fatal("request-stage audit error = nil, want injected failure")
	}
	if err := auditor.Audit(context.Background(), guardrails.AuditEvent{Stage: guardrails.AuditStageResponse}); err != nil {
		t.Fatalf("response-stage audit error = %v, want nil", err)
	}
}
