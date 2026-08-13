package effectivepolicy

import (
	"context"
	"testing"

	securityv1alpha1 "thyris-sz/api/v1alpha1"
	"thyris-sz/internal/extproc/policy"
)

func TestToPolicyDefinitionConvertsInlinePolicyWithoutMutation(t *testing.T) {
	spec := inlineSpec()
	definition, err := ToPolicyDefinition(spec, policy.Scope{Gateway: "edge", Route: "orders"})
	if err != nil {
		t.Fatalf("ToPolicyDefinition() error = %v", err)
	}
	if definition.Request.PII != policy.ActionMask || definition.Response.UnsafeContent != policy.ActionBlock {
		t.Fatalf("ToPolicyDefinition() actions = %+v", definition)
	}
	if definition.FailurePolicy.Request != policy.FailureModeClosed || definition.Telemetry.SampleRate != 0.25 {
		t.Fatalf("ToPolicyDefinition() settings = %+v", definition)
	}
	definition.Request.CustomPatternIDs[0] = "changed"
	if spec.Request.CustomPatternIDs[0] == "changed" {
		t.Fatal("ToPolicyDefinition() shared CRD slice storage")
	}
}

func TestToPolicyDefinitionRejectsNonInlineAndInvalidActions(t *testing.T) {
	if _, err := ToPolicyDefinition(securityv1alpha1.TSZGuardrailPolicySpec{}, policy.Scope{}); err == nil {
		t.Fatal("ToPolicyDefinition() error = nil for non-inline spec")
	}
	spec := inlineSpec()
	spec.Request.PII = "unexpected"
	if _, err := ToPolicyDefinition(spec, policy.Scope{}); err == nil {
		t.Fatal("ToPolicyDefinition() error = nil for invalid action")
	}
}

func TestInlinePolicyNameIsDeterministicAndTargetSpecific(t *testing.T) {
	first := InlinePolicyName("apps", "guardrails", "Gateway/edge")
	if first != InlinePolicyName("apps", "guardrails", "Gateway/edge") {
		t.Fatal("InlinePolicyName() is not deterministic")
	}
	if first == InlinePolicyName("apps", "guardrails", "HTTPRoute/orders") {
		t.Fatal("InlinePolicyName() collided across targets")
	}
}

func TestEnsureCompiledAndActiveIsIdempotentForMatchingActiveHash(t *testing.T) {
	definition, err := ToPolicyDefinition(inlineSpec(), policy.Scope{})
	if err != nil {
		t.Fatalf("ToPolicyDefinition() error = %v", err)
	}
	hash, err := policy.DefinitionIntegrityHash(definition)
	if err != nil {
		t.Fatalf("DefinitionIntegrityHash() error = %v", err)
	}
	repository := &activeRepository{active: policy.PolicySnapshot{ID: 42, Status: policy.StatusActive, IntegrityHash: hash}}
	compiler, err := policy.NewCompiler(repository)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	activator, err := policy.NewActivator(repository, noOpPublisher{})
	if err != nil {
		t.Fatalf("NewActivator() error = %v", err)
	}
	active, changed, err := (&Compiler{Repo: repository, Compiler: compiler, Activator: activator}).EnsureCompiledAndActive(context.Background(), "crd/apps/guardrails/test", definition)
	if err != nil || changed || active.ID != 42 {
		t.Fatalf("EnsureCompiledAndActive() = (%+v, %t, %v), want existing snapshot and no change", active, changed, err)
	}
	if repository.createCalled {
		t.Fatal("EnsureCompiledAndActive() created a snapshot for an identical active definition")
	}
}

type activeRepository struct {
	policy.Repository
	active       policy.PolicySnapshot
	createCalled bool
}

func (r *activeRepository) ActiveSnapshot(context.Context, string, *string) (policy.PolicySnapshot, error) {
	return r.active, nil
}

func (r *activeRepository) CreateValidated(context.Context, string, policy.PolicyDefinition) (int64, error) {
	r.createCalled = true
	return 0, nil
}

type noOpPublisher struct{}

func (noOpPublisher) PublishActivation(context.Context, policy.ActivationEvent) error { return nil }

func inlineSpec() securityv1alpha1.TSZGuardrailPolicySpec {
	return securityv1alpha1.TSZGuardrailPolicySpec{
		PolicySource:  securityv1alpha1.PolicySourceInline,
		Request:       &securityv1alpha1.RequestPolicySpec{PII: securityv1alpha1.PolicyActionMask, Secret: securityv1alpha1.PolicyActionBlock, PromptInjection: securityv1alpha1.PolicyActionAuditOnly, CustomPatternIDs: []string{"1"}},
		Response:      &securityv1alpha1.ResponsePolicySpec{Enabled: true, PII: securityv1alpha1.PolicyActionMask, Secret: securityv1alpha1.PolicyActionBlock, UnsafeContent: securityv1alpha1.PolicyActionBlock},
		FailurePolicy: securityv1alpha1.FailurePolicySpec{Request: securityv1alpha1.FailureModeClosed, Response: securityv1alpha1.FailureModeOpen},
		Telemetry:     securityv1alpha1.TelemetrySpec{Enabled: true, SampleRate: 0.25},
	}
}
