package capabilities

// AgentgatewayBufferedContentCapabilities is the deliberately narrow
// capability profile verified for buffered LLM, MCP, and A2A content payloads.
//
// agentgateway speaks the Envoy External Processing API, so the existing
// transport can inspect and mutate buffered OpenAI Chat Completions, OpenAI
// Responses, Anthropic Messages, MCP JSON-RPC, and A2A JSON-RPC requests and
// responses and can return an immediate response. Native TSZ controller
// reconciliation and streaming are not claimed until their own phases are
// implemented and tested.
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
	NativePolicyAttachment: false,
}

// AgentgatewayBufferedLLMCapabilities is retained for source compatibility.
// Deprecated: use AgentgatewayBufferedContentCapabilities, which also reflects
// the verified MCP JSON-RPC capability.
var AgentgatewayBufferedLLMCapabilities = AgentgatewayBufferedContentCapabilities

// AgentgatewayOpenAIBufferedCapabilities is retained for source compatibility.
// Deprecated: use AgentgatewayBufferedContentCapabilities.
var AgentgatewayOpenAIBufferedCapabilities = AgentgatewayBufferedContentCapabilities
