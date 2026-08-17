package v1alpha1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// PolicySource identifies the authoritative source for a policy attachment.
// +kubebuilder:validation:Enum=PostgresRef;Inline
type PolicySource string

const (
	PolicySourcePostgresRef PolicySource = "PostgresRef"
	PolicySourceInline      PolicySource = "Inline"
)

// PolicyAction is the Kubernetes representation of a guardrail action.
// +kubebuilder:validation:Enum=Mask;Block;Allow;AuditOnly
type PolicyAction string

const (
	PolicyActionMask      PolicyAction = "Mask"
	PolicyActionBlock     PolicyAction = "Block"
	PolicyActionAllow     PolicyAction = "Allow"
	PolicyActionAuditOnly PolicyAction = "AuditOnly"
)

// FailureMode describes how processing failures are handled.
// +kubebuilder:validation:Enum=FailOpen;FailClosed
type FailureMode string

const (
	FailureModeOpen   FailureMode = "FailOpen"
	FailureModeClosed FailureMode = "FailClosed"
)

// TSZGuardrailPolicy attaches a TSZ policy to Gateway API resources.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,path=tszguardrailpolicies,shortName=tszgp
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`
// +kubebuilder:printcolumn:name="Programmed",type=string,JSONPath=`.status.conditions[?(@.type=="Programmed")].status`
// +kubebuilder:printcolumn:name="PolicyVersion",type=string,JSONPath=`.status.policyVersion`
type TSZGuardrailPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TSZGuardrailPolicySpec   `json:"spec"`
	Status TSZGuardrailPolicyStatus `json:"status,omitempty"`
}

// TSZGuardrailPolicySpec declares one policy attachment.
// +kubebuilder:validation:XValidation:rule="self.policySource == 'PostgresRef' ? has(self.policyRef) : has(self.request) && has(self.response)",message="policyRef is required for PostgresRef; request and response are required for Inline"
type TSZGuardrailPolicySpec struct {
	// TargetRefs identifies the Gateway API resources to which this attachment applies.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:XValidation:rule="self.all(ref, ref.group == 'gateway.networking.k8s.io' && ref.kind in ['Gateway', 'HTTPRoute', 'GRPCRoute'])",message="targetRefs must use Gateway API Gateway, HTTPRoute, or GRPCRoute targets"
	TargetRefs []gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRefs"`

	// PolicySource selects either an existing immutable Postgres snapshot or an
	// inline policy definition stored by the controller.
	PolicySource PolicySource `json:"policySource"`

	// PolicyRef identifies an immutable policy snapshot when policySource is PostgresRef.
	PolicyRef *PolicyReference `json:"policyRef,omitempty"`
	// TemplateRefs pins reusable guardrail templates to immutable versions.
	// The controller resolves their pattern and validator references during
	// effective-policy compilation.
	TemplateRefs []TemplateReference `json:"templateRefs,omitempty"`
	Request      *RequestPolicySpec  `json:"request,omitempty"`
	Response     *ResponsePolicySpec `json:"response,omitempty"`

	// +optional
	// +kubebuilder:default={request:FailClosed,response:FailClosed}
	FailurePolicy FailurePolicySpec `json:"failurePolicy,omitempty"`
	// +optional
	// +kubebuilder:default="2s"
	ProcessingTimeout *metav1.Duration    `json:"processingTimeout,omitempty"`
	Streaming         *StreamingSpec      `json:"streaming,omitempty"`
	ClientOverrides   ClientOverridesSpec `json:"clientOverrides,omitempty"`
	Telemetry         TelemetrySpec       `json:"telemetry,omitempty"`
}

// PolicyReference identifies one immutable policy version in PostgreSQL.
type PolicyReference struct {
	// Name is the policy name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Version is required by reconciliation to preserve immutable snapshot semantics.
	// +optional
	// +kubebuilder:validation:Minimum=1
	Version *int32 `json:"version,omitempty"`
}

