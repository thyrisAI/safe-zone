package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPolicyMigrationPostgreSQL(t *testing.T) {
	dsn := os.Getenv("TSZ_POLICY_TEST_DSN")
	if dsn == "" {
		t.Skip("TSZ_POLICY_TEST_DSN is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PostgreSQL ping error = %v", err)
	}

	upSQL := readMigration(t, "migrations/000001_create_policy_snapshots.up.sql")
	downSQL := readMigration(t, "migrations/000001_create_policy_snapshots.down.sql")

	t.Run("empty schema", func(t *testing.T) {
		testMigrationInSchema(t, ctx, db, upSQL, downSQL, false)
	})
	t.Run("existing schema", func(t *testing.T) {
		testMigrationInSchema(t, ctx, db, upSQL, downSQL, true)
	})
}

func testMigrationInSchema(t *testing.T, ctx context.Context, db *sql.DB, upSQL, downSQL string, existing bool) {
	t.Helper()
	schema := fmt.Sprintf("tsz_policy_test_%d", time.Now().UnixNano())
	quotedSchema := quoteIdentifier(schema)
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+quotedSchema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if existing {
		if _, err := db.ExecContext(ctx, "CREATE TABLE patterns (id SERIAL PRIMARY KEY, name TEXT NOT NULL)"); err != nil {
			t.Fatalf("create pre-existing table: %v", err)
		}
	}

	if _, err := db.ExecContext(ctx, upSQL); err != nil {
		t.Fatalf("apply up migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, upSQL); err != nil {
		t.Fatalf("reapply up migration: %v", err)
	}

	assertLifecycleStatuses(t, ctx, db)
	assertActiveSnapshotConstraint(t, ctx, db)
	assertDefinitionDatabaseRoundTrip(t, ctx, db)

	if _, err := db.ExecContext(ctx, downSQL); err != nil {
		t.Fatalf("apply down migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, downSQL); err != nil {
		t.Fatalf("reapply down migration: %v", err)
	}
	var policiesTable *string
	if err := db.QueryRowContext(ctx, "SELECT to_regclass('policies')::text").Scan(&policiesTable); err != nil {
		t.Fatalf("check rollback: %v", err)
	}
	if policiesTable != nil {
		t.Fatalf("policies table remains after rollback: %s", *policiesTable)
	}
	if existing {
		var patternsTable *string
		if err := db.QueryRowContext(ctx, "SELECT to_regclass('patterns')::text").Scan(&patternsTable); err != nil {
			t.Fatalf("check existing table after rollback: %v", err)
		}
		if patternsTable == nil {
			t.Fatal("rollback removed pre-existing table")
		}
	}
}

func assertLifecycleStatuses(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT enumlabel
		FROM pg_enum
		JOIN pg_type ON pg_type.oid = pg_enum.enumtypid
		JOIN pg_namespace ON pg_namespace.oid = pg_type.typnamespace
		WHERE pg_type.typname = 'policy_snapshot_status'
		  AND pg_namespace.nspname = current_schema()
		ORDER BY enumsortorder`)
	if err != nil {
		t.Fatalf("query lifecycle statuses: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			t.Fatalf("scan lifecycle status: %v", err)
		}
		got = append(got, status)
	}
	want := []string{"draft", "validated", "compiled", "staged", "active", "superseded", "rolled_back"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lifecycle statuses = %v, want %v", got, want)
	}
}

func assertActiveSnapshotConstraint(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var policyID int64
	if err := db.QueryRowContext(ctx, "INSERT INTO policies (name) VALUES ('unique-active') RETURNING id").Scan(&policyID); err != nil {
		t.Fatalf("insert policy: %v", err)
	}
	definition := `{"scope":{},"request":{},"response":{},"failure_policy":{},"limits":{},"audit":{},"telemetry":{}}`
	if _, err := db.ExecContext(ctx, "INSERT INTO policy_snapshots (policy_id, version, status, definition) VALUES ($1, 1, 'active', $2::jsonb)", policyID, definition); err != nil {
		t.Fatalf("insert first active snapshot: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO policy_snapshots (policy_id, version, status, definition) VALUES ($1, 2, 'active', $2::jsonb)", policyID, definition); err == nil {
		t.Fatal("second active snapshot was accepted; want partial unique index violation")
	}
}

func assertDefinitionDatabaseRoundTrip(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	definition := PolicyDefinition{
		Scope: Scope{Environment: "test", Gateway: "gateway", Route: "route"},
		Request: RequestPolicy{
			PII:              ActionMask,
			Secret:           ActionBlock,
			PromptInjection:  ActionAuditOnly,
			CustomPatternIDs: []string{"pattern-1"},
			AllowlistIDs:     []string{"allow-1"},
			BlocklistIDs:     []string{"block-1"},
			CustomValidators: []ValidatorReference{{ID: "validator-1", Version: 4}},
		},
		Response:      ResponsePolicy{Enabled: true, PII: ActionMask, UnsafeContent: ActionBlock},
		FailurePolicy: FailurePolicy{Request: FailureModeClosed, Response: FailureModeOpen},
		Limits:        Limits{MaxBodyBytes: 1048576, ProcessingTimeoutMS: 2000, MaxDetections: 50},
		Audit:         AuditSettings{Enabled: true, IncludeCategories: true},
		Telemetry:     TelemetrySettings{Enabled: true, MetricsEnabled: true, SampleRate: 1},
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("marshal definition: %v", err)
	}

	var stored []byte
	if err := db.QueryRowContext(ctx, `
		WITH inserted_policy AS (
			INSERT INTO policies (name) VALUES ('round-trip') RETURNING id
		), inserted_snapshot AS (
			INSERT INTO policy_snapshots (policy_id, status, definition)
			SELECT id, 'validated', $1::jsonb FROM inserted_policy
			RETURNING definition
		)
		SELECT definition FROM inserted_snapshot`, encoded).Scan(&stored); err != nil {
		t.Fatalf("store and load definition: %v", err)
	}
	var got PolicyDefinition
	if err := json.Unmarshal(stored, &got); err != nil {
		t.Fatalf("unmarshal stored definition: %v", err)
	}
	if !reflect.DeepEqual(got, definition) {
		t.Fatalf("database round trip changed definition\ngot:  %#v\nwant: %#v", got, definition)
	}
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	contents, err := MigrationFiles.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(contents)
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
