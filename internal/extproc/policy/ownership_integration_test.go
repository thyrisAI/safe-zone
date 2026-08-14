package policy

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestCRDOwnershipTrackerPostgreSQL(t *testing.T) {
	dsn := os.Getenv("TSZ_POLICY_TEST_DSN")
	if dsn == "" {
		t.Skip("TSZ_POLICY_TEST_DSN is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	schema := fmt.Sprintf("tsz_ownership_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	sqlText, err := MigrationFiles.ReadFile("migrations/000003_create_owner_crd_refs.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(sqlText)); err != nil {
		t.Fatal(err)
	}
	tracker, err := NewCRDOwnershipTracker(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.ClaimOwnership(ctx, "crd/apps/guardrails/abc", nil, "apps", "guardrails"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.ClaimOwnership(ctx, "crd/apps/guardrails/abc", nil, "apps", "guardrails"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM owner_crd_refs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("ownership row count = %d, want 1", count)
	}
	if err := tracker.ReleaseOwnership(ctx, "crd/apps/guardrails/abc", nil, "apps", "guardrails"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM owner_crd_refs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("ownership row count after release = %d, want 0", count)
	}
}
