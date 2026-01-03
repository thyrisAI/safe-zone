package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var cliBinPath string

func getCLIPath(t *testing.T) string {
	if cliBinPath != "" {
		return cliBinPath
	}

	// We are in tests/e2e, so root is ../..
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	rootDir := filepath.Join(wd, "..", "..")

	tempDir := t.TempDir()
	exeName := "tsz-cli"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	outputPath := filepath.Join(tempDir, exeName)

	// Build the CLI
	cmd := exec.Command("go", "build", "-o", outputPath, "./pkg/tsz-cli")
	cmd.Dir = rootDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build CLI: %v\nOutput: %s", err, string(out))
	}

	cliBinPath = outputPath
	return cliBinPath
}

func runCLI(t *testing.T, args ...string) string {
	bin := getCLIPath(t)

	// Add global flag --url
	// Note: baseURL() comes from sanity_suite_test.go in the same package
	baseArgs := []string{"--url", baseURL()}

	// Prepend args to allow overriding or appending (though cobra flags are order independent usually)
	// But we want `tsz scan ... --url ...`
	// Actually we are executing `tsz [args...]`.
	// So `tsz scan --text ... --url ...`
	finalArgs := append(args, baseArgs...)

	cmd := exec.Command(bin, finalArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI command failed: %s %v\nError: %v\nOutput: %s", bin, finalArgs, err, string(out))
	}
	return string(out)
}

func TestCLI_Scan(t *testing.T) {
	out := runCLI(t, "scan", "--text", "My email is cli-test@example.com")

	if !strings.Contains(out, "redacted_text") {
		t.Errorf("Expected redacted_text in output, got:\n%s", out)
	}
	if !strings.Contains(out, "[EMAIL]") {
		t.Errorf("Expected [EMAIL] in output, got:\n%s", out)
	}
}

func TestCLI_Patterns_List(t *testing.T) {
	out := runCLI(t, "patterns", "list")

	if !strings.Contains(out, "EMAIL") {
		t.Errorf("Expected EMAIL pattern in list, got:\n%s", out)
	}
}

func TestCLI_Allowlist_Lifecycle(t *testing.T) {
	val := "cli-allow-test"

	// 1. Add
	out := runCLI(t, "allowlist", "add", "--value", val, "--desc", "Created by CLI test", "--key", "test-admin-key")
	if !strings.Contains(out, "created successfully") {
		t.Errorf("Expected success message, got:\n%s", out)
	}

	// 2. List
	out = runCLI(t, "allowlist", "list")
	if !strings.Contains(out, val) {
		t.Errorf("Expected %s in list, got:\n%s", val, out)
	}

	// 3. Remove (requires ID, tricky to parse from text output in simple test)
	// We might skip remove or parse JSON if we add --json flag to list?
	// The CLI output for list is JSON by default in my implementation (json.NewEncoder(os.Stdout).Encode(items)).
	// So we can parse it.
}
