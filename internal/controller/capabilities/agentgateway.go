package capabilities

// AgentgatewayBufferedContentCapabilities is the deliberately narrow
// capability profile verified for buffered LLM, MCP, A2A, and generic JSON
// content payloads.
//
// agentgateway speaks the Envoy External Processing API, so the existing
// transport can inspect and mutate buffered OpenAI Chat Completions, OpenAI
// Responses, Anthropic Messages, MCP JSON-RPC, A2A JSON-RPC, and explicitly
// selected generic JSON requests and responses and can return an immediate
// response. The TSZ controller also reconciles buffered AgentgatewayPolicy
// attachments. Streaming remains unclaimed until it is implemented and tested.
var AgentgatewayBufferedContentCapabilities = AdapterCapabilities{
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
	NativePolicyAttachment: true,
}

// AgentgatewayBufferedLLMCapabilities is retained for source compatibility.
// Deprecated: use AgentgatewayBufferedContentCapabilities, which also reflects
// the verified MCP JSON-RPC capability.
var AgentgatewayBufferedLLMCapabilities = AgentgatewayBufferedContentCapabilities

// AgentgatewayOpenAIBufferedCapabilities is retained for source compatibility.
// Deprecated: use AgentgatewayBufferedContentCapabilities.
var AgentgatewayOpenAIBufferedCapabilities = AgentgatewayBufferedContentCapabilities
