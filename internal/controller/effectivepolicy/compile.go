package effectivepolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	securityv1alpha1 "thyris-sz/api/v1alpha1"
	"thyris-sz/internal/extproc/policy"
)

// Compiler runs the existing policy lifecycle for controller-owned Inline
// policies. It does not implement a second lifecycle.
type Compiler struct {
	Repo      policy.Repository
	Compiler  *policy.Compiler
	Activator *policy.Activator
}

// RepublishActivation retries only Redis notification for a snapshot that is
// already Active. It must be used after ActivationPublishError instead of
// running the activation transaction a second time.
func (c *Compiler) RepublishActivation(ctx context.Context, policyName string, tenant *string) error {
	if c == nil || c.Activator == nil {
		return errors.New("effective policy activator is required")
	}
	return c.Activator.RepublishActivation(ctx, policyName, tenant)
}

// EnsureCompiledAndActive creates, compiles, and activates a new snapshot only
// when the active definition differs from def. Repository errors other than a
// missing active snapshot are returned: a controller must not write a policy
// during an unavailable policy-store incident.
func (c *Compiler) EnsureCompiledAndActive(ctx context.Context, dbPolicyName string, def policy.PolicyDefinition) (policy.PolicySnapshot, bool, error) {
	if c == nil || c.Repo == nil || c.Compiler == nil || c.Activator == nil {
		return policy.PolicySnapshot{}, false, errors.New("effective policy compiler dependencies are required")
	}
	dbPolicyName = strings.TrimSpace(dbPolicyName)
	if dbPolicyName == "" {
		return policy.PolicySnapshot{}, false, errors.New("effective policy name is required")
	}
	desiredHash, err := policy.DefinitionIntegrityHash(def)
	if err != nil {
		return policy.PolicySnapshot{}, false, fmt.Errorf("hash effective policy definition: %w", err)
	}

	active, err := c.Repo.ActiveSnapshot(ctx, dbPolicyName, def.Scope.Tenant)
	switch {
	case err == nil && active.IntegrityHash == desiredHash:
		return active, false, nil
	case err != nil && !errors.Is(err, policy.ErrNotFound):
		return policy.PolicySnapshot{}, false, fmt.Errorf("load active effective policy: %w", err)
	}

	snapshotID, err := c.Repo.CreateValidated(ctx, dbPolicyName, def)
	if err != nil {
		return policy.PolicySnapshot{}, false, fmt.Errorf("create validated effective policy: %w", err)
	}
	if err := c.Compiler.Compile(ctx, snapshotID); err != nil {
		return policy.PolicySnapshot{}, false, fmt.Errorf("compile effective policy: %w", err)
	}
	if err := c.Activator.Activate(ctx, snapshotID); err != nil {
		return policy.PolicySnapshot{}, false, fmt.Errorf("activate effective policy: %w", err)
	}
	active, err = c.Repo.SnapshotByID(ctx, snapshotID)
	if err != nil {
		return policy.PolicySnapshot{}, false, fmt.Errorf("load activated effective policy: %w", err)
	}
	return active, true, nil
}

// ToPolicyDefinition converts an Inline CRD policy without side effects. The
// compiler lifecycle subsequently resolves external references atomically.
func ToPolicyDefinition(spec securityv1alpha1.TSZGuardrailPolicySpec, scope policy.Scope) (policy.PolicyDefinition, error) {
	if spec.PolicySource != securityv1alpha1.PolicySourceInline || spec.Request == nil || spec.Response == nil {
		return policy.PolicyDefinition{}, errors.New("inline policy source requires request and response policy specs")
	}
	definition := policy.PolicyDefinition{
		Scope: scope,
		Request: policy.RequestPolicy{
			PII:              toPolicyAction(spec.Request.PII),
			Secret:           toPolicyAction(spec.Request.Secret),
			PromptInjection:  toPolicyAction(spec.Request.PromptInjection),
			CustomPatternIDs: append([]string(nil), spec.Request.CustomPatternIDs...),
			AllowlistIDs:     append([]string(nil), spec.Request.AllowlistIDs...),
			BlocklistIDs:     append([]string(nil), spec.Request.BlocklistIDs...),
			CustomValidators: toValidatorReferences(spec.Request.CustomValidators),
		},
		Response: policy.ResponsePolicy{
			Enabled:          spec.Response.Enabled,
			PII:              toPolicyAction(spec.Response.PII),
			Secret:           toPolicyAction(spec.Response.Secret),
			UnsafeContent:    toPolicyAction(spec.Response.UnsafeContent),
			CustomPatternIDs: append([]string(nil), spec.Response.CustomPatternIDs...),
			CustomValidators: toValidatorReferences(spec.Response.CustomValidators),
		},
		FailurePolicy: policy.FailurePolicy{
			Request:  toFailureMode(spec.FailurePolicy.Request),
			Response: toFailureMode(spec.FailurePolicy.Response),
		},
		Telemetry: policy.TelemetrySettings{
			Enabled: spec.Telemetry.Enabled, MetricsEnabled: spec.Telemetry.MetricsEnabled,
			TracingEnabled: spec.Telemetry.TracingEnabled, SampleRate: spec.Telemetry.SampleRate,
		},
	}
	if err := policy.ValidateDefinition(definition); err != nil {
		return policy.PolicyDefinition{}, err
	}
	return definition, nil
}

// InlinePolicyName returns a stable, database-safe identity for one CRD and
// target. The target key is deliberately included so attachment-specific
// definitions never collide.
func InlinePolicyName(namespace, policyName, targetKey string) string {
	digest := sha256.Sum256([]byte(targetKey))
	return fmt.Sprintf("crd/%s/%s/%s", namespace, policyName, hex.EncodeToString(digest[:8]))
}

func toPolicyAction(action securityv1alpha1.PolicyAction) policy.Action {
	switch action {
	case securityv1alpha1.PolicyActionAllow:
		return policy.ActionAllow
	case securityv1alpha1.PolicyActionMask:
		return policy.ActionMask
	case securityv1alpha1.PolicyActionBlock:
		return policy.ActionBlock
	case securityv1alpha1.PolicyActionAuditOnly:
		return policy.ActionAuditOnly
	default:
		return policy.Action(action)
	}
}

func toFailureMode(mode securityv1alpha1.FailureMode) policy.FailureMode {
	switch mode {
	case securityv1alpha1.FailureModeOpen:
		return policy.FailureModeOpen
	case securityv1alpha1.FailureModeClosed:
		return policy.FailureModeClosed
	default:
		return policy.FailureMode(mode)
	}
}

func toValidatorReferences(references []securityv1alpha1.ValidatorReference) []policy.ValidatorReference {
	converted := make([]policy.ValidatorReference, 0, len(references))
	for _, reference := range references {
		converted = append(converted, policy.ValidatorReference{ID: reference.ID, Version: int(reference.Version)})
	}
	return converted
}