// TemplateReference identifies one immutable reusable guardrail template.
type TemplateReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:Minimum=1
	Version int32 `json:"version"`
}

// RequestPolicySpec is the CRD-native form of policy.RequestPolicy.
type RequestPolicySpec struct {
	PII             PolicyAction `json:"pii"`
	Secret          PolicyAction `json:"secret"`
	PromptInjection PolicyAction `json:"promptInjection"`

	CustomPatternIDs []string             `json:"customPatternIDs,omitempty"`
	AllowlistIDs     []string             `json:"allowlistIDs,omitempty"`
	BlocklistIDs     []string             `json:"blocklistIDs,omitempty"`
	CustomValidators []ValidatorReference `json:"customValidators,omitempty"`
}

// ResponsePolicySpec is the CRD-native form of policy.ResponsePolicy.
type ResponsePolicySpec struct {
	Enabled       bool         `json:"enabled"`
	PII           PolicyAction `json:"pii"`
	Secret        PolicyAction `json:"secret"`
	UnsafeContent PolicyAction `json:"unsafeContent"`

	CustomPatternIDs []string             `json:"customPatternIDs,omitempty"`
	CustomValidators []ValidatorReference `json:"customValidators,omitempty"`
}

// ValidatorReference pins a custom validator to an immutable version.
type ValidatorReference struct {
	// +kubebuilder:validation:MinLength=1
	ID string `json:"id"`
	// +kubebuilder:validation:Minimum=1
	Version int32 `json:"version"`
}

// FailurePolicySpec defines request and response failure behavior.
type FailurePolicySpec struct {
	// +kubebuilder:default=FailClosed
	Request FailureMode `json:"request"`
	// +kubebuilder:default=FailClosed
	Response FailureMode `json:"response"`
}

// ProcessingTimeoutOrDefault returns the ext_proc timeout with the production
// fail-closed default used when API defaulting has not run (for example tests).
func (s TSZGuardrailPolicySpec) ProcessingTimeoutOrDefault() time.Duration {
	if s.ProcessingTimeout == nil || s.ProcessingTimeout.Duration <= 0 {
		return 2 * time.Second
	}
	return s.ProcessingTimeout.Duration
}

// FailOpen reports whether either request or response handling explicitly
// opts into fail-open. Omitted failurePolicy defaults to fail-closed.
func (s TSZGuardrailPolicySpec) FailOpen() bool {
	return s.FailurePolicy.Request == FailureModeOpen || s.FailurePolicy.Response == FailureModeOpen
}

// StreamingSpec configures streaming enforcement for an attachment.
type StreamingSpec struct {
	Enabled bool `json:"enabled"`
	// +kubebuilder:validation:Enum=None;Windowed
	Mode string `json:"mode,omitempty"`
}

// ClientOverridesSpec controls opt-in client behavior for an attachment.
type ClientOverridesSpec struct {
	AllowPolicyHeader bool `json:"allowPolicyHeader,omitempty"`
}

// TelemetrySpec controls policy telemetry emitted by the controller and runtime.
type TelemetrySpec struct {
	Enabled        bool `json:"enabled,omitempty"`
	MetricsEnabled bool `json:"metricsEnabled,omitempty"`
	TracingEnabled bool `json:"tracingEnabled,omitempty"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	SampleRate float64 `json:"sampleRate,omitempty"`
}

// TSZGuardrailPolicyStatus reports policy resolution and programming state.
type TSZGuardrailPolicyStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	PolicyVersion      *int               `json:"policyVersion,omitempty"`
	EffectivePolicyID  string             `json:"effectivePolicyID,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	TargetRefStatuses  []TargetRefStatus  `json:"targetRefStatuses,omitempty"`
}

// TargetRefStatus reports independent resolution and programming state for
// one entry in spec.targetRefs.
type TargetRefStatus struct {
	TargetRef  gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`
	Conditions []metav1.Condition                                        `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type TSZGuardrailPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TSZGuardrailPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TSZGuardrailPolicy{}, &TSZGuardrailPolicyList{})
}
