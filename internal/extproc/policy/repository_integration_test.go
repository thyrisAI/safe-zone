package policy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresRepositoryCreateValidated(t *testing.T) {
	dsn := os.Getenv("TSZ_POLICY_TEST_DSN")
	if dsn == "" {
		t.Skip("TSZ_POLICY_TEST_DSN is not set")
	}

	db := openRepositoryTestDatabase(t, dsn)
	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	definition := validPolicyDefinition()
	snapshotID, err := repository.CreateValidated(ctx, "default", definition)
	if err != nil {
		t.Fatalf("CreateValidated() error = %v", err)
	}
	snapshot, err := repository.SnapshotByID(ctx, snapshotID)
	if err != nil {
		t.Fatalf("SnapshotByID() error = %v", err)
	}
	if snapshot.Status != StatusValidated {
		t.Fatalf("snapshot status = %q, want %q", snapshot.Status, StatusValidated)
	}
	if snapshot.Version != nil {
		t.Fatalf("validated snapshot version = %v, want nil", *snapshot.Version)
	}
	if snapshot.CompiledAt != nil {
		t.Fatalf("validated snapshot compiled_at = %v, want nil", snapshot.CompiledAt)
	}
	if snapshot.ActivatedAt != nil {
		t.Fatalf("validated snapshot activated_at = %v, want nil", snapshot.ActivatedAt)
	}
	if snapshot.PolicyName != "default" || snapshot.Tenant != nil {
		t.Fatalf("unexpected resolved policy identity: %+v", snapshot)
	}
}

