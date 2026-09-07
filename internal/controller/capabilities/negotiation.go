package capabilities

import (
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	securityv1beta1 "thyris-sz/api/v1beta1"
	"thyris-sz/internal/extproc/policy"
)

// AdapterCapabilities is a trusted declaration compiled into an adapter. It is
// never populated from a TSZGuardrailPolicy or another user-controlled source.
type AdapterCapabilities struct {
	Name                                                                        string
	Version                                                                     string
	RequestHeaders, RequestBufferedBody, RequestBodyMutation, ImmediateResponse bool
	ResponseBufferedBody, ResponseBodyMutation                                  bool
	ResponseStreaming                                                           StreamingCapability
	DynamicMetadata, NativePolicyAttachment                                     bool
}

// Capability is a security-relevant behavior that a policy may require from a
// gateway adapter. Values are stable so errors, status, and tests can compare
// negotiation results without depending on struct field names.
type Capability string

const (
	CapabilityNativePolicyAttachment    Capability = "nativePolicyAttachment"
	CapabilityRequestHeaders            Capability = "requestHeaders"
	CapabilityRequestBufferedBody       Capability = "requestBufferedBody"
	CapabilityRequestBodyMutation       Capability = "requestBodyMutation"
	CapabilityImmediateResponse         Capability = "immediateResponse"
	CapabilityResponseBufferedBody      Capability = "responseBufferedBody"
	CapabilityResponseBodyMutation      Capability = "responseBodyMutation"
	CapabilityResponseStreamingWindowed Capability = "responseStreaming:Windowed"
	CapabilityDynamicMetadata           Capability = "dynamicMetadata"
)

var (
	ErrUnsupportedCapability        = errors.New("unsupported adapter capability")
	ErrInvalidCapabilityDeclaration = errors.New("invalid adapter capability declaration")
)

// Requirements is an ordered, duplicate-free set derived from a policy. Its
// order is deterministic so reconciliation status does not flap.
type Requirements []Capability

// Negotiation records the trusted adapter declaration and the policy
// requirements it satisfied. The result is passed to the native adapter with
// the effective attachment settings, without introducing a network capability
// handshake.
type Negotiation struct {
	AdapterName    string
	AdapterVersion string
	Required       Requirements
}

func (n Negotiation) Clone() Negotiation {
	n.Required = append(Requirements(nil), n.Required...)
	return n
}

// ValidateDeclaration rejects impossible or ambiguous adapter declarations at
// registration time, before any policy can select the adapter.
func ValidateDeclaration(caps AdapterCapabilities) error {
	if strings.TrimSpace(caps.Name) == "" || strings.TrimSpace(caps.Version) == "" {
		return fmt.Errorf("%w: name and version are required", ErrInvalidCapabilityDeclaration)
	}
	if caps.Version != strings.TrimSpace(caps.Version) {
		return fmt.Errorf("%w: adapter %s version must not contain surrounding whitespace", ErrInvalidCapabilityDeclaration, caps.Name)
	}
	if problems := validation.IsDNS1123Label(caps.Name); len(problems) > 0 {
		return fmt.Errorf("%w: adapter name %q must be a DNS label: %s", ErrInvalidCapabilityDeclaration, caps.Name, strings.Join(problems, "; "))
	}
	switch caps.ResponseStreaming {
	case "", StreamingNone, StreamingWindowed:
	default:
		return fmt.Errorf("%w: adapter %s declares unknown response streaming mode %q", ErrInvalidCapabilityDeclaration, caps.Name, caps.ResponseStreaming)
	}
	if caps.RequestBodyMutation && !caps.RequestBufferedBody {
		return fmt.Errorf("%w: adapter %s declares request body mutation without buffered request bodies", ErrInvalidCapabilityDeclaration, caps.Name)
	}
	if caps.ResponseBodyMutation && !caps.ResponseBufferedBody && caps.ResponseStreaming != StreamingWindowed {
		return fmt.Errorf("%w: adapter %s declares response body mutation without buffered or windowed response bodies", ErrInvalidCapabilityDeclaration, caps.Name)
	}
	return nil
}

// Negotiate verifies that every policy requirement is offered by the selected
// adapter. All missing capabilities are returned together so policy status is
// actionable. No requirement is downgraded or silently ignored.
func Negotiate(required Requirements, offered AdapterCapabilities) (Negotiation, error) {
	if err := ValidateDeclaration(offered); err != nil {
		return Negotiation{}, err
	}
	required = uniqueRequirements(required)
	missing := make(Requirements, 0)
	for _, capability := range required {
		if !supports(offered, capability) {
			missing = append(missing, capability)
		}
	}
	if len(missing) > 0 {
		names := make([]string, len(missing))
		for index, capability := range missing {
			names[index] = string(capability)
		}
		return Negotiation{}, fmt.Errorf("%w: adapter %s@%s is missing %s", ErrUnsupportedCapability, offered.Name, offered.Version, strings.Join(names, ", "))
	}
	return Negotiation{AdapterName: offered.Name, AdapterVersion: offered.Version, Required: append(Requirements(nil), required...)}, nil
}

