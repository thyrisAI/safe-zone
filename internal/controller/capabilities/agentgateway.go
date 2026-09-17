package capabilities

// AgentgatewayOpenAIBufferedCapabilities is the deliberately narrow
// capability profile verified for the buffered OpenAI Phase 1 slices.
//
// agentgateway speaks the Envoy External Processing API, so the existing
// transport can inspect and mutate buffered Chat Completions and Responses API
// requests and responses and can return an immediate response. Native TSZ
// controller reconciliation and streaming are not claimed until their own
// phases are implemented and tested.
var AgentgatewayOpenAIBufferedCapabilities = AdapterCapabilities{
	Name:                   "agentgateway",
	Version:                "1.0.0",
	RequestHeaders:         true,
	RequestBufferedBody:    true,
	RequestBodyMutation:    true,
	ImmediateResponse:      true,
	ResponseBufferedBody:   true,
	ResponseBodyMutation:   true,
	ResponseStreaming:      StreamingNone,
	DynamicMetadata:        true,
	NativePolicyAttachment: false,
}
