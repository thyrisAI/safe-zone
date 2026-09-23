package agentgatewayresource

import (
	"thyris-sz/internal/controller/capabilities"
	"thyris-sz/internal/controller/nativeadapter"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// +kubebuilder:rbac:groups=agentgateway.dev,resources=agentgatewaypolicies,verbs=get;list;watch;create;update;patch;delete

var _ nativeadapter.Adapter = (*Reconciler)(nil)

func (r *Reconciler) Descriptor() nativeadapter.Descriptor {
	return nativeadapter.Descriptor{
		Capabilities: capabilities.AgentgatewayBufferedContentCapabilities,
		Targets: []nativeadapter.TargetCapability{
			{Group: gatewayv1.GroupName, Kind: "Gateway", SectionName: true},
			{Group: gatewayv1.GroupName, Kind: "HTTPRoute", SectionName: true},
		},
		ResourceKind:     "AgentgatewayPolicy",
		ProgrammedReason: "AgentgatewayExtProcConfigured",
	}
}