func TestPostgresRepositoryResolvesPolicyIdentityConcurrently(t *testing.T) {
	dsn := os.Getenv("TSZ_POLICY_TEST_DSN")
	if dsn == "" {
		t.Skip("TSZ_POLICY_TEST_DSN is not set")
	}

	db := openRepositoryTestDatabase(t, dsn)
	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const writers = 8
	start := make(chan struct{})
	errorsChannel := make(chan error, writers)
	var waitGroup sync.WaitGroup
	for range writers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			_, err := repository.CreateValidated(ctx, "concurrent", validPolicyDefinition())
			errorsChannel <- err
		}()
	}
	close(start)
	waitGroup.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent CreateValidated() error = %v", err)
		}
	}

	var policyCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM policies WHERE name = 'concurrent' AND tenant IS NULL").Scan(&policyCount); err != nil {
		t.Fatalf("count resolved policies: %v", err)
	}
	if policyCount != 1 {
		t.Fatalf("resolved policy count = %d, want 1", policyCount)
	}
	var snapshotCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM policy_snapshots s
		JOIN policies p ON p.id = s.policy_id
		WHERE p.name = 'concurrent' AND p.tenant IS NULL AND s.status = 'validated'`).Scan(&snapshotCount); err != nil {
		t.Fatalf("count validated snapshots: %v", err)
	}
	if snapshotCount != writers {
		t.Fatalf("validated snapshot count = %d, want %d", snapshotCount, writers)
	}
}

func TestPostgresRepositorySeparatesTenantIdentity(t *testing.T) {
	dsn := os.Getenv("TSZ_POLICY_TEST_DSN")
	if dsn == "" {
		t.Skip("TSZ_POLICY_TEST_DSN is not set")
	}

	db := openRepositoryTestDatabase(t, dsn)
	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tenantA := "tenant-a"
	tenantB := "tenant-b"
	definitionA := validPolicyDefinition()
	definitionA.Scope.Tenant = &tenantA
	definitionB := validPolicyDefinition()
	definitionB.Scope.Tenant = &tenantB
	for _, definition := range []PolicyDefinition{definitionA, definitionA, definitionB} {
		if _, err := repository.CreateValidated(ctx, "shared-name", definition); err != nil {
			t.Fatalf("CreateValidated() error = %v", err)
		}
	}

	var policyCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM policies WHERE name = 'shared-name'").Scan(&policyCount); err != nil {
		t.Fatalf("count tenant policies: %v", err)
	}
	if policyCount != 2 {
		t.Fatalf("tenant-specific policy count = %d, want 2", policyCount)
	}
}

func TestPostgresRepositoryRejectsInvalidActionBeforeWrite(t *testing.T) {
	dsn := os.Getenv("TSZ_POLICY_TEST_DSN")
	if dsn == "" {
		t.Skip("TSZ_POLICY_TEST_DSN is not set")
	}

	db := openRepositoryTestDatabase(t, dsn)
	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	definition := validPolicyDefinition()
	definition.Request.Secret = "REDACT"
	_, err = repository.CreateValidated(context.Background(), "invalid", definition)
	if !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("CreateValidated() error = %v, want ErrInvalidDefinition", err)
	}

	var count int
	if err := db.QueryRow("SELECT count(*) FROM policies WHERE name = 'invalid'").Scan(&count); err != nil {
		t.Fatalf("count invalid policies: %v", err)
	}
	if count != 0 {
		t.Fatalf("invalid definition wrote %d policies, want 0", count)
	}
}

func TestPostgresRepositoryLifecycleQueries(t *testing.T) {
	dsn := os.Getenv("TSZ_POLICY_TEST_DSN")
	if dsn == "" {
		t.Skip("TSZ_POLICY_TEST_DSN is not set")
	}

	db := openRepositoryTestDatabase(t, dsn)
	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	firstID, err := repository.CreateValidated(ctx, "lifecycle", validPolicyDefinition())
	if err != nil {
		t.Fatalf("create first validated snapshot: %v", err)
	}
	secondID, err := repository.CreateValidated(ctx, "lifecycle", validPolicyDefinition())
	if err != nil {
		t.Fatalf("create second validated snapshot: %v", err)
	}

	compile := func(snapshotID int64, hash string) {
		t.Helper()
		if err := repository.WithinTransaction(ctx, func(transaction Transaction) error {
			snapshot, err := transaction.SnapshotForUpdate(ctx, snapshotID)
			if err != nil {
				return err
			}
			if snapshot.Status != StatusValidated {
				return fmt.Errorf("snapshot status = %s, want validated", snapshot.Status)
			}
			version, err := transaction.NextVersion(ctx, snapshot.PolicyID)
			if err != nil {
				return err
			}
			return transaction.MarkCompiled(ctx, snapshotID, snapshot.Definition, version, hash, time.Now().UTC())
		}); err != nil {
			t.Fatalf("compile repository queries: %v", err)
		}
	}

	compile(firstID, "sha256:first")
	if err := repository.WithinTransaction(ctx, func(transaction Transaction) error {
		return transaction.MarkActive(ctx, firstID, time.Now().UTC())
	}); err != nil {
		t.Fatalf("activate first snapshot: %v", err)
	}
	active, err := repository.ActiveSnapshot(ctx, "lifecycle", nil)
	if err != nil {
		t.Fatalf("load active snapshot: %v", err)
	}
	if active.ID != firstID || active.Version == nil || *active.Version != 1 {
		t.Fatalf("first active snapshot = %+v, want id=%d version=1", active, firstID)
	}
	activeSnapshots, err := repository.ActiveSnapshots(ctx)
	if err != nil {
		t.Fatalf("list active snapshots: %v", err)
	}
	if len(activeSnapshots) != 1 || activeSnapshots[0].ID != firstID {
		t.Fatalf("active snapshots = %+v, want first snapshot only", activeSnapshots)
	}

	compile(secondID, "sha256:second")
	if err := repository.WithinTransaction(ctx, func(transaction Transaction) error {
		second, err := transaction.SnapshotForUpdate(ctx, secondID)
		if err != nil {
			return err
		}
		current, err := transaction.ActiveSnapshotForUpdate(ctx, second.PolicyID)
		if err != nil {
			return err
		}
		if err := transaction.MarkSuperseded(ctx, current.ID); err != nil {
			return err
		}
		return transaction.MarkActive(ctx, secondID, time.Now().UTC())
	}); err != nil {
		t.Fatalf("supersede and activate repository queries: %v", err)
	}

	if err := repository.WithinTransaction(ctx, func(transaction Transaction) error {
		policy, err := transaction.PolicyForUpdate(ctx, "lifecycle", nil)
		if err != nil {
			return err
		}
		current, err := transaction.ActiveSnapshotForUpdate(ctx, policy.ID)
		if err != nil {
			return err
		}
		previous, err := transaction.LatestSupersededForUpdate(ctx, policy.ID)
		if err != nil {
			return err
		}
		if current.ID != secondID || previous.ID != firstID {
			return fmt.Errorf("rollback candidates current=%d previous=%d", current.ID, previous.ID)
		}
		if err := transaction.MarkRolledBack(ctx, current.ID); err != nil {
			return err
		}
		return transaction.MarkActive(ctx, previous.ID, time.Now().UTC())
	}); err != nil {
		t.Fatalf("rollback repository queries: %v", err)
	}

	active, err = repository.ActiveSnapshot(ctx, "lifecycle", nil)
	if err != nil {
		t.Fatalf("load rolled-back active snapshot: %v", err)
	}
	if active.ID != firstID {
		t.Fatalf("active snapshot after rollback = %d, want %d", active.ID, firstID)
	}
	rolledBack, err := repository.SnapshotByID(ctx, secondID)
	if err != nil {
		t.Fatalf("load rolled-back snapshot: %v", err)
	}
	if rolledBack.Status != StatusRolledBack {
		t.Fatalf("second snapshot status = %s, want rolled_back", rolledBack.Status)
	}
}

func openRepositoryTestDatabase(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}
	defer adminDB.Close()

	schema := fmt.Sprintf("tsz_repository_test_%d", time.Now().UnixNano())
	if _, err := adminDB.Exec("CREATE SCHEMA " + quoteIdentifier(schema)); err != nil {
		t.Fatalf("create repository test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupDB, err := sql.Open("pgx", dsn)
		if err == nil {
			_, _ = cleanupDB.Exec("DROP SCHEMA IF EXISTS " + quoteIdentifier(schema) + " CASCADE")
			_ = cleanupDB.Close()
		}
	})

	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	query := parsedDSN.Query()
	query.Set("search_path", schema)
	parsedDSN.RawQuery = query.Encode()
	db, err := sql.Open("pgx", parsedDSN.String())
	if err != nil {
		t.Fatalf("open scoped repository database: %v", err)
	}
	db.SetMaxOpenConns(12)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(readMigration(t, "migrations/000001_create_policy_snapshots.up.sql")); err != nil {
		t.Fatalf("apply repository test migration: %v", err)
	}
	return db
}
