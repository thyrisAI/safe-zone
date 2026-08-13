package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotFound               = errors.New("policy record not found")
	ErrInvalidDefinition      = errors.New("invalid policy definition")
	ErrInvalidTransition      = errors.New("invalid policy snapshot transition")
	ErrActiveSnapshotConflict = errors.New("policy already has an active snapshot")
)

// Repository exposes only policy creation and reads needed outside lifecycle
// transactions. Lifecycle mutations are available through Transaction.
type Repository interface {
	CreateValidated(ctx context.Context, policyName string, definition PolicyDefinition) (snapshotID int64, err error)
	PolicyByName(ctx context.Context, policyName string, tenant *string) (Policy, error)
	SnapshotByID(ctx context.Context, snapshotID int64) (PolicySnapshot, error)
	SnapshotByVersion(ctx context.Context, policyName string, tenant *string, version int) (PolicySnapshot, error)
	ActiveSnapshot(ctx context.Context, policyName string, tenant *string) (PolicySnapshot, error)
	ActiveSnapshots(ctx context.Context) ([]PolicySnapshot, error)
	WithinTransaction(ctx context.Context, fn func(Transaction) error) error
}

// Transaction contains the row-locking queries required by the compiler,
// activator and rollback services. It deliberately has no generic status
// update, so draft/staged workflows cannot be introduced accidentally.
type Transaction interface {
	PolicyForUpdate(ctx context.Context, policyName string, tenant *string) (Policy, error)
	LockPolicy(ctx context.Context, policyID int64) error
	TryLockPolicyForActivation(ctx context.Context, policyID int64) error
	SnapshotForUpdate(ctx context.Context, snapshotID int64) (PolicySnapshot, error)
	ResolveReferences(ctx context.Context, definition PolicyDefinition) (PolicyDefinition, error)
	NextVersion(ctx context.Context, policyID int64) (int, error)
	MarkCompiled(ctx context.Context, snapshotID int64, definition PolicyDefinition, version int, integrityHash string, compiledAt time.Time) error
	ActiveSnapshotForUpdate(ctx context.Context, policyID int64) (PolicySnapshot, error)
	LatestSupersededForUpdate(ctx context.Context, policyID int64) (PolicySnapshot, error)
	MarkSuperseded(ctx context.Context, snapshotID int64) error
	MarkActive(ctx context.Context, snapshotID int64, activatedAt time.Time) error
	MarkRolledBack(ctx context.Context, snapshotID int64) error
}

func (r *PostgresRepository) LockPolicy(ctx context.Context, policyID int64) error {
	if r.tx == nil {
		return errors.New("LockPolicy requires a repository transaction")
	}
	var lockedID int64
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM policies WHERE id = $1 FOR UPDATE", policyID).Scan(&lockedID); err != nil {
		return wrapNotFound("lock policy", err)
	}
	return nil
}

// TryLockPolicyForActivation prevents two independently started activations
// from being serialized into two successful state transitions. A caller that
// loses this transaction-scoped lock must report an activation conflict and
// retry only after observing the newly active policy version.
func (r *PostgresRepository) TryLockPolicyForActivation(ctx context.Context, policyID int64) error {
	if r.tx == nil {
		return errors.New("TryLockPolicyForActivation requires a repository transaction")
	}
	var lockedID int64
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM policies WHERE id = $1 FOR UPDATE NOWAIT", policyID).Scan(&lockedID); err != nil {
		return wrapNotFound("lock policy for activation", err)
	}
	return nil
}

type PostgresRepository struct {
	db *sql.DB
	tx *sql.Tx
}

func NewPostgresRepository(db *sql.DB) (*PostgresRepository, error) {
	if db == nil {
		return nil, errors.New("PostgreSQL database is required")
	}
	return &PostgresRepository{db: db}, nil
}

