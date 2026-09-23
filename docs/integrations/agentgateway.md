# agentgateway integration

## Status

The current compatibility slice supports buffered, non-streaming OpenAI Chat
Completions, OpenAI Responses API, and Anthropic Messages-compatible payloads
and targets the agentgateway 1.5 policy schema. Its Envoy ExtProc wire
compatibility is covered in-process. CI also runs a real agentgateway 1.5.0
standalone proxy against TSZ's ExtProc server for buffered OpenAI Chat
Completions and generic JSON request and response masking, plus immediate
request blocking. The controller reconciliation path is covered against the
agentgateway 1.5 policy schema; a live Kubernetes compatibility matrix is not
yet claimed. The three
payload-family items in Phase 1 and the buffered
MCP, A2A, and generic JSON HTTP items from Phase 2 in issue #50 are implemented.
Streaming remains outside this slice.

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

## Native policy attachment

The TSZ controller registers `agentgateway` as a native adapter. Apply the
[native TSZ policy example](../../examples/bring-your-gateway/agentgateway/native-tsz-policy.yaml)
to a `Gateway` or `HTTPRoute` target. The controller compiles the policy,
records its trusted route binding, and creates one deterministic
`AgentgatewayPolicy` owned by the `TSZGuardrailPolicy`.

The generated policy uses buffered request and response bodies, sends headers,
skips trailers, disables ExtProc mode overrides, and maps TSZ `FailOpen` or
`FailClosed` to agentgateway's `failureMode`. It supplies gateway, listener,
route, and rule identity through ExtProc `requestAttributes`; client headers do
not choose the native policy. Deleting the TSZ policy garbage-collects the
owned agentgateway resource. Conflicts and unsupported streaming requests are
rejected by the shared controller admission path.

Agentgateway exposes one `failureMode` for the whole ExtProc attachment. Set
`failurePolicy.request` and `failurePolicy.response` to the same value. A mixed
pair is rejected with `UnsupportedCapability` before the controller creates or
updates an `AgentgatewayPolicy`; it is never collapsed to `FailOpen` in a way
that weakens the fail-closed direction.

The controller service account requires create, update, watch, and delete
permissions for `agentgatewaypolicies.agentgateway.dev`. The Helm native BYG
RBAC includes these permissions. Install the agentgateway CRDs before starting
the TSZ controller. If discovery cannot find the served
`agentgateway.dev/v1alpha1` resource, the controller disables only this adapter
and continues serving installed adapters; restart it after installing the CRD.

Enable the shared native BYG components and label ExtProc telemetry correctly:

```sh
helm upgrade --install thyris-sz deployment/helm/thyris-sz \
  --set envoyGateway.enabled=true \
  --set envoyGateway.mode=native \
  --set envoyGateway.extProc.config.gatewayAdapter=agentgateway
```

`processingTimeout` remains part of the TSZ policy contract, but agentgateway
1.5 has no timeout field on `traffic.extProc`. Configure the TSZ Service backend
deadline with a separate Service-targeted agentgateway backend policy when a
gateway-enforced deadline is required. This limitation does not change the
generated ExtProc failure mode.

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

## A2A JSON-RPC

The [A2A example](../../examples/bring-your-gateway/agentgateway/a2a-json-rpc.yaml)
uses an `AgentgatewayBackend` with an A2A target, publishes it below
`/agents/support`, and rewrites that prefix to `/` for the upstream agent. Its
ExtProc policy targets `POST` requests only, so agentgateway can serve the
Agent Card over `GET` without sending that non-JSON document to this content
adapter.

TSZ supports both the `tasks/send` method currently shown by agentgateway and
the standard `message/send` method. It inspects user message text and structured
data on requests. On responses it inspects agent messages, task-status messages,
history, and artifact text or structured data. File bytes and URIs, JSON-RPC
IDs, roles, task state, and protocol metadata are preserved. Task-management
methods and JSON-RPC error responses pass without content mutation.

