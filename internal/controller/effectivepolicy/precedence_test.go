package effectivepolicy

import (
	"testing"

	"thyris-sz/internal/controller/policyattach"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestSelectWinnerUsesMostSpecificWholePolicy(t *testing.T) {
	rule := gatewayv1.SectionName("inspect")
	winner, conflict := SelectWinner([]Candidate{
		{ID: "gateway", Target: resolvedTarget("Gateway", nil)},
		{ID: "listener", Target: resolvedTarget("Gateway", &rule)},
		{ID: "route", Target: resolvedTarget("HTTPRoute", nil)},
		{ID: "rule", Target: resolvedTarget("HTTPRoute", &rule)},
	})
	if conflict != nil {
		t.Fatalf("SelectWinner() conflict = %v", conflict)
	}
	if winner.ID != "rule" {
		t.Fatalf("SelectWinner() = %q, want rule", winner.ID)
	}
}

func TestSelectWinnerReportsSameLevelConflictDeterministically(t *testing.T) {
	winner, conflict := SelectWinner([]Candidate{
		{ID: "z-policy", Target: resolvedTarget("HTTPRoute", nil)},
		{ID: "a-policy", Target: resolvedTarget("HTTPRoute", nil)},
		{ID: "gateway", Target: resolvedTarget("Gateway", nil)},
	})
	if winner.ID != "" {
		t.Fatalf("SelectWinner() winner = %q, want empty on conflict", winner.ID)
	}
	if conflict == nil {
		t.Fatal("SelectWinner() conflict = nil, want conflict")
	}
	if conflict.Level != LevelRouteWide {
		t.Fatalf("conflict level = %d, want %d", conflict.Level, LevelRouteWide)
	}
	if got := []string{conflict.Candidates[0].ID, conflict.Candidates[1].ID}; got[0] != "a-policy" || got[1] != "z-policy" {
		t.Fatalf("conflict candidates = %v, want sorted IDs", got)
	}
}

func resolvedTarget(kind string, section *gatewayv1.SectionName) policyattach.ResolvedTarget {
	return policyattach.ResolvedTarget{
		Kind: kind,
		Ref: gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName(gatewayv1.LocalPolicyTargetReferenceWithSectionName{
			SectionName: section,
		}),
	}
}
