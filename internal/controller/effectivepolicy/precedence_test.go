package effectivepolicy

import (
	"testing"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestSelectWinnerUsesMostSpecificWholePolicy(t *testing.T) {
	rule := gatewayv1.SectionName("inspect")
	winner, conflict := SelectWinner([]Candidate{
		candidate("gateway", "Gateway", nil), candidate("listener", "Gateway", &rule), candidate("route", "HTTPRoute", nil), candidate("rule", "HTTPRoute", &rule),
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
		candidate("z-policy", "HTTPRoute", nil), candidate("a-policy", "HTTPRoute", nil), candidate("gateway", "Gateway", nil),
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

func candidate(id, kind string, section *gatewayv1.SectionName) Candidate {
	return Candidate{ID: id, Kind: kind,
		Ref: gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName(gatewayv1.LocalPolicyTargetReferenceWithSectionName{
			SectionName: section,
		}),
	}
}
