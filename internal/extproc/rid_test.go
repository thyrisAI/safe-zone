package extproc

import (
	"regexp"
	"testing"
)

func TestNewBYGRIDIsIndependentAndUnique(t *testing.T) {
	format := regexp.MustCompile(`^RID-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := make(map[string]struct{}, 128)
	for range 128 {
		rid := NewBYGRID()
		if !format.MatchString(rid) {
			t.Fatalf("BYG RID %q does not use the independent RID-UUID format", rid)
		}
		if _, exists := seen[rid]; exists {
			t.Fatalf("duplicate BYG RID %q", rid)
		}
		seen[rid] = struct{}{}
	}
}
