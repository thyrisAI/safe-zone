# Gateway adapter development contract

This document is the normative development contract for adding a gateway to
Bring Your Gateway (BYG). A new adapter translates a gateway's transport and,
optionally, its declarative control-plane resources. It does not implement
content policy, detection, or provider payload handling.

The contract has two independent extension surfaces:

- A **portable data-plane adapter** translates the gateway protocol to and from
  [`extproc.ProcessingRequest`](../../internal/extproc/contract.go) and
  [`extproc.ProcessingResult`](../../internal/extproc/contract.go). Every new
  gateway integration needs this surface.
- A **native control-plane adapter** implements
  [`nativeadapter.Adapter`](../../internal/controller/nativeadapter/adapter.go)
  when the gateway has a stable declarative attachment API. This surface is
  required for a Level 2 native integration, but not for a Level 1 portable
  integration.

Envoy is the reference implementation, not part of the shared contract.
Gateway-specific protocol types, generated clients, resource types, and error
formats must remain in the adapter package. In particular,
`internal/guardrails` must never import them.

## Ownership boundaries

| Concern | Owner |
|---|---|
| Native message sequencing, buffering, cancellation, and protocol errors | Data-plane adapter |
| Conversion to `ProcessingRequest` and from `ProcessingResult` | Data-plane adapter |
| Native block response, body/header mutation, and metadata encoding | Data-plane adapter |
| Trusted gateway, route, tenant, request, and trace identity extraction | Data-plane adapter |
| Trusted route identity to policy identity resolution | Shared `PolicyResolver` plus adapter-supplied identity |
| Immutable snapshot lookup and request/response version pinning | Shared BYG runtime |
| Policy compilation, guardrail decisions, and failure-policy meaning | Shared BYG runtime |
| OpenAI, Anthropic, Gemini, MCP, and other content-shape handling | Shared content processors under `internal/extproc` |
| Capability admission and rejection | Shared controller capability checks |
| Native resources, deterministic names, ownership, and cleanup | Native control-plane adapter |
| Gateway routing, authentication, rate limits, retries, and provider credentials | Customer gateway |

An adapter may enforce transport limits needed to protect its own process. It
must not create a second policy engine or change a guardrail decision. If a new
gateway exposes a feature that the shared contract cannot represent, propose an
additive contract change and test it with Envoy and the new adapter; do not hide
gateway semantics in `Attributes` merely to avoid a design review.

## Data-plane contract

The exact Go types in [`contract.go`](../../internal/extproc/contract.go) are
the source of truth. The following rules govern their use.

### Request construction

For each native request or response stage, construct a fresh
`ProcessingRequest`:

- `Stage` is exactly `request` or `response` and must pass `Validate`.
- `Headers` use lower-case keys and preserve repeated values. Copy maps and
  value slices with `CloneHeaders`; adapter-owned buffers must not escape.
- `Body` is copied before crossing the boundary. `nil` means that this native
  stage carries no body mutation candidate; an empty, non-nil body may be a
  deliberate mutation.
- `ContentType` is derived from the stage's headers. `RequestPath` always comes
  from original request routing data, including during response processing.
- `RPCMethod` is retained from the request when a response wire format does not
  repeat it.
- `Gateway`, `Route`, `Tenant`, request IDs, and `Attributes` are populated only
  from documented sources. Mark which sources are trusted and which are merely
  informational in the integration guide.
- `TraceParent` is accepted only from a gateway-configured trusted attribute.
  Do not trust arbitrary downstream trace headers for security decisions.
- `EndOfStream` reflects the native protocol event. Per-request state must be
  isolated, bounded, cancellation-aware, and released on every exit path.

The adapter/runtime boundary must pin one immutable `PolicySnapshot` when the
request begins. The same snapshot and `PolicyVersion` are used for all request
and response stages. A reload affects new requests only. Missing, incompatible,
or failed snapshots follow the configured failure policy; they are never
treated as a clean guardrail result.