func (r *PostgresRepository) CreateValidated(ctx context.Context, policyName string, definition PolicyDefinition) (int64, error) {
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		return 0, fmt.Errorf("%w: policy name is required", ErrInvalidDefinition)
	}
	if err := ValidateDefinition(definition); err != nil {
		return 0, err
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return 0, fmt.Errorf("marshal policy definition: %w", err)
	}

	if r.tx != nil {
		return r.createValidated(ctx, policyName, definition.Scope.Tenant, encoded)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin create-validated transaction: %w", err)
	}
	txRepository := &PostgresRepository{db: r.db, tx: tx}
	snapshotID, err := txRepository.createValidated(ctx, policyName, definition.Scope.Tenant, encoded)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit create-validated transaction: %w", err)
	}
	return snapshotID, nil
}

func (r *PostgresRepository) createValidated(ctx context.Context, policyName string, tenant *string, definition []byte) (int64, error) {
	queryer := r.queryer()
	if _, err := queryer.ExecContext(ctx, `
		INSERT INTO policies (name, tenant)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, policyName, tenant); err != nil {
		return 0, fmt.Errorf("resolve policy identity: %w", err)
	}

	var policyID int64
	if err := queryer.QueryRowContext(ctx, `
		SELECT id
		FROM policies
		WHERE name = $1 AND tenant IS NOT DISTINCT FROM $2
		FOR UPDATE`, policyName, tenant).Scan(&policyID); err != nil {
		return 0, wrapNotFound("resolve policy identity", err)
	}

	var snapshotID int64
	if err := queryer.QueryRowContext(ctx, `
		INSERT INTO policy_snapshots (policy_id, status, definition)
		VALUES ($1, 'validated', $2::jsonb)
		RETURNING id`, policyID, definition).Scan(&snapshotID); err != nil {
		return 0, fmt.Errorf("create validated snapshot: %w", err)
	}
	return snapshotID, nil
}

func (r *PostgresRepository) PolicyByName(ctx context.Context, policyName string, tenant *string) (Policy, error) {
	return scanPolicy(r.queryer().QueryRowContext(ctx, `
		SELECT id, name, tenant, created_at
		FROM policies
		WHERE name = $1 AND tenant IS NOT DISTINCT FROM $2`, strings.TrimSpace(policyName), tenant))
}

func (r *PostgresRepository) PolicyForUpdate(ctx context.Context, policyName string, tenant *string) (Policy, error) {
	if r.tx == nil {
		return Policy{}, errors.New("PolicyForUpdate requires a repository transaction")
	}
	return scanPolicy(r.tx.QueryRowContext(ctx, `
		SELECT id, name, tenant, created_at
		FROM policies
		WHERE name = $1 AND tenant IS NOT DISTINCT FROM $2
		FOR UPDATE`, strings.TrimSpace(policyName), tenant))
}

func (r *PostgresRepository) SnapshotByID(ctx context.Context, snapshotID int64) (PolicySnapshot, error) {
	return scanSnapshot(r.queryer().QueryRowContext(ctx, snapshotSelect+` WHERE s.id = $1`, snapshotID))
}

// SnapshotByVersion loads an immutable policy snapshot by its user-facing
// version, never by the database's internal snapshot primary key.
func (r *PostgresRepository) SnapshotByVersion(ctx context.Context, policyName string, tenant *string, version int) (PolicySnapshot, error) {
	if version <= 0 {
		return PolicySnapshot{}, fmt.Errorf("load policy snapshot by version: %w", ErrNotFound)
	}
	return scanSnapshot(r.queryer().QueryRowContext(ctx, snapshotSelect+`
		WHERE p.name = $1 AND p.tenant IS NOT DISTINCT FROM $2 AND s.version = $3`,
		strings.TrimSpace(policyName), tenant, version))
}

func (r *PostgresRepository) SnapshotForUpdate(ctx context.Context, snapshotID int64) (PolicySnapshot, error) {
	if r.tx == nil {
		return PolicySnapshot{}, errors.New("SnapshotForUpdate requires a repository transaction")
	}
	return scanSnapshot(r.tx.QueryRowContext(ctx, snapshotSelect+` WHERE s.id = $1 FOR UPDATE OF s`, snapshotID))
}

func (r *PostgresRepository) ActiveSnapshot(ctx context.Context, policyName string, tenant *string) (PolicySnapshot, error) {
	return scanSnapshot(r.queryer().QueryRowContext(ctx, snapshotSelect+`
		WHERE p.name = $1 AND p.tenant IS NOT DISTINCT FROM $2 AND s.status = 'active'`, strings.TrimSpace(policyName), tenant))
}

func (r *PostgresRepository) ActiveSnapshots(ctx context.Context) ([]PolicySnapshot, error) {
	rows, err := r.queryer().QueryContext(ctx, snapshotSelect+` WHERE s.status = 'active' ORDER BY s.policy_id`)
	if err != nil {
		return nil, fmt.Errorf("list active policy snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []PolicySnapshot
	for rows.Next() {
		snapshot, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active policy snapshots: %w", err)
	}
	return snapshots, nil
}

func (r *PostgresRepository) WithinTransaction(ctx context.Context, fn func(Transaction) error) error {
	if fn == nil {
		return errors.New("transaction callback is required")
	}
	if r.tx != nil {
		return fn(r)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin policy transaction: %w", err)
	}
	txRepository := &PostgresRepository{db: r.db, tx: tx}
	if err := fn(txRepository); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit policy transaction: %w", err)
	}
	return nil
}

func (r *PostgresRepository) NextVersion(ctx context.Context, policyID int64) (int, error) {
	if r.tx == nil {
		return 0, errors.New("NextVersion requires a repository transaction")
	}
	var lockedPolicyID int64
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM policies WHERE id = $1 FOR UPDATE", policyID).Scan(&lockedPolicyID); err != nil {
		return 0, wrapNotFound("lock policy for version allocation", err)
	}
	var version int
	if err := r.tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM policy_snapshots
		WHERE policy_id = $1`, policyID).Scan(&version); err != nil {
		return 0, fmt.Errorf("allocate next policy version: %w", err)
	}
	return version, nil
}