// NegotiateSpec derives visible attachment requirements before target
// resolution or native resource writes.
func NegotiateSpec(spec securityv1beta1.TSZGuardrailPolicySpec, offered AdapterCapabilities) (Negotiation, error) {
	required, err := requirementsForSpec(spec)
	if err != nil {
		return Negotiation{}, err
	}
	return Negotiate(required, offered)
}

// NegotiateDefinition repeats negotiation against the fully resolved immutable
// snapshot. This prevents PostgresRef actions hidden behind policyRef from
// bypassing adapter admission.
func NegotiateDefinition(def policy.PolicyDefinition, offered AdapterCapabilities) (Negotiation, error) {
	spec := securityv1beta1.TSZGuardrailPolicySpec{
		Request:   &securityv1beta1.RequestPolicySpec{PII: apiAction(def.Request.PII), Secret: apiAction(def.Request.Secret), PromptInjection: apiAction(def.Request.PromptInjection)},
		Response:  &securityv1beta1.ResponsePolicySpec{Enabled: def.Response.Enabled, PII: apiAction(def.Response.PII), Secret: apiAction(def.Response.Secret), UnsafeContent: apiAction(def.Response.UnsafeContent)},
		Streaming: &securityv1beta1.StreamingSpec{Enabled: def.Streaming.Mode == policy.StreamingModeWindowed, Mode: def.Streaming.Mode},
	}
	return NegotiateSpec(spec, offered)
}

// CheckCapabilities and CheckDefinition preserve the original admission API
// for callers that need only success or failure.
func CheckCapabilities(spec securityv1beta1.TSZGuardrailPolicySpec, caps AdapterCapabilities) error {
	_, err := NegotiateSpec(spec, caps)
	return err
}

func CheckDefinition(def policy.PolicyDefinition, caps AdapterCapabilities) error {
	_, err := NegotiateDefinition(def, caps)
	return err
}

func requirementsForSpec(spec securityv1beta1.TSZGuardrailPolicySpec) (Requirements, error) {
	required := Requirements{CapabilityNativePolicyAttachment, CapabilityRequestHeaders, CapabilityRequestBufferedBody}
	if spec.Request != nil {
		required = appendActionRequirements(required, true, spec.Request.PII, spec.Request.Secret, spec.Request.PromptInjection)
	}
	if spec.Response != nil && spec.Response.Enabled {
		required = append(required, CapabilityResponseBufferedBody)
		required = appendActionRequirements(required, false, spec.Response.PII, spec.Response.Secret, spec.Response.UnsafeContent)
	}
	if spec.StreamingMode() == string(StreamingWindowed) {
		required = append(required, CapabilityResponseStreamingWindowed)
	}
	return uniqueRequirements(required), nil
}

func appendActionRequirements(required Requirements, request bool, actions ...securityv1beta1.PolicyAction) Requirements {
	if containsAction(securityv1beta1.PolicyActionMask, actions...) {
		if request {
			required = append(required, CapabilityRequestBodyMutation)
		} else {
			required = append(required, CapabilityResponseBodyMutation)
		}
	}
	if containsAction(securityv1beta1.PolicyActionBlock, actions...) {
		required = append(required, CapabilityImmediateResponse)
	}
	return required
}

func containsAction(want securityv1beta1.PolicyAction, actions ...securityv1beta1.PolicyAction) bool {
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}

func uniqueRequirements(required Requirements) Requirements {
	seen := make(map[Capability]struct{}, len(required))
	result := make(Requirements, 0, len(required))
	for _, capability := range required {
		if _, found := seen[capability]; found {
			continue
		}
		seen[capability] = struct{}{}
		result = append(result, capability)
	}
	return result
}

func supports(caps AdapterCapabilities, capability Capability) bool {
	switch capability {
	case CapabilityNativePolicyAttachment:
		return caps.NativePolicyAttachment
	case CapabilityRequestHeaders:
		return caps.RequestHeaders
	case CapabilityRequestBufferedBody:
		return caps.RequestBufferedBody
	case CapabilityRequestBodyMutation:
		return caps.RequestBodyMutation
	case CapabilityImmediateResponse:
		return caps.ImmediateResponse
	case CapabilityResponseBufferedBody:
		return caps.ResponseBufferedBody
	case CapabilityResponseBodyMutation:
		return caps.ResponseBodyMutation
	case CapabilityResponseStreamingWindowed:
		return caps.ResponseStreaming == StreamingWindowed
	case CapabilityDynamicMetadata:
		return caps.DynamicMetadata
	default:
		return false
	}
}

func apiAction(action policy.Action) securityv1beta1.PolicyAction {
	switch action {
	case policy.ActionMask:
		return securityv1beta1.PolicyActionMask
	case policy.ActionBlock:
		return securityv1beta1.PolicyActionBlock
	case policy.ActionAuditOnly:
		return securityv1beta1.PolicyActionAuditOnly
	case policy.ActionAllow:
		return securityv1beta1.PolicyActionAllow
	default:
		return securityv1beta1.PolicyAction(action)
	}
}