Policy authority cannot come from an untrusted client header. A preview
integration may use `X-TSZ-Policy` only when the gateway removes the downstream
value and writes exactly one trusted value. A native integration should map
trusted gateway attributes through a controller-owned `RoutePolicyBinding`.
Missing and ambiguous identities are errors and must not fall back to a default
adapter, Envoy, or audit-only behavior.

### Result mapping

The processor returns one of four actions:

| Action | Adapter obligation |
|---|---|
| `ALLOW` | Continue without changing the body unless an explicit mutation is present. |
| `MASK` | Apply the returned body and header mutations exactly once. Correct body-related headers as required by the native protocol. |
| `BLOCK` | Stop forwarding and produce the documented gateway-native safe error or immediate response. |
| `AUDIT_ONLY` | Forward the original traffic byte-for-byte; publish only safe aggregate telemetry. |

Validate every returned action. Unknown actions and impossible native mappings
are adapter errors, not `ALLOW`. `ProcessingResult.Body == nil` means no body
mutation; do not confuse it with a mutation to an empty body. The adapter owns
native status-code and error-envelope selection. Block responses must not reuse
the inspected or mutated body and must never expose a finding, matched value,
prompt, credential, or validator response.

Only [`SafeMetadata`](../../internal/extproc/contract.go) may cross into native
dynamic metadata. Preserve its stable meanings and use a gateway-specific
encoding of the `io.thyris.tsz` namespace where supported. Raw bodies, raw PII,
user IDs, unbounded errors, and credentials are forbidden in metadata, headers,
logs, metrics, and traces.

### Streaming

Streaming is optional and capability-gated. A data-plane adapter must state
whether it supports no response enforcement or event-aligned windowing. A
windowed implementation delegates inspection to `StreamingWindowProcessor`;
it must not duplicate SSE or provider parsing inside the gateway package.

Buffers and overlap windows must be bounded and must exert backpressure. On a
processor failure, fail-open forwards the original unmodified window; it must
not emit a partial mutation. A halt prevents future bytes only. Neither
windowing nor halt can retract bytes already released, so an integration guide
must not describe them as strict zero-leakage enforcement. See
[BYG streaming guarantees](../concepts/STREAMING.md).

## Capability declaration and admission

Declare a stable DNS-label name, adapter version, and the capabilities defined
by [`AdapterCapabilities`](../../internal/controller/capabilities/envoy_gateway.go):

```go
capabilities.AdapterCapabilities{
    Name:                   "example-gateway",
    Version:                "1.0.0",
    RequestHeaders:         true,
    RequestBufferedBody:    true,
    RequestBodyMutation:    true,
    ImmediateResponse:      true,
    ResponseBufferedBody:   true,
    ResponseBodyMutation:   true,
    ResponseStreaming:      capabilities.StreamingNone,
    DynamicMetadata:        false,
    NativePolicyAttachment: false,
}
```

Define what the version identifies for the adapter and keep it stable across a
release. Do not treat that single value as a replacement for the tested gateway
version range; publish compatibility separately in the supported gateway matrix
and integration guide.

Capabilities are trusted binary declarations, never user-supplied policy data.
Run shared capability checks against both inline policy configuration and the
fully resolved immutable snapshot. A policy requiring an unsupported feature
must be rejected before activation or native resource writes. Never silently
downgrade `MASK` or `BLOCK` to `AUDIT_ONLY`, and never advertise a capability
that is only partially mapped or untested.

The current capability vocabulary is intentionally small. Extend it
additively when a policy needs a distinction that affects its security
guarantee. Updating a boolean from `false` to `true` requires mapping tests and
conformance evidence.

## Native control-plane contract

Implement `nativeadapter.Adapter` only when the gateway provides a stable API
that can attach the data-plane processor deterministically. The implementation
must provide:

