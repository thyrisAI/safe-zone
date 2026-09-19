# agentgateway: buffered content APIs

These Phase 1 and Phase 2 examples attach the existing TSZ External Processor
to agentgateway `HTTPRoute` resources. The current scope is buffered,
non-streaming OpenAI Chat Completions, OpenAI Responses API, and Anthropic
Messages traffic, plus buffered MCP JSON-RPC messages over Streamable HTTP and
buffered A2A JSON-RPC traffic through an A2A backend.
Generic JSON HTTP request and response bodies are supported through an explicit
route-owned content-adapter selector.

Apply [`openai-chat-completions.yaml`](openai-chat-completions.yaml) after the
agentgateway, `openai` `AgentgatewayBackend`, TSZ deployment, and a compiled TSZ
policy are available in the `ai-platform` namespace. Replace `production-ai`
with that compiled policy ID before applying the manifest.

Configure the TSZ deployment with the trusted adapter identity:

```sh
kubectl -n ai-platform set env deployment/tsz-ext-proc \
  TSZ_GATEWAY_ADAPTER=agentgateway
```

Before applying any route example, edit and apply
[`trusted-routing-context.yaml`](trusted-routing-context.yaml). Replace
`production-ai` with the compiled policy ID and adjust its path mappings when
you rename a route. This Gateway-level `PreRouting` transformation overwrites
the TSZ policy, gateway, route, and content-adapter headers before ExtProc can
observe them. A post-routing `HTTPRoute` header filter is too late because
agentgateway executes `traffic.extProc` before post-routing header mutation.

Apply [`openai-responses.yaml`](openai-responses.yaml) for
`POST /v1/responses`. It reuses the `tsz-ext-proc` Service created by the Chat
Completions example. If Responses is installed alone, install an equivalent
internal h2c Service on port `9002` first.

Apply [`anthropic-messages.yaml`](anthropic-messages.yaml) for
`POST /v1/messages`. The example includes an `AgentgatewayBackend` whose
`/v1/messages` route is explicitly typed as `Messages`. Create its referenced
`anthropic-secret` first, and install the internal `tsz-ext-proc` Service if
the Chat Completions example was not applied.

Apply [`mcp-json-rpc.yaml`](mcp-json-rpc.yaml) for an MCP endpoint under
`/mcp`. The example defines a `StreamableHTTP` `AgentgatewayBackend` whose
upstream `mcp-server` Service listens on port `80` at `/mcp`. With the route
prefix and target path shown, agentgateway clients connect at `/mcp/mcp`.

Apply [`a2a-json-rpc.yaml`](a2a-json-rpc.yaml) for an A2A agent exposed below
`/agents/support`. The example defines an A2A `AgentgatewayBackend`, rewrites
the public prefix to `/`, and applies ExtProc only to `POST` requests so the
Agent Card remains available over `GET`.

Apply [`generic-json-http.yaml`](generic-json-http.yaml) for ordinary JSON APIs
below `/api/customer-profiles`. The example routes to a Kubernetes Service and
uses the matching entry in `trusted-routing-context.yaml` to overwrite
`X-TSZ-Content-Adapter: generic-json`. Replace the backend Service and path and
update that trusted mapping together.

The `PreRouting` policy uses `set` to overwrite `X-TSZ-Policy`,
`X-TSZ-Gateway`, and `X-TSZ-Route`; it also removes a client-provided generic
adapter selector on protocol-specific routes. Do not accept either header as
client authority. The complete native route-binding flow is a later
integration phase.

Both body modes are intentionally `Buffered`. agentgateway defaults to
`FullDuplexStreamed`, while this compatibility profile guarantees enforcement
only after TSZ receives the complete JSON body. Configure the
agentgateway frontend buffer limit and `TSZ_MAX_BODY_BYTES` consistently.

Send a request through the route:

```sh
curl --fail-with-body \
  --header 'content-type: application/json' \
  --data '{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"Email me at person@example.com"}]}' \
  http://AGENTGATEWAY_ADDRESS/v1/chat/completions
```

For the Responses API:

```sh
curl --fail-with-body \
  --header 'content-type: application/json' \
  --data '{"model":"gpt-4.1-mini","instructions":"Protect sensitive data","input":"Email person@example.com"}' \
  http://AGENTGATEWAY_ADDRESS/v1/responses
```

For Anthropic Messages:

```sh
curl --fail-with-body \
  --header 'content-type: application/json' \
  --header 'anthropic-version: 2023-06-01' \
  --data '{"model":"claude-sonnet-4-5","max_tokens":128,"messages":[{"role":"user","content":"Email person@example.com"}]}' \
  http://AGENTGATEWAY_ADDRESS/v1/messages
```

For an MCP tool call, complete the target server's initialization flow first
and include its session header when required:

```sh
curl --fail-with-body \
  --header 'content-type: application/json' \
  --header 'accept: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lookup","arguments":{"email":"person@example.com"}}}' \
  http://AGENTGATEWAY_ADDRESS/mcp/mcp
```

For an A2A message using the method exposed in the current agentgateway
documentation:

```sh
curl --fail-with-body \
  --header 'content-type: application/json' \
  --data '{"jsonrpc":"2.0","id":"1","method":"tasks/send","params":{"id":"task-1","message":{"role":"user","parts":[{"type":"text","text":"Email person@example.com"}]}}}' \
  http://AGENTGATEWAY_ADDRESS/agents/support
```

For a generic JSON API:

```sh
curl --fail-with-body \
  --header 'content-type: application/json' \
  --data '{"name":"Example","email":"person@example.com","active":true}' \
  http://AGENTGATEWAY_ADDRESS/api/customer-profiles
```

With a masking policy, the backend must receive sanitized message content. A
blocking policy must return a TSZ immediate response without contacting the
backend. When response enforcement is enabled, TSZ inspects and may mask or
block `choices[*].message` text and function-call arguments before they reach
the client.

For Responses payloads, TSZ inspects `instructions`, message input, function
arguments and results, assistant output/refusals, and the visible
`output_text` aggregate while preserving unrelated JSON and non-text parts.

For Anthropic payloads, TSZ inspects top-level system content, user and
assistant text, tool-use inputs, and tool-result text on requests, plus
assistant text and tool-use inputs on responses. Image/document sources and
thinking blocks are preserved unchanged.

For MCP JSON-RPC, TSZ inspects `prompts/get` and `tools/call` argument and
result content while preserving protocol routing fields and binary content.
agentgateway remains responsible for MCP authentication, authorization,
sessions, server routing, and tool selection.

For A2A JSON-RPC, TSZ inspects user message text and structured data, plus
agent messages, task status/history, and artifact content in responses. It
preserves task and message identity, state, metadata, and file bytes or URIs.
agentgateway remains responsible for authentication, Agent Card discovery,
routing, and task lifecycle.

For generic JSON, TSZ evaluates the complete document as structured content so
schema and semantic validators retain full context. Objects, arrays, and scalar
values are accepted for `application/json` and `application/*+json`. A masked
document must remain valid JSON with the same top-level kind.

Streaming and automatic `AgentgatewayPolicy` reconciliation are not claimed by
these examples. MCP SSE, batches, and stdio transport are outside this buffered
JSON profile. A2A streaming/SSE, REST, gRPC, push-notification webhooks, and file
inspection are also outside it. `/v1/messages/count_tokens` is a separate route
and remains outside this compatibility slice. Multipart, form, binary, NDJSON,
JSON Text Sequences, and streaming JSON are not handled by the generic adapter.
