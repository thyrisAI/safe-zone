package integration

import (
	"os"
	"os/exec"
	"testing"
)

// TestBYGPolicyStoreReadiness is intentionally environment-gated: it needs
// the pinned Kind cluster, real PostgreSQL and Redis. It verifies the policy
// store/migration and repository/cache integration; it does not deploy ext-proc
// replicas or claim cache convergence coverage.
func TestBYGPolicyStoreReadiness(t *testing.T) {
	if os.Getenv("TSZ_BYG_KIND_E2E") != "1" {
		t.Skip("set TSZ_BYG_KIND_E2E=1 after provisioning the pinned Kind/PostgreSQL/Redis harness")
	}
	command := exec.Command("./deployments/envoy-gateway/kind-bootstrap.sh", "verify-policy-store-readiness")
	command.Dir = "../.."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("BYG policy store readiness harness failed: %v\n%s", err, output)
	}
}