- A complete `Descriptor` with capabilities, supported local Gateway API target
  kinds and section scopes, owned resource kind, and success status reason.
- Idempotent `Reconcile` behavior with deterministic names and controller owner
  references. Build and validate the desired object before writing it. Failed
  updates must leave the last known good resource active.
- `Remove` behavior that deletes only resources controlled by the supplied
  policy owner. Use UID/resource-version preconditions when the native API
  supports them. Never delete a same-named foreign resource.
- A `RouteIdentity` mapping that exactly matches the trusted identity emitted by
  the data-plane adapter. Keys sharing the binding store must not collide with
  another adapter.
- Owned-resource prototypes for watches and a bounded resource count for
  metrics.

Register the implementation explicitly at controller startup with
`nativeadapter.NewRegistry`. Also register its Kubernetes schemes and narrowly
scoped RBAC. Registries reject nil, duplicate, incomplete, and portable-only
implementations; unknown `spec.adapter` values remain unsupported. Policy
authors cannot load adapter code or resource templates.

The shared controller continues to own target resolution, attachment
precedence, conflict detection, reference resolution, policy compilation,
snapshot activation, last-known-good status, and common conditions. An adapter
must not fork those rules. Vendor-specific targets or cross-namespace access
require an explicit shared-API and security review before implementation.

## Failure, limits, and lifecycle requirements

Every adapter must define and test:

- Fail-closed and explicitly configured fail-open behavior for policy lookup,
  processor errors, timeouts, unavailable transports, and audit delivery.
- Maximum body, native message, stream-buffer, and concurrent-request limits,
  with deterministic errors and no unbounded per-request state.
- Cancellation and disconnect propagation, graceful shutdown, and cleanup of
  parser/buffer references on success and every error path.
- Response-only or otherwise incomplete native lifecycles. If the request's
  pinned policy is unavailable, do not infer a response policy from client data.
- Mutation of content length, transfer encoding, compression, and checksums as
  required by the native protocol.
- Preservation of gateway-owned authentication, routing, rate limiting, model
  routing, provider transformation, retry, and usage metadata.

Production integrations default request enforcement to fail-closed. Any
gateway limitation that makes this impossible must be explicit in admission,
the compatibility matrix, and the integration guide.

## Package and dependency rules

A typical implementation uses packages such as:

```text
internal/extproc/<gateway>/             # native transport translation
internal/controller/<gateway>resource/ # optional declarative resources
cmd/tsz-<gateway>-processor/            # optional dedicated entry point
deployments/<gateway>/                  # version-pinned deployment assets
examples/bring-your-gateway/<gateway>/  # focused runnable examples
```

Do not copy provider parsers or guardrail logic into these packages. Reuse the
gateway-neutral processor and policy cache by dependency injection. Generated
protocol code and vendor SDKs must not leak into `internal/extproc` contract
types, `internal/guardrails`, policy definitions, or audit events.

Prefer stable public gateway APIs. Direct privileged control-plane or xDS
modification is a Level 3 integration and requires a separate threat model,
least-privilege design, release compatibility tests, and an experimental label.

## Required verification

Before claiming an adapter capability, add focused tests for its native mapping
and lifecycle. At minimum cover:

1. Request headers/body and response headers/body map to the correct stage.
2. Repeated headers, empty bodies, invalid sequences, body limits, and unknown
   native messages are handled deterministically.
3. `ALLOW`, `MASK`, `BLOCK`, and `AUDIT_ONLY` map without changing their
   security meaning; body and header mutations are correct.
4. Trusted identity cannot be supplied, duplicated, or overridden by the
   downstream client, and request/response processing uses one pinned version.
5. Fail-open and fail-closed cover missing policy, processor failure, timeout,
   and gateway disconnect.
6. Concurrent requests have isolated state; cancellation and graceful shutdown
   release resources.
7. Metadata, audit, metric, trace, and error outputs contain no fixture secrets
   or raw findings.
