package capabilities

// AgentgatewayBufferedLLMCapabilities is the deliberately narrow capability
// profile verified for the buffered LLM API payloads in Phase 1.
//
// agentgateway speaks the Envoy External Processing API, so the existing
// transport can inspect and mutate buffered OpenAI Chat Completions, OpenAI
// Responses, and Anthropic Messages requests and responses and can return an
// immediate response. Native TSZ controller reconciliation and streaming are
// not claimed until their own phases are implemented and tested.
var AgentgatewayBufferedLLMCapabilities = AdapterCapabilities{
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

// AgentgatewayOpenAIBufferedCapabilities is retained for source compatibility.
// Deprecated: use AgentgatewayBufferedLLMCapabilities, which reflects all
// verified Phase 1 payload families.
var AgentgatewayOpenAIBufferedCapabilities = AgentgatewayBufferedLLMCapabilities
