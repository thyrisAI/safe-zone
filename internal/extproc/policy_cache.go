package extproc

import "thyris-sz/internal/extproc/policy"

// PolicyCache provides stream-pinnable policy snapshots to transport adapters.
// It contains no gateway protocol types.
type PolicyCache interface {
	Ready() bool
	Get(policyID string) (policy.CompiledSnapshot, bool)
}