func (r *PostgresRepository) MarkCompiled(ctx context.Context, snapshotID int64, definition PolicyDefinition, version int, integrityHash string, compiledAt time.Time) error {
	if version <= 0 || strings.TrimSpace(integrityHash) == "" || compiledAt.IsZero() {
		return fmt.Errorf("%w: compiled version, integrity hash and timestamp are required", ErrInvalidTransition)
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return fmt.Errorf("marshal compiled policy definition: %w", err)
	}
	return r.transition(ctx, `
		UPDATE policy_snapshots
		SET status = 'compiled', definition = $2::jsonb, version = $3, integrity_hash = $4, compiled_at = $5
		WHERE id = $1 AND status = 'validated'`, snapshotID, encoded, version, integrityHash, compiledAt)
}

func (r *PostgresRepository) ResolveReferences(ctx context.Context, definition PolicyDefinition) (PolicyDefinition, error) {
	if r.tx == nil {
		return PolicyDefinition{}, errors.New("ResolveReferences requires a repository transaction")
	}
	if err := ValidateDefinition(definition); err != nil {
		return PolicyDefinition{}, err
	}
	if err := r.requireReferences(ctx, "patterns", definition.Request.CustomPatternIDs); err != nil {
		return PolicyDefinition{}, fmt.Errorf("resolve request custom patterns: %w", err)
	}
	if err := r.requireReferences(ctx, "allowlist", definition.Request.AllowlistIDs); err != nil {
		return PolicyDefinition{}, fmt.Errorf("resolve request allowlist: %w", err)
	}
	if err := r.requireReferences(ctx, "blocklist", definition.Request.BlocklistIDs); err != nil {
		return PolicyDefinition{}, fmt.Errorf("resolve request blocklist: %w", err)
	}
	if err := r.requireReferences(ctx, "patterns", definition.Response.CustomPatternIDs); err != nil {
		return PolicyDefinition{}, fmt.Errorf("resolve response custom patterns: %w", err)
	}
	compiledRules, err := r.resolveRequestRules(ctx, definition.Request)
	if err != nil {
		return PolicyDefinition{}, err
	}
	definition.Request.CompiledRules = compiledRules

	versionAware, err := r.validatorsAreVersionAware(ctx)
	if err != nil {
		return PolicyDefinition{}, err
	}
	for _, group := range []struct {
		name       string
		validators []ValidatorReference
	}{
		{name: "request", validators: definition.Request.CustomValidators},
		{name: "response", validators: definition.Response.CustomValidators},
	} {
		for _, reference := range group.validators {
			id, err := parseReferenceID(reference.ID)
			if err != nil {
				return PolicyDefinition{}, fmt.Errorf("resolve %s validator %q: %w", group.name, reference.ID, err)
			}
			query := "SELECT id FROM format_validators WHERE id = $1 AND deleted_at IS NULL"
			args := []any{id}
			if versionAware {
				query += " AND version = $2"
				args = append(args, reference.Version)
			}
			query += " FOR KEY SHARE"
			var resolvedID int64
			if err := r.tx.QueryRowContext(ctx, query, args...).Scan(&resolvedID); err != nil {
				return PolicyDefinition{}, wrapReferenceError(fmt.Sprintf("%s validator %s version %d", group.name, reference.ID, reference.Version), err)
			}
		}
	}
	return definition, nil
}