8. Every declared capability has a positive mapping test and every unsupported
   capability is rejected before activation.
9. A native adapter additionally covers idempotent reconciliation, deterministic
   ownership, foreign-resource safety, deletion, conflicts, last known good,
   status, and RBAC.

Run `go test ./...` and the relevant race, integration, and clean-cluster suites.
Every data-plane adapter must invoke the reusable contract suite from a native
package test:

```go
func TestAdapterContract(t *testing.T) {
    adaptertest.Run(t, newGatewayContractDriver())
}
```

The driver implements `adaptertest.Driver`, constructs native lifecycle
messages, calls the adapter's real input/output mapping functions, and returns
semantic observations. It must not reproduce normalization or result-mapping
logic merely to make the suite pass. The shared suite is in
[`internal/extproc/adaptertest`](../../internal/extproc/adaptertest/contract.go),
and the [Envoy driver](../../internal/extproc/envoy/contract_test.go) is the
reference implementation.

This transport contract suite and the cross-gateway conformance suite are
separate Phase 7 deliverables. Passing the contract suite proves boundary
semantics only. Every adapter must also invoke `adaptertest.RunConformance`
with a driver that runs the supplied processor and immutable snapshot through
the adapter's real native server, middleware, or plugin entry point. The shared
conformance scenarios verify request masking before upstream delivery, request
blocking, response filtering before client delivery, request/response failure
modes, and correlated PII-safe audit, metadata, and metric output.

```go
func TestAdapterConformance(t *testing.T) {
    adaptertest.RunConformance(t, newGatewayConformanceDriver(t))
}
```

See the [shared conformance suite](../../internal/extproc/adaptertest/conformance.go)
and its [Envoy driver](../../internal/extproc/envoy/conformance_test.go). Optional
scenarios are selected from the adapter's validated capability declaration.

## Documentation and release gate

Every shipping adapter must include a complete integration guide and focused,
runnable examples. The guide must state:

- Integration level and maturity (`experimental`, `preview`, or `stable`).
- Adapter version and tested gateway, Kubernetes, and API/CRD versions.
- Supported targets, request/response protocols, actions, mutation, streaming,
  metadata, and known ordering constraints.
- Trust and policy-authority sources, failure modes, body/time/concurrency
  limits, network isolation, and TLS or mTLS expectations.
- Prerequisites, installation, safe/masked/blocked verification, troubleshooting,
  upgrade, rollback, and narrowly scoped cleanup.

Examples use a local mock provider by default, contain no real credentials or
routable internal endpoints, and verify what the upstream actually received.
Pin version-sensitive dependencies and include smoke tests in CI where
practical. Add all guides to the [documentation index](../README.md) and run the
documentation link checker.

Every data-plane adapter package matching `internal/extproc/<gateway>/adapter.go`
must also have an entry in
[`adapter-releases.json`](../../examples/bring-your-gateway/adapter-releases.json).
The entry binds the implementation to its maturity, indexed integration guide,
example-set guide, smoke command, and runnable safe, request-mask,
request-block, response-mask, response-block, fail-open, fail-closed, and
telemetry directories. Each directory needs `README.md`, `policy.json`,
`request.json`, and `expected-status`; a custom runner may add gateway-specific
resources without weakening the shared assertions.

`go test ./tests/release` is the mechanical release gate. It discovers adapter
packages rather than trusting the manifest alone, rejects missing or stale
entries, validates the complete scenario set and focused files, and verifies
that the integration guide is present in the documentation index. Add the
manifest entry in the same change as a new adapter; an adapter without its
guide and runnable example contract cannot pass CI.

An adapter is ready for review only when its declared capabilities match its
tests and guide, unsupported policies fail closed at admission/reconciliation,
gateway-specific dependencies remain isolated, and the shared guardrail engine
and content-policy pipeline required no gateway-specific branch.
