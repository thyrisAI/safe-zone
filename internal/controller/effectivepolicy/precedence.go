// Package effectivepolicy implements deterministic whole-policy attachment
// selection for one effective Gateway API target.
package effectivepolicy

import (
	"fmt"
	"sort"

	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// AttachmentLevel orders policy attachment specificity.
type AttachmentLevel int

const (
	LevelGatewayWide AttachmentLevel = iota
	LevelListener
	LevelRouteWide
	LevelRule
)

// Candidate is one valid policy attachment considered for the same effective
// request target. ID must uniquely identify the policy for conflict reporting.
type Candidate struct {
	ID   string
	Kind string
	Ref  gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName
}

// Selector adapts SelectWinner for dependency injection into the attachment
// reconciler without coupling it to a particular persistence implementation.
type Selector struct{}

func (Selector) Select(candidates []Candidate) (Candidate, *ConflictError) {
	return SelectWinner(candidates)
}

// ConflictError reports multiple policies at the same most-specific level.
type ConflictError struct {
	Level      AttachmentLevel
	Candidates []Candidate
}

// DetectConflicts finds every effective target and specificity level that has
// more than one claimant; no claimant is automatically selected.
func DetectConflicts(candidates []Candidate) map[string][]ConflictError {
	groups := map[string]map[AttachmentLevel][]Candidate{}
	for _, candidate := range candidates {
		section := ""
		if candidate.Ref.SectionName != nil {
			section = string(*candidate.Ref.SectionName)
		}
		key := fmt.Sprintf("%s/%s/%s/%s", candidate.Ref.Group, candidate.Ref.Kind, candidate.Ref.Name, section)
		if groups[key] == nil {
			groups[key] = map[AttachmentLevel][]Candidate{}
		}
		level := LevelOf(candidate)
		groups[key][level] = append(groups[key][level], candidate)
	}
	conflicts := map[string][]ConflictError{}
	for target, levels := range groups {
		for level, members := range levels {
			if len(members) > 1 {
				sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
				conflicts[target] = append(conflicts[target], ConflictError{Level: level, Candidates: members})
			}
		}
	}
	return conflicts
}

func (e *ConflictError) Error() string {
	ids := make([]string, 0, len(e.Candidates))
	for _, candidate := range e.Candidates {
		ids = append(ids, candidate.ID)
	}
	return fmt.Sprintf("conflicting policies at attachment level %d: %v", e.Level, ids)
}

// LevelOf returns the specificity of a resolved target. An absent section name
// is resource-wide; a section name is listener/rule-level. Section validity is
// intentionally handled by target resolution/status, not silently downgraded.
func LevelOf(target Candidate) AttachmentLevel {
	switch target.Kind {
	case "HTTPRoute", "GRPCRoute":
		if target.Ref.SectionName != nil {
			return LevelRule
		}
		return LevelRouteWide
	case "Gateway":
		if target.Ref.SectionName != nil {
			return LevelListener
		}
	}
	return LevelGatewayWide
}

// SelectWinner selects the single most-specific whole policy. It never merges
// fields from policies at different levels. A tie at the selected level is a
// deterministic conflict for the status/conflict reconciliation phase.
func SelectWinner(candidates []Candidate) (Candidate, *ConflictError) {
	if len(candidates) == 0 {
		return Candidate{}, nil
	}

	winningLevel := LevelGatewayWide
	for index, candidate := range candidates {
		if index == 0 || LevelOf(candidate) > winningLevel {
			winningLevel = LevelOf(candidate)
		}
	}

	winners := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if LevelOf(candidate) == winningLevel {
			winners = append(winners, candidate)
		}
	}
	sort.Slice(winners, func(i, j int) bool { return winners[i].ID < winners[j].ID })
	if len(winners) > 1 {
		return Candidate{}, &ConflictError{Level: winningLevel, Candidates: winners}
	}
	return winners[0], nil
}
