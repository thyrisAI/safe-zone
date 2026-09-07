package release_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const manifestPath = "examples/bring-your-gateway/adapter-releases.json"

var requiredExampleScenarios = []string{
	"safe_request",
	"request_masking",
	"request_blocking",
	"response_masking",
	"response_blocking",
	"fail_open",
	"fail_closed",
	"telemetry",
}

type releaseManifest struct {
	SchemaVersion int              `json:"schema_version"`
	Adapters      []adapterRelease `json:"adapters"`
}

type adapterRelease struct {
	Name             string            `json:"name"`
	Maturity         string            `json:"maturity"`
	SourceDir        string            `json:"source_dir"`
	IntegrationGuide string            `json:"integration_guide"`
	ExampleGuide     string            `json:"example_guide"`
	SmokeTest        string            `json:"smoke_test"`
	Examples         map[string]string `json:"examples"`
}

func TestEveryGatewayAdapterHasReleaseDocumentationAndExamples(t *testing.T) {
	repoRoot := repositoryRoot(t)
	manifest := readManifest(t, repoRoot)
	if manifest.SchemaVersion != 1 {
		t.Fatalf("%s schema_version = %d, want 1", manifestPath, manifest.SchemaVersion)
	}

	registered := make(map[string]string, len(manifest.Adapters))
	adapterNames := make(map[string]struct{}, len(manifest.Adapters))
	for _, adapter := range manifest.Adapters {
		validateAdapterRelease(t, repoRoot, adapter, adapterNames)
		sourceDir := filepath.Clean(adapter.SourceDir)
		if previous, exists := registered[sourceDir]; exists {
			t.Errorf("adapter releases %q and %q use the same source_dir %q", previous, adapter.Name, sourceDir)
		}
		registered[sourceDir] = adapter.Name
	}

	implemented := discoverAdapterSources(t, repoRoot)
	for sourceDir := range implemented {
		if _, ok := registered[sourceDir]; !ok {
			t.Errorf("gateway adapter %q has adapter.go but no entry in %s", sourceDir, manifestPath)
		}
	}
	for sourceDir, name := range registered {
		if _, ok := implemented[sourceDir]; !ok {
			t.Errorf("adapter release %q points to %q, which has no adapter.go", name, sourceDir)
		}
	}
	requireAllNumberedExamplesDocumented(t, repoRoot)
}

func validateAdapterRelease(t *testing.T, repoRoot string, adapter adapterRelease, names map[string]struct{}) {
	t.Helper()
	if adapter.Name == "" {
		t.Error("adapter release name must not be empty")
		return
	}
	if _, exists := names[adapter.Name]; exists {
		t.Errorf("duplicate adapter release name %q", adapter.Name)
	}
	names[adapter.Name] = struct{}{}

	switch adapter.Maturity {
	case "experimental", "preview", "stable":
	default:
		t.Errorf("adapter %q maturity = %q, want experimental, preview, or stable", adapter.Name, adapter.Maturity)
	}

	cleanRepositoryPath(t, adapter.SourceDir)
	requireRegularFile(t, repoRoot, adapter.IntegrationGuide, adapter.Name+" integration guide")
	requireTextContains(t, repoRoot, adapter.IntegrationGuide, []string{
		"support status", "version", "install", "security", "failure", "metric", "tracing",
		"upgrade", "rollback", "troubleshoot", "cleanup", "runnable",
	})
	requireRegularFile(t, repoRoot, adapter.ExampleGuide, adapter.Name+" example guide")
	requireTextContains(t, repoRoot, adapter.ExampleGuide, []string{
		"architecture", "prerequisite", "tested version", "safe request", "request masking",
		"request blocking", "response masking", "response blocking", "fail open", "fail closed",
		"upstream", "logs", "metrics", "traces", "troubleshooting", "cleanup", "production",
	})
	requireExecutableFile(t, repoRoot, adapter.SmokeTest, adapter.Name+" smoke test")
	requireDocumentIndexed(t, repoRoot, adapter.IntegrationGuide)
	requireDocumentIndexed(t, repoRoot, adapter.ExampleGuide)

	for _, scenario := range requiredExampleScenarios {
		directory, ok := adapter.Examples[scenario]
		if !ok || directory == "" {
			t.Errorf("adapter %q has no %q runnable example", adapter.Name, scenario)
			continue
		}
		requireExampleDirectory(t, repoRoot, adapter.Name, scenario, directory)
	}
	for scenario := range adapter.Examples {
		if !contains(requiredExampleScenarios, scenario) {
			t.Errorf("adapter %q declares unknown example scenario %q", adapter.Name, scenario)
		}
	}
}