agentgateway remains responsible for A2A routing, authentication, Agent Card
discovery, and task lifecycle. This profile handles complete JSON-RPC documents
only. Streaming methods such as `message/stream` and `tasks/sendSubscribe` are
reported as unsupported processing failures instead of passing uninspected;
A2A SSE, REST, gRPC, file-content inspection, and push-notification webhooks are
outside this compatibility slice.

## Generic JSON HTTP APIs

The [generic JSON example](../../examples/bring-your-gateway/agentgateway/generic-json-http.yaml)
protects a normal Kubernetes Service below `/api/customer-profiles`. The route
mapping in [the trusted routing context policy](../../examples/bring-your-gateway/agentgateway/trusted-routing-context.yaml)
overwrites `X-TSZ-Content-Adapter: generic-json`; this explicit selector keeps
unknown or malformed LLM, MCP, and A2A payloads from silently falling back to a
less specific parser. Treat the selector as trusted route configuration and do
not accept a client-provided value.

TSZ inspects each complete request or response JSON document as one structured
value. This retains full context for schema, semantic, AI, PII, secret, pattern,
allowlist, and blocklist evaluation. Objects, arrays, and scalar JSON values are
supported with `application/json` and `application/*+json` media types. `ALLOW`
and `AUDIT_ONLY` preserve the original bytes; `MASK` must produce valid JSON of
the same top-level kind; `BLOCK` uses the normal safe ExtProc immediate response.
Duplicate keys, malformed JSON, unsupported media types, and invalid mutations
follow the configured request or response failure policy.

The profile is deliberately buffered and does not inspect multipart forms,
form-encoded bodies, arbitrary text, binary data, NDJSON, JSON Text Sequences,
or streaming JSON. agentgateway continues to own HTTP routing, authentication,
authorization, retries, timeouts, and backend selection.

## Policy identity and failures

The current deployment uses the existing trusted-route-header resolver. The
route owner must overwrite `X-TSZ-Policy` in a Gateway-level `PreRouting`
transformation; a client-supplied value is never an authoritative policy
identifier. A normal `HTTPRoute` request-header filter executes too late for
agentgateway ExtProc, whose policy stage precedes post-routing transformations.
Manual manifests can continue using this mapping. Native controller-managed
attachments instead send controller-owned identity through ExtProc request
attributes and resolve it through the route binding store.

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
Responses, Anthropic Messages, MCP JSON-RPC, A2A JSON-RPC, and generic JSON HTTP
request/response bodies through the same Envoy ExtProc gRPC server used in
production. They verify request masking, immediate request blocking, response
masking, fail-open/fail-closed behavior, safe adapter identity, and PII-safe
audit/metadata output.

Run it with:

```sh
go test ./internal/extproc/envoy -run AgentgatewayOpenAIChatCompletionsCompatibility
go test ./internal/extproc/envoy -run AgentgatewayOpenAIResponsesCompatibility
go test ./internal/extproc/envoy -run AgentgatewayAnthropicMessagesCompatibility
go test ./internal/extproc/envoy -run AgentgatewayMCPJSONRPCCompatibility
go test ./internal/extproc/envoy -run AgentgatewayA2ACompatibility
go test ./internal/extproc/envoy -run AgentgatewayGenericJSONCompatibility
```

See the [example README](../../examples/bring-your-gateway/agentgateway/README.md)
for the current prerequisites, request, and known limitations.

CI downloads and checksum-verifies the pinned agentgateway 1.5.0 Linux binary,
then runs `TestLiveAgentgatewayExtProc`. To run the same test locally, set
`TSZ_TEST_AGENTGATEWAY_BINARY` to an agentgateway 1.5.0 binary and run:

```sh
go test ./internal/extproc/envoy -run '^TestLiveAgentgatewayExtProc$' -count=1 -v
```

This test starts agentgateway, the TSZ gRPC server, and a local HTTP backend.
It checks the actual HTTP request and response mutations and confirms a blocked
request does not reach the backend. The standalone fixture supplies policy and
content-adapter headers directly. Native controller tests cover
`AgentgatewayPolicy` generation, trusted identity attributes, failure mode,
ownership, updates, and deletion. A live Kubernetes matrix and the other four
payload families still need live cluster coverage before a stable compatibility
claim.
