package policy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestCompilerCompilesValidatedSnapshotAndLocksDefinition(t *testing.T) {
	db, repository := openCompilerTestRepository(t)
	patternID, allowlistID, blocklistID, validatorID := insertReferenceFixtures(t, db)
	definition := validPolicyDefinition()
	definition.Request.CustomPatternIDs = []string{strconv.FormatInt(patternID, 10)}
	definition.Request.AllowlistIDs = []string{strconv.FormatInt(allowlistID, 10)}
	definition.Request.BlocklistIDs = []string{strconv.FormatInt(blocklistID, 10)}
	definition.Request.CustomValidators = []ValidatorReference{{ID: strconv.FormatInt(validatorID, 10), Version: 9}}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snapshotID, err := repository.CreateValidated(ctx, "compiler", definition)
	if err != nil {
		t.Fatalf("CreateValidated() error = %v", err)
	}
	compiler, err := NewCompiler(repository)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	if err := compiler.Compile(ctx, snapshotID); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	snapshot, err := repository.SnapshotByID(ctx, snapshotID)
	if err != nil {
		t.Fatalf("SnapshotByID() error = %v", err)
	}
	if snapshot.Status != StatusCompiled || snapshot.Version == nil || *snapshot.Version != 1 {
		t.Fatalf("compiled snapshot status/version = %s/%v, want compiled/1", snapshot.Status, snapshot.Version)
	}
	if snapshot.CompiledAt == nil || snapshot.CompiledAt.IsZero() {
		t.Fatal("compiled_at was not set")
	}
	if snapshot.ActivatedAt != nil {
		t.Fatalf("activated_at = %v, want nil", snapshot.ActivatedAt)
	}
	canonical, err := CanonicalDefinition(snapshot.Definition)
	if err != nil {
		t.Fatalf("CanonicalDefinition() error = %v", err)
	}
	digest := sha256.Sum256(canonical)
	wantHash := "sha256:" + hex.EncodeToString(digest[:])
	if snapshot.IntegrityHash != wantHash {
		t.Fatalf("integrity hash = %q, want %q", snapshot.IntegrityHash, wantHash)
	}
	if !reflect.DeepEqual(snapshot.Definition.Request.CustomPatternIDs, definition.Request.CustomPatternIDs) ||
		len(snapshot.Definition.Request.CompiledRules.CustomPatterns) != 1 ||
		len(snapshot.Definition.Request.CompiledRules.Allowlist) != 1 ||
		len(snapshot.Definition.Request.CompiledRules.Blocklist) != 1 ||
		len(snapshot.Definition.Request.CompiledRules.Validators) != 1 {
		t.Fatalf("compiled definition did not retain references and material: %+v", snapshot.Definition.Request)
	}

	if _, err := db.ExecContext(ctx, "UPDATE policy_snapshots SET definition = '{}'::jsonb WHERE id = $1", snapshotID); err == nil {
		t.Fatal("compiled definition update succeeded; want database trigger rejection")
	}
	if err := compiler.Compile(ctx, snapshotID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second Compile() error = %v, want ErrInvalidTransition", err)
	}
}