func (r *PostgresRepository) resolveRequestRules(ctx context.Context, request RequestPolicy) (CompiledRequestRules, error) {
	rules := CompiledRequestRules{}
	for _, reference := range request.CustomPatternIDs {
		id, err := parseReferenceID(reference)
		if err != nil {
			return CompiledRequestRules{}, err
		}
		var rule CompiledPattern
		if err := r.tx.QueryRowContext(ctx, `
			SELECT id::text, name, regex, COALESCE(category, 'PII')
			FROM patterns WHERE id = $1 AND deleted_at IS NULL FOR KEY SHARE`, id,
		).Scan(&rule.ID, &rule.Name, &rule.Regex, &rule.Category); err != nil {
			return CompiledRequestRules{}, wrapReferenceError("request pattern "+reference, err)
		}
		rule.Action = actionForCategory(request, rule.Category)
		rules.CustomPatterns = append(rules.CustomPatterns, rule)
	}
	for _, reference := range request.AllowlistIDs {
		id, err := parseReferenceID(reference)
		if err != nil {
			return CompiledRequestRules{}, err
		}
		var rule CompiledList
		if err := r.tx.QueryRowContext(ctx, `SELECT id::text, value FROM allowlist WHERE id = $1 AND deleted_at IS NULL FOR KEY SHARE`, id).Scan(&rule.ID, &rule.Value); err != nil {
			return CompiledRequestRules{}, wrapReferenceError("request allowlist "+reference, err)
		}
		rules.Allowlist = append(rules.Allowlist, rule)
	}
	for _, reference := range request.BlocklistIDs {
		id, err := parseReferenceID(reference)
		if err != nil {
			return CompiledRequestRules{}, err
		}
		var rule CompiledList
		if err := r.tx.QueryRowContext(ctx, `SELECT id::text, value FROM blocklist WHERE id = $1 AND deleted_at IS NULL FOR KEY SHARE`, id).Scan(&rule.ID, &rule.Value); err != nil {
			return CompiledRequestRules{}, wrapReferenceError("request blocklist "+reference, err)
		}
		rules.Blocklist = append(rules.Blocklist, rule)
	}
	for _, reference := range request.CustomValidators {
		id, err := parseReferenceID(reference.ID)
		if err != nil {
			return CompiledRequestRules{}, err
		}
		var rule CompiledValidator
		if err := r.tx.QueryRowContext(ctx, `
			SELECT id::text, name, type, COALESCE(rule, ''), COALESCE(expected_response, '')
			FROM format_validators WHERE id = $1 AND deleted_at IS NULL FOR KEY SHARE`, id,
		).Scan(&rule.ID, &rule.Name, &rule.Kind, &rule.Rule, &rule.ExpectedResponse); err != nil {
			return CompiledRequestRules{}, wrapReferenceError("request validator "+reference.ID, err)
		}
		rule.Version = reference.Version
		rule.Action = request.PII
		rules.Validators = append(rules.Validators, rule)
	}
	return rules, nil
}

