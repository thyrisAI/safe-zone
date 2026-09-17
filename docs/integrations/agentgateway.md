# agentgateway integration

## Status

The current compatibility slice supports buffered, non-streaming OpenAI Chat
Completions and Responses API-compatible payloads and targets the agentgateway
1.5 policy schema. Its Envoy ExtProc wire compatibility is covered in-process;
a live agentgateway compatibility matrix is not yet claimed. These are the
first two items of Phase 1 in issue #50; Anthropic Messages, streaming, and
automatic TSZ controller reconciliation remain outside this slice.

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

The agentgateway compatibility tests drive buffered Chat Completions and
Responses request/response bodies through the same Envoy ExtProc gRPC server
used in production. They verify request masking, immediate request blocking,
response masking, fail-open/fail-closed behavior, safe adapter identity, and
PII-safe audit/metadata output.

Run it with:

```sh
go test ./internal/extproc/envoy -run AgentgatewayOpenAIChatCompletionsCompatibility
go test ./internal/extproc/envoy -run AgentgatewayOpenAIResponsesCompatibility
```

See the [example README](../../examples/bring-your-gateway/agentgateway/README.md)
for the current prerequisites, request, and known limitations.
