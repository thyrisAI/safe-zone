package capabilities

type StreamingCapability string

const (
	StreamingNone     StreamingCapability = "None"
	StreamingWindowed StreamingCapability = "Windowed"
)

var EnvoyGatewayCapabilities = AdapterCapabilities{Name: "envoy-gateway", Version: "1.8.3", RequestHeaders: true, RequestBufferedBody: true, RequestBodyMutation: true, ImmediateResponse: true, ResponseBufferedBody: true, ResponseBodyMutation: true, ResponseStreaming: StreamingWindowed, DynamicMetadata: true, NativePolicyAttachment: true}
