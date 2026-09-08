package release_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestHelmEnvoyMigrationsMatchRuntimeMigrations(t *testing.T) {
	repoRoot := repositoryRoot(t)
	for _, name := range []string{
		"000001_create_policy_snapshots.up.sql",
		"000002_create_route_policy_bindings.up.sql",
		"000003_create_owner_crd_refs.up.sql",
		"000004_create_guardrail_template_snapshots.up.sql",
	} {
		runtimePath := filepath.Join(repoRoot, "internal", "extproc", "policy", "migrations", name)
		chartPath := filepath.Join(repoRoot, "deployment", "helm", "thyris-sz", "files", "envoy-migrations", name)

		runtimeSQL, err := os.ReadFile(runtimePath)
		if err != nil {
			t.Fatalf("read runtime migration %s: %v", name, err)
		}
		chartSQL, err := os.ReadFile(chartPath)
		if err != nil {
			t.Fatalf("read Helm migration %s: %v", name, err)
		}
		if !bytes.Equal(runtimeSQL, chartSQL) {
			t.Errorf("Helm migration %s drifted from the runtime source", name)
		}
	}
}

func TestHelmDatabaseBootstrapMatchesCanonicalScript(t *testing.T) {
	repoRoot := repositoryRoot(t)
	canonical, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "database", "init.sql"))
	if err != nil {
		t.Fatalf("read canonical database bootstrap: %v", err)
	}
	packaged, err := os.ReadFile(filepath.Join(repoRoot, "deployment", "helm", "thyris-sz", "files", "init.sql"))
	if err != nil {
		t.Fatalf("read Helm database bootstrap: %v", err)
	}
	if !bytes.Equal(canonical, packaged) {
		t.Error("Helm database bootstrap drifted from scripts/database/init.sql")
	}
}

func TestLegacyEnvoyDeploymentDirectoryIsRemoved(t *testing.T) {
	repoRoot := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(repoRoot, "deployments")); !os.IsNotExist(err) {
		t.Fatalf("legacy deployments directory still exists: %v", err)
	}
}

func TestLegacyRootUtilityPathsAreRemoved(t *testing.T) {
	repoRoot := repositoryRoot(t)
	for _, path := range []string{
		"ci", "hack", "init.sql", "Dockerfile", "docker-compose.yml",
		"ROADMAP.md", "SECURITY_ROADMAP.md", "ADOPTERS.md",
	} {
		if _, err := os.Stat(filepath.Join(repoRoot, path)); !os.IsNotExist(err) {
			t.Errorf("legacy root path %q still exists: %v", path, err)
		}
	}
}
