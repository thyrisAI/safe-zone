package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionAccepted     = "Accepted"
	ConditionResolvedRefs = "ResolvedRefs"
	ConditionProgrammed   = "Programmed"
	ConditionPolicySynced = "PolicySynced"

	ReasonValid                 = "Valid"
	ReasonResolved              = "Resolved"
	ReasonExtProcConfigured     = "ExtProcConfigured"
	ReasonSnapshotActive        = "SnapshotActive"
	ReasonConflicted            = "Conflicted"
	ReasonInvalidTarget         = "InvalidTarget"
	ReasonPolicyNotFound        = "PolicyNotFound"
	ReasonUnsupportedCapability = "UnsupportedCapability"
	ReasonSnapshotRejected      = "SnapshotRejected"
	ReasonProcessorUnavailable  = "ProcessorUnavailable"
	ReasonVersionIncompatible   = "VersionIncompatible"
)

// SetStatusCondition updates a condition using Kubernetes' standard merge and
// transition-time semantics rather than manually editing a condition slice.
func SetStatusCondition(conditions *[]metav1.Condition, condition metav1.Condition) {
	meta.SetStatusCondition(conditions, condition)
}