func TestCompilerResolvesImmutableTemplateReferences(t *testing.T) {
	db, repository := openCompilerTestRepository(t)
	migration, err := MigrationFiles.ReadFile("migrations/000004_create_guardrail_template_snapshots.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	patternID, _, _, validatorID := insertReferenceFixtures(t, db)
	template, err := json.Marshal(TemplateDefinition{Request: TemplateRequestRules{
		CustomPatternIDs: []string{strconv.FormatInt(patternID, 10)},
		CustomValidators: []ValidatorReference{{ID: strconv.FormatInt(validatorID, 10), Version: 1}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var templateID int64
	if err := db.QueryRow(`INSERT INTO guardrail_templates (name) VALUES ('banking') RETURNING id`).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO guardrail_template_snapshots (template_id, version, definition) VALUES ($1, 2, $2::jsonb)`, templateID, template); err != nil {
		t.Fatal(err)
	}
	definition := validPolicyDefinition()
	definition.TemplateRefs = []TemplateReference{{Name: "banking", Version: 2}}
	ctx := context.Background()
	snapshotID, err := repository.CreateValidated(ctx, "templated", definition)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := NewCompiler(repository)
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.Compile(ctx, snapshotID); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.SnapshotByID(ctx, snapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Definition.TemplateRefs) != 1 || snapshot.Definition.TemplateRefs[0].Name != "banking" || snapshot.Definition.TemplateRefs[0].Version != 2 {
		t.Fatalf("resolved templates = %+v", snapshot.Definition.TemplateRefs)
	}
	if len(snapshot.Definition.Request.CompiledRules.CustomPatterns) != 1 || len(snapshot.Definition.Request.CompiledRules.Validators) != 1 {
		t.Fatalf("template material was not compiled: %+v", snapshot.Definition.Request.CompiledRules)
	}
}

func TestCompilerRejectsMissingAndMalformedReferences(t *testing.T) {
	_, repository := openCompilerTestRepository(t)
	compiler, err := NewCompiler(repository)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	tests := []struct {
		name      string
		reference string
	}{
		{name: "missing", reference: "999999"},
		{name: "malformed", reference: "pattern-one"},
		{name: "zero", reference: "0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := validPolicyDefinition()
			definition.Request.CustomPatternIDs = []string{test.reference}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			snapshotID, err := repository.CreateValidated(ctx, "invalid-reference-"+test.name, definition)
			if err != nil {
				t.Fatalf("CreateValidated() error = %v", err)
			}
			if err := compiler.Compile(ctx, snapshotID); err == nil {
				t.Fatal("Compile() error = nil, want reference error")
			}
			snapshot, err := repository.SnapshotByID(ctx, snapshotID)
			if err != nil {
				t.Fatalf("SnapshotByID() error = %v", err)
			}
			if snapshot.Status != StatusValidated || snapshot.Version != nil || snapshot.CompiledAt != nil {
				t.Fatalf("failed compile mutated snapshot: %+v", snapshot)
			}
		})
	}
}

func TestCompilerAllocatesVersionsPerPolicyConcurrently(t *testing.T) {
	_, repository := openCompilerTestRepository(t)
	compiler, err := NewCompiler(repository)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	firstA, err := repository.CreateValidated(ctx, "policy-a", validPolicyDefinition())
	if err != nil {
		t.Fatalf("create policy-a first snapshot: %v", err)
	}
	secondA, err := repository.CreateValidated(ctx, "policy-a", validPolicyDefinition())
	if err != nil {
		t.Fatalf("create policy-a second snapshot: %v", err)
	}
	firstB, err := repository.CreateValidated(ctx, "policy-b", validPolicyDefinition())
	if err != nil {
		t.Fatalf("create policy-b snapshot: %v", err)
	}

	ids := []int64{firstA, secondA, firstB}
	start := make(chan struct{})
	errorsChannel := make(chan error, len(ids))
	var waitGroup sync.WaitGroup
	for _, snapshotID := range ids {
		waitGroup.Add(1)
		go func(id int64) {
			defer waitGroup.Done()
			<-start
			errorsChannel <- compiler.Compile(ctx, id)
		}(snapshotID)
	}
	close(start)
	waitGroup.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent Compile() error = %v", err)
		}
	}

	versionsA := make([]int, 0, 2)
	for _, snapshotID := range []int64{firstA, secondA} {
		snapshot, err := repository.SnapshotByID(ctx, snapshotID)
		if err != nil {
			t.Fatalf("load policy-a snapshot: %v", err)
		}
		if snapshot.Version == nil {
			t.Fatalf("policy-a snapshot %d has nil version", snapshotID)
		}
		versionsA = append(versionsA, *snapshot.Version)
	}
	sort.Ints(versionsA)
	if !reflect.DeepEqual(versionsA, []int{1, 2}) {
		t.Fatalf("policy-a versions = %v, want [1 2]", versionsA)
	}
	snapshotB, err := repository.SnapshotByID(ctx, firstB)
	if err != nil {
		t.Fatalf("load policy-b snapshot: %v", err)
	}
	if snapshotB.Version == nil || *snapshotB.Version != 1 {
		t.Fatalf("policy-b version = %v, want 1", snapshotB.Version)
	}
}

func TestCompilerAllocatesSequentialVersionsIndependentlyPerPolicy(t *testing.T) {
	_, repository := openCompilerTestRepository(t)
	compiler, err := NewCompiler(repository)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	compileNew := func(policyName string) PolicySnapshot {
		t.Helper()
		snapshotID, err := repository.CreateValidated(ctx, policyName, validPolicyDefinition())
		if err != nil {
			t.Fatalf("CreateValidated(%q) error = %v", policyName, err)
		}
		if err := compiler.Compile(ctx, snapshotID); err != nil {
			t.Fatalf("Compile(%q) error = %v", policyName, err)
		}
		snapshot, err := repository.SnapshotByID(ctx, snapshotID)
		if err != nil {
			t.Fatalf("SnapshotByID(%q) error = %v", policyName, err)
		}
		return snapshot
	}

	firstA := compileNew("sequential-a")
	secondA := compileNew("sequential-a")
	firstB := compileNew("sequential-b")
	firstC := compileNew("sequential-c")

	if firstA.Version == nil || *firstA.Version != 1 {
		t.Fatalf("sequential-a first version = %v, want 1", firstA.Version)
	}
	if secondA.Version == nil || *secondA.Version != 2 {
		t.Fatalf("sequential-a second version = %v, want 2", secondA.Version)
	}
	if firstB.Version == nil || *firstB.Version != 1 {
		t.Fatalf("sequential-b first version = %v, want 1", firstB.Version)
	}
	if firstC.Version == nil || *firstC.Version != 1 {
		t.Fatalf("sequential-c first version = %v, want 1", firstC.Version)
	}
	if firstA.IntegrityHash != secondA.IntegrityHash || firstA.IntegrityHash != firstB.IntegrityHash {
		t.Fatalf("same definition hashes differ: %q %q %q", firstA.IntegrityHash, secondA.IntegrityHash, firstB.IntegrityHash)
	}
}

func TestCompilerUsesValidatorVersionWhenSchemaSupportsIt(t *testing.T) {
	db, repository := openCompilerTestRepository(t)
	_, _, _, validatorID := insertReferenceFixtures(t, db)
	if _, err := db.Exec("ALTER TABLE format_validators ADD COLUMN version INTEGER NOT NULL DEFAULT 1"); err != nil {
		t.Fatalf("add validator version column: %v", err)
	}
	compiler, err := NewCompiler(repository)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	definition := validPolicyDefinition()
	definition.Request.CustomValidators = []ValidatorReference{{ID: strconv.FormatInt(validatorID, 10), Version: 2}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snapshotID, err := repository.CreateValidated(ctx, "version-aware", definition)
	if err != nil {
		t.Fatalf("CreateValidated() error = %v", err)
	}
	if err := compiler.Compile(ctx, snapshotID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Compile() version mismatch error = %v, want ErrNotFound", err)
	}

	definition.Request.CustomValidators[0].Version = 1
	matchingID, err := repository.CreateValidated(ctx, "version-aware", definition)
	if err != nil {
		t.Fatalf("CreateValidated() matching version error = %v", err)
	}
	if err := compiler.Compile(ctx, matchingID); err != nil {
		t.Fatalf("Compile() matching validator version error = %v", err)
	}
}

func openCompilerTestRepository(t *testing.T) (*sql.DB, *PostgresRepository) {
	t.Helper()
	dsn := os.Getenv("TSZ_POLICY_TEST_DSN")
	if dsn == "" {
		t.Skip("TSZ_POLICY_TEST_DSN is not set")
	}
	db := openRepositoryTestDatabase(t, dsn)
	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	return db, repository
}

func insertReferenceFixtures(t *testing.T, db *sql.DB) (patternID, allowlistID, blocklistID, validatorID int64) {
	t.Helper()
	statements := []string{
		`CREATE TABLE patterns (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, regex TEXT NOT NULL, category TEXT, deleted_at TIMESTAMPTZ NULL)`,
		`CREATE TABLE allowlist (id BIGSERIAL PRIMARY KEY, value TEXT NOT NULL, deleted_at TIMESTAMPTZ NULL)`,
		`CREATE TABLE blocklist (id BIGSERIAL PRIMARY KEY, value TEXT NOT NULL, deleted_at TIMESTAMPTZ NULL)`,
		`CREATE TABLE format_validators (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, type TEXT NOT NULL, rule TEXT, expected_response TEXT, deleted_at TIMESTAMPTZ NULL)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("create reference table: %v", err)
		}
	}
	if err := db.QueryRow(`INSERT INTO patterns (name, regex, category) VALUES ('fixture-pattern', 'fixture-[0-9]+', 'PII') RETURNING id`).Scan(&patternID); err != nil {
		t.Fatalf("insert pattern fixture: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO allowlist (value) VALUES ('allowed-fixture') RETURNING id`).Scan(&allowlistID); err != nil {
		t.Fatalf("insert allowlist fixture: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO blocklist (value) VALUES ('blocked-fixture') RETURNING id`).Scan(&blocklistID); err != nil {
		t.Fatalf("insert blocklist fixture: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO format_validators (name, type, rule, expected_response) VALUES ('fixture-validator', 'REGEX', '^fixture$', 'YES') RETURNING id`).Scan(&validatorID); err != nil {
		t.Fatalf("insert validator fixture: %v", err)
	}
	return patternID, allowlistID, blocklistID, validatorID
}