func actionForCategory(request RequestPolicy, category string) Action {
	switch strings.ToUpper(strings.TrimSpace(category)) {
	case "SECRET":
		return request.Secret
	case "PROMPT_INJECTION", "INJECTION":
		return request.PromptInjection
	default:
		return request.PII
	}
}

func (r *PostgresRepository) requireReferences(ctx context.Context, table string, references []string) error {
	for _, reference := range references {
		id, err := parseReferenceID(reference)
		if err != nil {
			return err
		}
		var resolvedID int64
		query := fmt.Sprintf("SELECT id FROM %s WHERE id = $1 AND deleted_at IS NULL FOR KEY SHARE", table)
		if err := r.tx.QueryRowContext(ctx, query, id).Scan(&resolvedID); err != nil {
			return wrapReferenceError(fmt.Sprintf("%s id %s", table, reference), err)
		}
	}
	return nil
}

func (r *PostgresRepository) validatorsAreVersionAware(ctx context.Context) (bool, error) {
	var versionAware bool
	if err := r.tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'format_validators'
			  AND column_name = 'version'
		)`).Scan(&versionAware); err != nil {
		return false, fmt.Errorf("detect validator version support: %w", err)
	}
	return versionAware, nil
}

func parseReferenceID(reference string) (int64, error) {
	trimmed := strings.TrimSpace(reference)
	value, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("reference id must be a positive integer, got %q", reference)
	}
	return value, nil
}

func wrapReferenceError(reference string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", reference, ErrNotFound)
	}
	return fmt.Errorf("%s: %w", reference, err)
}

func (r *PostgresRepository) ActiveSnapshotForUpdate(ctx context.Context, policyID int64) (PolicySnapshot, error) {
	if r.tx == nil {
		return PolicySnapshot{}, errors.New("ActiveSnapshotForUpdate requires a repository transaction")
	}
	return scanSnapshot(r.tx.QueryRowContext(ctx, snapshotSelect+`
		WHERE s.policy_id = $1 AND s.status = 'active'
		FOR UPDATE OF s`, policyID))
}

func (r *PostgresRepository) LatestSupersededForUpdate(ctx context.Context, policyID int64) (PolicySnapshot, error) {
	if r.tx == nil {
		return PolicySnapshot{}, errors.New("LatestSupersededForUpdate requires a repository transaction")
	}
	return scanSnapshot(r.tx.QueryRowContext(ctx, snapshotSelect+`
		WHERE s.policy_id = $1 AND s.status = 'superseded'
		ORDER BY s.version DESC NULLS LAST, s.id DESC
		LIMIT 1
		FOR UPDATE OF s`, policyID))
}

func (r *PostgresRepository) MarkSuperseded(ctx context.Context, snapshotID int64) error {
	return r.transition(ctx, `UPDATE policy_snapshots SET status = 'superseded' WHERE id = $1 AND status = 'active'`, snapshotID)
}

func (r *PostgresRepository) MarkActive(ctx context.Context, snapshotID int64, activatedAt time.Time) error {
	if activatedAt.IsZero() {
		return fmt.Errorf("%w: activation timestamp is required", ErrInvalidTransition)
	}
	return r.transition(ctx, `
		UPDATE policy_snapshots
		SET status = 'active', activated_at = $2
		WHERE id = $1 AND status IN ('compiled', 'superseded')`, snapshotID, activatedAt)
}

func (r *PostgresRepository) MarkRolledBack(ctx context.Context, snapshotID int64) error {
	return r.transition(ctx, `UPDATE policy_snapshots SET status = 'rolled_back' WHERE id = $1 AND status = 'active'`, snapshotID)
}

func (r *PostgresRepository) transition(ctx context.Context, statement string, args ...any) error {
	if r.tx == nil {
		return errors.New("policy lifecycle transition requires a repository transaction")
	}
	result, err := r.tx.ExecContext(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("update policy snapshot lifecycle: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read policy snapshot update count: %w", err)
	}
	if updated != 1 {
		return ErrInvalidTransition
	}
	return nil
}

func ValidateDefinition(definition PolicyDefinition) error {
	type actionCandidate struct {
		path   string
		action Action
	}
	actions := []actionCandidate{
		{path: "request.pii", action: definition.Request.PII},
		{path: "request.secret", action: definition.Request.Secret},
		{path: "request.prompt_injection", action: definition.Request.PromptInjection},
	}
	if definition.Response.Enabled {
		actions = append(actions,
			actionCandidate{path: "response.pii", action: definition.Response.PII},
			actionCandidate{path: "response.secret", action: definition.Response.Secret},
			actionCandidate{path: "response.unsafe_content", action: definition.Response.UnsafeContent},
		)
	} else {
		optionalResponseActions := []actionCandidate{
			{path: "response.pii", action: definition.Response.PII},
			{path: "response.secret", action: definition.Response.Secret},
			{path: "response.unsafe_content", action: definition.Response.UnsafeContent},
		}
		for _, candidate := range optionalResponseActions {
			if candidate.action != "" {
				actions = append(actions, candidate)
			}
		}
	}
	for _, candidate := range actions {
		if !candidate.action.Valid() {
			return fmt.Errorf("%w: %s action must be ALLOW, MASK, BLOCK or AUDIT_ONLY, got %q", ErrInvalidDefinition, candidate.path, candidate.action)
		}
	}
	for _, validators := range [][]ValidatorReference{definition.Request.CustomValidators, definition.Response.CustomValidators} {
		for _, validator := range validators {
			if strings.TrimSpace(validator.ID) == "" || validator.Version <= 0 {
				return fmt.Errorf("%w: validator references require an id and positive version", ErrInvalidDefinition)
			}
		}
	}
	return nil
}

const snapshotSelect = `
	SELECT s.id, s.policy_id, p.name, p.tenant, s.version, s.status::text,
	       s.definition, s.integrity_hash, s.compiled_at, s.activated_at
	FROM policy_snapshots s
	JOIN policies p ON p.id = s.policy_id`

type rowScanner interface {
	Scan(dest ...any) error
}

type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (r *PostgresRepository) queryer() queryer {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

func scanPolicy(row rowScanner) (Policy, error) {
	var policy Policy
	var tenant sql.NullString
	if err := row.Scan(&policy.ID, &policy.Name, &tenant, &policy.CreatedAt); err != nil {
		return Policy{}, wrapNotFound("load policy", err)
	}
	if tenant.Valid {
		policy.Tenant = &tenant.String
	}
	return policy, nil
}

func scanSnapshot(row rowScanner) (PolicySnapshot, error) {
	var snapshot PolicySnapshot
	var tenant sql.NullString
	var version sql.NullInt64
	var status string
	var definition []byte
	var compiledAt sql.NullTime
	var activatedAt sql.NullTime
	if err := row.Scan(
		&snapshot.ID,
		&snapshot.PolicyID,
		&snapshot.PolicyName,
		&tenant,
		&version,
		&status,
		&definition,
		&snapshot.IntegrityHash,
		&compiledAt,
		&activatedAt,
	); err != nil {
		return PolicySnapshot{}, wrapNotFound("load policy snapshot", err)
	}
	if tenant.Valid {
		snapshot.Tenant = &tenant.String
	}
	if version.Valid {
		value := int(version.Int64)
		snapshot.Version = &value
	}
	snapshot.Status = SnapshotStatus(status)
	if compiledAt.Valid {
		snapshot.CompiledAt = &compiledAt.Time
	}
	if activatedAt.Valid {
		snapshot.ActivatedAt = &activatedAt.Time
	}
	if err := json.Unmarshal(definition, &snapshot.Definition); err != nil {
		return PolicySnapshot{}, fmt.Errorf("decode policy definition: %w", err)
	}
	return snapshot, nil
}

func wrapNotFound(operation string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", operation, ErrNotFound)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ Repository = (*PostgresRepository)(nil)
var _ Transaction = (*PostgresRepository)(nil)
