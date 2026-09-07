// Package nativeadapter defines the control-plane boundary for declarative
// Gateway API attachments. It contains no gateway-specific resource types.
package nativeadapter

import (
	"context"
	"fmt"
	"time"

	security "thyris-sz/api/v1beta1"
	"thyris-sz/internal/controller/capabilities"
	"thyris-sz/internal/extproc/policy"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gateway "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// EffectivePolicy contains validated data-plane attachment settings.
type EffectivePolicy struct {
	ProcessingTimeout      time.Duration
	FailOpen               bool
	NegotiatedCapabilities capabilities.Negotiation
}

// TargetCapability declares supported Gateway API targets and section attachment.
type TargetCapability struct {
	Group, Kind string
	SectionName bool
}

// Descriptor is supplied by trusted controller code, never by policy authors.
type Descriptor struct {
	Capabilities     capabilities.AdapterCapabilities
	Targets          []TargetCapability
	ResourceKind     string
	ProgrammedReason string
}

func (d Descriptor) CheckTarget(ref gateway.LocalPolicyTargetReferenceWithSectionName) error {
	for _, target := range d.Targets {
		if target.Group == string(ref.Group) && target.Kind == string(ref.Kind) && (ref.SectionName == nil || target.SectionName) {
			return nil
		}
	}
	return fmt.Errorf("%w: adapter %s does not support target %s/%s with the requested section scope", capabilities.ErrUnsupportedCapability, d.Capabilities.Name, ref.Group, ref.Kind)
}

// Adapter owns native configuration, including deterministic naming, owner
// references and safe conflict removal. Reconcile must preserve existing
// resources on failure. Remove must delete only resources controlled by owner.
// OwnedResources supplies watch prototypes; the binary registers their schemes
// and narrowly scoped RBAC. Kubernetes garbage collection handles owner deletion.
// RouteIdentity must match the adapter's trusted data-plane identity contract.
type Adapter interface {
	Descriptor() Descriptor
	Reconcile(context.Context, *security.TSZGuardrailPolicy, gateway.LocalPolicyTargetReferenceWithSectionName, EffectivePolicy) (controllerutil.OperationResult, error)
	Remove(context.Context, *security.TSZGuardrailPolicy, gateway.LocalPolicyTargetReferenceWithSectionName) error
	RouteIdentity(gateway.LocalPolicyTargetReferenceWithSectionName, client.Object) policy.RouteIdentity
	OwnedResources() []client.Object
	ManagedResourceCount(context.Context) (int, error)
}

// Registry is immutable after construction. Only explicitly installed adapters
// are selectable; unknown names never fall back to Envoy or audit-only behavior.
type Registry struct {
	adapters map[string]Adapter
	ordered  []Adapter
}

func NewRegistry(adapters ...Adapter) (*Registry, error) {
	r := &Registry{adapters: make(map[string]Adapter)}
	for _, adapter := range adapters {
		if adapter == nil {
			return nil, fmt.Errorf("native adapter is nil")
		}
		d := adapter.Descriptor()
		if err := capabilities.ValidateDeclaration(d.Capabilities); err != nil {
			return nil, err
		}
		if d.ResourceKind == "" || d.ProgrammedReason == "" || len(d.Targets) == 0 {
			return nil, fmt.Errorf("native adapter descriptor is incomplete")
		}
		if !d.Capabilities.NativePolicyAttachment {
			return nil, fmt.Errorf("%w: adapter %s has no declarative attachment support", capabilities.ErrUnsupportedCapability, d.Capabilities.Name)
		}
		if _, exists := r.adapters[d.Capabilities.Name]; exists {
			return nil, fmt.Errorf("duplicate native adapter %q", d.Capabilities.Name)
		}
		r.adapters[d.Capabilities.Name] = adapter
		r.ordered = append(r.ordered, adapter)
	}
	return r, nil
}

func (r *Registry) Get(name string) (Adapter, error) {
	if r != nil {
		if adapter, ok := r.adapters[name]; ok {
			return adapter, nil
		}
	}
	return nil, fmt.Errorf("%w: native adapter %q is not installed", capabilities.ErrUnsupportedCapability, name)
}

func (r *Registry) All() []Adapter {
	if r == nil {
		return nil
	}
	return append([]Adapter(nil), r.ordered...)
}
