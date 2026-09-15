# agentgateway: OpenAI Chat Completions

This Phase 1 example attaches the existing TSZ External Processor to an
agentgateway `HTTPRoute`. It covers buffered, non-streaming
`POST /v1/chat/completions` traffic only.

Apply [`openai-chat-completions.yaml`](openai-chat-completions.yaml) after the
agentgateway, `openai` `AgentgatewayBackend`, TSZ deployment, and a compiled TSZ
policy are available in the `ai-platform` namespace. Replace `production-ai`
with that compiled policy ID before applying the manifest.

The included route uses `RequestHeaderModifier.set` to overwrite
`X-TSZ-Policy`, `X-TSZ-Gateway`, and `X-TSZ-Route` before ExtProc runs. Do not
change this to `add`, and do not accept a client-provided policy identity. The
complete native route-binding flow is a later integration phase.

Both body modes are intentionally `Buffered`. agentgateway defaults to
`FullDuplexStreamed`, while this compatibility profile guarantees enforcement
only after TSZ receives the complete Chat Completions JSON body. Configure the
agentgateway frontend buffer limit and `TSZ_EXTPROC_MAX_BODY_BYTES` consistently.

Send a request through the route:

```sh
curl --fail-with-body \
  --header 'content-type: application/json' \
  --data '{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"Email me at person@example.com"}]}' \
  http://AGENTGATEWAY_ADDRESS/v1/chat/completions
```

With a masking policy, the backend must receive sanitized message content. A
blocking policy must return a TSZ immediate response without contacting the
backend. When response enforcement is enabled, TSZ inspects and may mask or
block `choices[*].message` text and function-call arguments before they reach
the client.

Streaming, automatic `AgentgatewayPolicy` reconciliation, and the remaining
Phase 1 payload families are not claimed by this example.
