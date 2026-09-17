# agentgateway integration

## Status

The current compatibility slice supports buffered, non-streaming OpenAI Chat
Completions, OpenAI Responses API, and Anthropic Messages-compatible payloads
and targets the agentgateway 1.5 policy schema. Its Envoy ExtProc wire
compatibility is covered in-process; a live agentgateway compatibility matrix
is not yet claimed. The three payload-family items in Phase 1 and buffered MCP
JSON-RPC traffic from the first item of Phase 2 in issue #50 are implemented.
A2A, generic JSON, streaming, and automatic TSZ controller reconciliation
remain outside this slice.

## Architecture

agentgateway owns routes, providers, credentials, authentication, retries,
timeouts, and backend selection. TSZ is attached only as an external content
security processor:

```text
Client -> agentgateway -> TSZ ExtProc -> ALLOW / MASK / BLOCK -> LLM backend
Client <- agentgateway <- TSZ ExtProc <- ALLOW / MASK / BLOCK <- LLM backend
```

agentgateway implements `envoy.service.ext_proc.v3.ExternalProcessor`, so this
integration reuses `internal/extproc/envoy.Server` and the gateway-neutral BYG
processor. There is no agentgateway-specific guardrail engine.

Set `TSZ_GATEWAY_ADAPTER=agentgateway` on the TSZ ExtProc deployment. This
trusted startup setting labels safe metadata and audit events correctly; it is
not derived from client traffic. The default remains `envoy-gateway` for
backwards compatibility.

## OpenAI Chat Completions

TSZ inspects supported text in `system`, `developer`, `user`, `assistant`, and
`tool` messages, including text content parts, refusal text, function-call
arguments, and tool results. Unknown JSON fields and non-text multimodal parts
are preserved. For non-streaming responses, supported assistant content and
tool-call arguments under `choices` are inspected before delivery.

The checked-in [AgentgatewayPolicy example](../../examples/bring-your-gateway/agentgateway/openai-chat-completions.yaml)
selects only `/v1/chat/completions` and explicitly configures `Buffered` request
and response bodies. Do not use agentgateway's default `FullDuplexStreamed`
body mode for this profile: TSZ must receive the complete JSON document before
it can guarantee masking or blocking.

## OpenAI Responses API

The [Responses API policy example](../../examples/bring-your-gateway/agentgateway/openai-responses.yaml)
selects only `/v1/responses` and uses the same buffered ExtProc contract.

Request inspection covers top-level `instructions`, string input, message
history with `system`, `developer`, `user`, and `assistant` roles, text content
parts, function-call arguments, and function-call outputs. Image, audio, and
file content remains unchanged rather than being interpreted as text.

Buffered response inspection covers every assistant `output_text` or refusal
item and function-call arguments. When a compatible response also includes the
convenience `output_text` field, mutation keeps that aggregate synchronized
with the guarded output items. Unknown fields, ordering, and untouched JSON
bytes are preserved.

## Anthropic Messages

agentgateway exposes Anthropic-compatible traffic by assigning the `Messages`
route type to `/v1/messages` on an `AgentgatewayBackend`. The checked-in
[Anthropic Messages example](../../examples/bring-your-gateway/agentgateway/anthropic-messages.yaml)
configures that route and attaches buffered ExtProc enforcement only to the
exact Messages endpoint. TSZ recognizes the trusted request path, so it does
not depend on clients sending `anthropic-version` before agentgateway performs
provider processing.

Request inspection covers top-level system prompts, user and assistant text,
tool-use input objects, and tool-result text. Buffered response inspection
covers assistant text and tool-use input objects. Image and document sources,
thinking and redacted-thinking blocks, signatures, unknown fields, ordering,
and untouched JSON bytes are preserved rather than interpreted as text.

The `/v1/messages/count_tokens` route is intentionally not included. It is a
separate agentgateway route type and is outside this issue item.

## MCP JSON-RPC

The [MCP example](../../examples/bring-your-gateway/agentgateway/mcp-json-rpc.yaml)
uses an `AgentgatewayBackend` with a `StreamableHTTP` MCP target, an `HTTPRoute`
for `/mcp`, and conditional buffered ExtProc processing. agentgateway continues
to own MCP server selection, protocol negotiation, sessions, authentication,
and tool authorization. TSZ only applies content guardrails to individual
JSON-RPC 2.0 messages.

TSZ inspects `prompts/get` string arguments and returned text or embedded text
resources. For `tools/call`, it inspects request argument objects and response
text, embedded text resources, and `structuredContent`. JSON-RPC IDs, method and
tool names, annotations, resource links, and binary image, audio, or resource
blob data remain unchanged. Request method state is retained inside the ExtProc
transaction so a response cannot claim a different originating method.

Initialization, discovery, notifications, unrelated methods, and JSON-RPC
errors pass without content mutation. Malformed covered content follows the
configured route failure policy. This profile requires complete
`application/json` request and response documents; MCP SSE, JSON-RPC batches,
stdio transport, and binary inspection are not claimed. Configure clients and
servers for JSON responses when strict response enforcement is required.

## Policy identity and failures

The current deployment uses the existing trusted-route-header resolver. The
route owner must overwrite `X-TSZ-Policy`; a client-supplied value is never an
authoritative policy identifier. Native route bindings and automatic
`AgentgatewayPolicy` generation will replace this manual step in a later
phase.

TSZ's configured request and response failure modes remain authoritative.
Use fail-closed for workloads where uninspected content must not pass. A block
or fail-closed decision uses an ExtProc immediate response, so agentgateway
does not call the selected LLM backend.

## Security and limits

- Keep port `9002` cluster-internal and restrict it to agentgateway workloads.
- Use `kubernetes.io/h2c` for the internal plaintext gRPC Service, or configure
  TSZ mTLS and the corresponding agentgateway backend trust settings.
- Keep the agentgateway frontend buffer limit at least as large as the TSZ
  body limit; reject oversized bodies rather than inspecting only a prefix.
- Dynamic metadata under `io.thyris.tsz` contains identifiers and aggregate
  outcomes only, never prompts, PII values, credentials, or detection text.

## Verification

The agentgateway compatibility tests drive buffered Chat Completions,
Responses, Anthropic Messages, and MCP JSON-RPC request/response bodies through
the same Envoy ExtProc gRPC server used in production. They verify request
masking, immediate request blocking, response masking, fail-open/fail-closed
behavior, safe adapter identity, and PII-safe audit/metadata output.

Run it with:

```sh
go test ./internal/extproc/envoy -run AgentgatewayOpenAIChatCompletionsCompatibility
go test ./internal/extproc/envoy -run AgentgatewayOpenAIResponsesCompatibility
go test ./internal/extproc/envoy -run AgentgatewayAnthropicMessagesCompatibility
go test ./internal/extproc/envoy -run AgentgatewayMCPJSONRPCCompatibility
```

See the [example README](../../examples/bring-your-gateway/agentgateway/README.md)
for the current prerequisites, request, and known limitations.
