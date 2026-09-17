# agentgateway: buffered LLM APIs

These Phase 1 examples attach the existing TSZ External Processor to
agentgateway `HTTPRoute` resources. The current scope is buffered,
non-streaming OpenAI Chat Completions, OpenAI Responses API, and Anthropic
Messages traffic.

Apply [`openai-chat-completions.yaml`](openai-chat-completions.yaml) after the
agentgateway, `openai` `AgentgatewayBackend`, TSZ deployment, and a compiled TSZ
policy are available in the `ai-platform` namespace. Replace `production-ai`
with that compiled policy ID before applying the manifest.

Configure the TSZ deployment with the trusted adapter identity:

```sh
kubectl -n ai-platform set env deployment/tsz-ext-proc \
  TSZ_GATEWAY_ADAPTER=agentgateway
```

Apply [`openai-responses.yaml`](openai-responses.yaml) for
`POST /v1/responses`. It reuses the `tsz-ext-proc` Service created by the Chat
Completions example. If Responses is installed alone, install an equivalent
internal h2c Service on port `9002` first.

Apply [`anthropic-messages.yaml`](anthropic-messages.yaml) for
`POST /v1/messages`. The example includes an `AgentgatewayBackend` whose
`/v1/messages` route is explicitly typed as `Messages`. Create its referenced
`anthropic-secret` first, and install the internal `tsz-ext-proc` Service if
the Chat Completions example was not applied.

The included route uses `RequestHeaderModifier.set` to overwrite
`X-TSZ-Policy`, `X-TSZ-Gateway`, and `X-TSZ-Route` before ExtProc runs. Do not
change this to `add`, and do not accept a client-provided policy identity. The
complete native route-binding flow is a later integration phase.

Both body modes are intentionally `Buffered`. agentgateway defaults to
`FullDuplexStreamed`, while this compatibility profile guarantees enforcement
only after TSZ receives the complete OpenAI JSON body. Configure the
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

Streaming and automatic `AgentgatewayPolicy` reconciliation are not claimed by
these examples. `/v1/messages/count_tokens` is a separate route and remains
outside this compatibility slice.
