package policy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// CRDOwnershipTracker records which TSZGuardrailPolicy objects currently use
// controller-owned Inline policies. Snapshot rows are intentionally retained
// when the last owner disappears so the audit trail remains immutable.
type CRDOwnershipTracker struct {
	db *sql.DB
}

func NewCRDOwnershipTracker(db *sql.DB) (*CRDOwnershipTracker, error) {
	if db == nil {
		return nil, errors.New("PostgreSQL database is required")
	}
	return &CRDOwnershipTracker{db: db}, nil
}

func (t *CRDOwnershipTracker) ClaimOwnership(ctx context.Context, policyName string, tenant *string, namespace, ownerName string) error {
	if err := validateCRDOwnership(policyName, namespace, ownerName); err != nil {
		return err
	}
	if _, err := t.db.ExecContext(ctx, `
		INSERT INTO owner_crd_refs (policy_name, tenant, owner_namespace, owner_name)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (policy_name, tenant, owner_namespace, owner_name)
		DO UPDATE SET updated_at = now()`, strings.TrimSpace(policyName), tenant, namespace, ownerName); err != nil {
		return fmt.Errorf("claim Inline policy ownership: %w", err)
	}
	return nil
}

func (t *CRDOwnershipTracker) ReleaseOwnership(ctx context.Context, policyName string, tenant *string, namespace, ownerName string) error {
	if err := validateCRDOwnership(policyName, namespace, ownerName); err != nil {
		return err
	}
	if _, err := t.db.ExecContext(ctx, `
		DELETE FROM owner_crd_refs
		WHERE policy_name = $1 AND tenant IS NOT DISTINCT FROM $2
		  AND owner_namespace = $3 AND owner_name = $4`, strings.TrimSpace(policyName), tenant, namespace, ownerName); err != nil {
		return fmt.Errorf("release Inline policy ownership: %w", err)
	}
	return nil
}

func validateCRDOwnership(policyName, namespace, ownerName string) error {
	if strings.TrimSpace(policyName) == "" || strings.TrimSpace(namespace) == "" || strings.TrimSpace(ownerName) == "" {
		return errors.New("Inline policy ownership requires policy name, namespace, and owner name")
	}
	return nil
}