func discoverAdapterSources(t *testing.T, repoRoot string) map[string]struct{} {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repoRoot, "internal", "extproc", "*", "adapter.go"))
	if err != nil {
		t.Fatal(err)
	}
	discovered := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		relative, err := filepath.Rel(repoRoot, filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		discovered[relative] = struct{}{}
	}
	return discovered
}

func requireAllNumberedExamplesDocumented(t *testing.T, repoRoot string) {
	t.Helper()
	directories, err := filepath.Glob(filepath.Join(repoRoot, "examples", "bring-your-gateway", "[0-9][0-9]-*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range directories {
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() {
			continue
		}
		_, policyErr := os.Stat(filepath.Join(directory, "policy.json"))
		_, runnerErr := os.Stat(filepath.Join(directory, "run.sh"))
		if os.IsNotExist(policyErr) && os.IsNotExist(runnerErr) {
			continue
		}
		relative, err := filepath.Rel(repoRoot, filepath.Join(directory, "README.md"))
		if err != nil {
			t.Fatal(err)
		}
		requireRegularFile(t, repoRoot, relative, "runnable BYG example documentation")
	}
}

func requireExampleDirectory(t *testing.T, repoRoot, adapter, scenario, directory string) {
	t.Helper()
	clean := cleanRepositoryPath(t, directory)
	info, err := os.Stat(filepath.Join(repoRoot, clean))
	if err != nil || !info.IsDir() {
		t.Errorf("adapter %q %s example directory %q is missing", adapter, scenario, directory)
		return
	}
	for _, name := range []string{"README.md", "policy.json", "request.json", "expected-status"} {
		requireRegularFile(t, repoRoot, filepath.Join(clean, name), fmt.Sprintf("adapter %q %s example", adapter, scenario))
	}
	requireTextContains(t, repoRoot, filepath.Join(clean, "README.md"), []string{
		"prerequisite", "expected", "upstream", "troubleshoot", "cleanup",
	})
}

func requireRegularFile(t *testing.T, repoRoot, path, purpose string) {
	t.Helper()
	clean := cleanRepositoryPath(t, path)
	info, err := os.Stat(filepath.Join(repoRoot, clean))
	if err != nil || !info.Mode().IsRegular() {
		t.Errorf("%s requires regular file %q", purpose, path)
		return
	}
	if info.Size() == 0 {
		t.Errorf("%s file %q must not be empty", purpose, path)
	}
}

func requireExecutableFile(t *testing.T, repoRoot, path, purpose string) {
	t.Helper()
	requireRegularFile(t, repoRoot, path, purpose)
	info, err := os.Stat(filepath.Join(repoRoot, cleanRepositoryPath(t, path)))
	if err == nil && info.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s file %q must be executable", purpose, path)
	}
}

func requireTextContains(t *testing.T, repoRoot, path string, terms []string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(repoRoot, cleanRepositoryPath(t, path)))
	if err != nil {
		t.Errorf("read documentation %q: %v", path, err)
		return
	}
	lower := strings.ToLower(string(contents))
	for _, term := range terms {
		if !strings.Contains(lower, term) {
			t.Errorf("documentation %q must cover %q", path, term)
		}
	}
}

func requireDocumentIndexed(t *testing.T, repoRoot, guide string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(repoRoot, "docs", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	relative := strings.TrimPrefix(filepath.ToSlash(guide), "docs/")
	if !strings.Contains(string(contents), relative) {
		t.Errorf("integration guide %q is not linked from docs/README.md", guide)
	}
}

func cleanRepositoryPath(t *testing.T, path string) string {
	t.Helper()
	clean := filepath.Clean(path)
	if path == "" || filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		t.Fatalf("release manifest path %q must stay inside the repository", path)
	}
	return clean
}

func readManifest(t *testing.T, repoRoot string) releaseManifest {
	t.Helper()
	file, err := os.Open(filepath.Join(repoRoot, manifestPath))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest releaseManifest
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode %s: %v", manifestPath, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("decode %s: unexpected trailing JSON", manifestPath)
	}
	return manifest
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate adapter release test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
