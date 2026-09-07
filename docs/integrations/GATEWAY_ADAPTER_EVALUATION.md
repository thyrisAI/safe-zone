# Gateway adapter evaluation

- **Evaluation date:** 2026-09-07
- **Status:** Architecture evaluation; not a support or compatibility claim
- **Scope:** Kong, Apache APISIX, NGINX, Traefik, Istio, AWS API Gateway,
  Azure API Management, Google Cloud API Gateway, and Apigee

This evaluation applies the BYG
[adapter-development contract](ADAPTER_DEVELOPMENT.md) to the Phase 7 candidate
gateways. It identifies viable adapter shapes and the evidence still required;
it does not register another adapter, publish a tested version range, or change
the shipped Envoy-only support status.

## Outcome

1. **Keep Kong Gateway + KIC as the demand-selected implementation spike.** Its
   plugin lifecycle exposes request and fully buffered response phases, body
   mutation, and immediate responses. KIC also supplies a concrete Kubernetes
   attachment path. The spike must prefer the stable PDK and must not depend on
   removed legacy Go plugin-server directives.
2. **Promote NGINX Gateway Fabric to a parallel protocol-compatibility probe.**
   Its experimental `PayloadProcessor` is the most direct newly documented
   external payload-processing surface and attaches to a Gateway or HTTPRoute.
   The published example proves request and response blocking, but not TSZ
   masking, safe metadata, or wire-protocol compatibility.
3. **Retain Apache APISIX as the strongest fallback new transport.** The
   external Go runner documents request short-circuiting and response-body
   rewriting. Its post-response path has material feature incompatibilities
   that must be tested against customer routing and upstream security needs.
4. **Treat Istio as an Envoy-adapter reuse profile, not a new data-plane
   adapter.** An `EnvoyFilter` can insert `ext_proc`, but this is an xDS-coupled,
   upgrade-sensitive attachment. A separate transport implementation would
   duplicate the existing Envoy adapter without adding a gateway capability.
5. **Keep Traefik Proxy experimental.** A Go HTTP middleware is a plausible
   portable adapter and Gateway API can reference a Traefik `Middleware`, but
   the open-source plugin mechanism remains experimental. Traefik Hub's
   product-specific Content Guard is useful evidence, not a portable extension
   contract for TSZ.
6. **For managed gateways, investigate Azure API Management first and Apigee
   second when customer demand names the product and tier.** Both have native
   request/response policy pipelines. AWS API Gateway and Google Cloud API
   Gateway lack an in-path external payload-processing hook capable of meeting
   strict BYG request mutation and response filtering; support there would be a
   chained/full proxy profile rather than a native adapter.

The provisional Kong choice and its demand gates remain recorded in
[Next gateway selection](NEXT_GATEWAY_DECISION.md). Verified customer evidence
can reorder these technical priorities.

## Evaluation method

Each candidate is evaluated against the same security-relevant questions:

- Can it inspect and mutate a buffered request before any upstream byte is
  sent, and can it stop forwarding with a safe local response?
- Can it inspect and mutate a complete non-streaming response before any body
  reaches the client?
- Is streaming explicitly bounded, backpressured, and honest about bytes that
  have already been released?
- Can it carry trusted route/policy identity and PII-safe decision metadata?
- Can policy attachment be reconciled deterministically with visible status and
  without granting TSZ broad gateway control-plane privileges?
- Are timeout, failure, cancellation, ordering, body-limit, and deployment
  semantics testable through the shared contract and conformance suites?

The evidence labels below mean:

- **Documented:** the vendor or project documents the necessary primitive.
- **Spike:** a plausible primitive exists, but its BYG behavior is unproven.
- **No direct hook:** the documented gateway surface cannot satisfy the
  capability without making TSZ or another function the upstream proxy.

## Self-managed and Kubernetes gateways

| Gateway | Preferred adapter shape | Request mask / block | Buffered response filter | Streaming | Native attachment | Disposition |
|---|---|---|---|---|---|---|
| Kong Gateway + KIC | Lua or current Go-PDK plugin calling the BYG processor | Documented primitives; conformance required | Documented buffered `response` phase; conformance required | Chunk filtering exists, but HTTP/2/gRPC and leakage guarantees need a spike | `KongPlugin` attached through KIC/Gateway API | **Selected implementation spike** |
| Apache APISIX | External Go plugin runner over its Unix-socket RPC | Runner documents direct response and request modification | Runner documents `ResponseFilter` body rewrite | No strict BYG guarantee established | Route plugin configuration is native; Gateway API ownership mapping needs a spike | **Fallback** |
| NGINX Gateway Fabric | `PayloadProcessor` `ExtProcess` backend if its protocol can map to BYG | Request inspection/block documented; masking unproven | Response inspection/block documented; masking unproven | No BYG streaming guarantee established | Experimental inherited policy targets Gateway or HTTPRoute | **Parallel compatibility probe** |
| Traefik Proxy | Go/Wasm HTTP middleware | Plausible; plugin implementation required | Plausible by wrapping the response writer; buffering semantics unproven | No strict BYG guarantee established | Gateway API `ExtensionRef` can reference a Traefik `Middleware` | **Experimental / demand-gated** |
| Istio | Reuse the Envoy `ext_proc` adapter through a narrowly scoped `EnvoyFilter` | Existing Envoy path is capable; Istio ordering unproven | Existing Envoy path is capable; Istio ordering unproven | Existing Envoy modes require an Istio compatibility run | `EnvoyFilter` today; do not invent a second transport | **Envoy reuse investigation** |

### Kong Gateway

Kong plugins run at request and response lifecycle entry points. The `response`
handler receives the whole upstream response before it is sent to the client
and enables buffered proxy mode. The PDK can read the raw client body, replace
the body sent to the Service while correcting `Content-Length`, replace a
buffered response body, and terminate request processing with
`kong.response.exit`. See the official
[plugin lifecycle](https://developer.konghq.com/gateway/entities/plugin/),
[`kong.request`](https://developer.konghq.com/gateway/pdk/reference/kong.request/),
[`kong.service.request`](https://developer.konghq.com/gateway/pdk/reference/kong.service.request/),
and [`kong.response`](https://developer.konghq.com/gateway/pdk/reference/kong.response/)
references.

The preferred spike is a minimal Lua plugin unless the current Go PDK proves
equivalent lifecycle, cancellation, memory, and release compatibility. Kong's
current [breaking-change record](https://developer.konghq.com/gateway/breaking-changes/)
states that legacy `go_pluginserver_exe` and `go_plugins_dir` directives were
removed, so an implementation must not copy an obsolete Go plugin-server
deployment model.

Kong remains a strong Level 2 candidate because KIC can attach plugin
configuration to route resources, but plugin order relative to authentication,
rate limiting, transformations, and logging must be pinned. A block in an early
phase still permits later response/log phases, so the adapter must distinguish
gateway-generated local replies from upstream responses.

### Apache APISIX

APISIX supports in-process Lua lifecycle phases and external plugin runners.
The official [external plugin documentation](https://apisix.apache.org/docs/apisix/external-plugin/)
describes language runners managed as APISIX subprocesses over a Unix socket.
The [Go runner guide](https://apisix.apache.org/docs/go-plugin-runner/getting-started/)
documents a request filter that can respond without touching upstream and a
`ResponseFilter` that reads and rewrites the upstream response body.

This is a good match for a Go BYG bridge, but the external runner and APISIX
share a host/user and restart lifecycle. Isolation, socket permissions, crash
behavior, and rolling upgrades therefore need explicit tests. The documented
[`ext-plugin-post-resp`](https://apisix.apache.org/docs/apisix/plugins/ext-plugin-post-resp/)
path is incompatible with `proxy-control`, `proxy-mirror`, and `proxy-cache`,
and does not support mTLS between APISIX and its upstream in that mode. These
constraints can violate the requirement to preserve the customer's native
gateway behavior and must be treated as admission/compatibility limits.

### NGINX and NGINX Gateway Fabric

The evaluation distinguishes raw NGINX configuration, NGINX Gateway Fabric,
and commercial F5 products. They are not interchangeable support targets.

NGINX Gateway Fabric 2.7 documents an experimental `PayloadProcessor` whose
ordered `ExtProcess` entry calls an external payload service and whose inherited
policy targets a Gateway or HTTPRoute. The official
[PayloadProcessor example](https://docs.nginx.com/nginx-gateway-fabric/how-to/f5-ai-guardrails/)
shows request content being blocked before the LLM and response content being
withheld before the client. The
[API reference](https://docs.nginx.com/nginx-gateway-fabric/reference/api/)
states that request and response payloads are processed in order.

This surface merits a small protocol probe before a custom NGINX module or
snippet is considered. The API is explicitly experimental and the published
integration targets F5 AI Guardrails. TSZ must first prove protocol ownership,
request and response masking, deterministic fail-open/closed behavior,
authentication to the processor, limits, and safe metadata. NGINX
[Snippets](https://docs.nginx.com/nginx-gateway-fabric/traffic-management/snippets/)
are disabled by default and can expose TLS material or destabilize generated
configuration; they are not an acceptable default BYG integration mechanism.

### Traefik

Traefik's middleware chain is based on Go's `http.Handler`, which makes a
portable request/response wrapper technically plausible. Gateway API
`ExtensionRef` can reference Traefik
[`Middleware`](https://doc.traefik.io/traefik/reference/routing-configuration/kubernetes/gateway-api/)
resources. However, the open-source
[plugin configuration](https://doc.traefik.io/traefik/master/reference/install-configuration/experimental/plugins/)
is explicitly experimental, and the official documentation does not establish
bounded full-response buffering, cancellation, or a stable external processor
protocol for third-party guardrails.

Traefik Hub publishes a
[Content Guard](https://doc.traefik.io/traefik-hub/ai-gateway/middlewares/content-guard)
that inspects request/response JSON and buffers SSE response rules at the cost
of real-time streaming. This proves product interest in the problem, but it is
not evidence that open-source Traefik exposes the same contract to TSZ. Evaluate
Hub and Proxy as separate products and licensing/deployment profiles.

### Istio

Istio's data plane is Envoy, so the first option is reuse of the shipped TSZ
`ext_proc` service. Istio's
[`EnvoyFilter`](https://istio.io/latest/docs/reference/config/networking/envoy-filter/)
can add HTTP filters and clusters at gateway or sidecar scope, but Istio warns
that patches are tied to internal implementation and xDS APIs, can destabilize
the mesh, and require monitoring across proxy upgrades. Conflicting filters
also have undefined behavior.

`WasmPlugin` offers a higher-level extension resource with ordering,
`targetRefs`, integrity hashes, and fail-open/fail-closed loading behavior; see
the official [WasmPlugin reference](https://istio.io/latest/docs/reference/config/proxy_extensions/wasm-plugin/).
It would still require a proxy-Wasm bridge and duplicate protocol work unless
it merely configures or complements the Envoy adapter. Therefore:

- do not create an `istio` data-plane adapter;
- spike a narrowly scoped `EnvoyFilter` attachment to the existing adapter;
- pin Istio and proxy versions and verify filter order with authn, authz,
  telemetry, routing, retries, and any inference extension;
- classify automatic `EnvoyFilter` reconciliation as higher-risk Level 3 until
  a stable Istio API can express the required `ext_proc` attachment.

## Managed cloud gateways

Managed products require a named product, tier, region, networking model, and
customer requirement before implementation. A generic `cloud-gateway` adapter
would hide incompatible execution, IAM, quota, payload, and deployment models.

| Product | Native BYG feasibility | Principal constraint | Disposition |
|---|---|---|---|
| AWS API Gateway HTTP/REST APIs | **No direct strict adapter** | Authorizers decide access; HTTP API mappings cannot replace bodies or call an external guardrail inline. A Lambda/HTTP proxy would make TSZ part of the backend chain. | Chained proxy only; demand-gated |
| Azure API Management | **Plausible policy adapter** | XML policy composition, body preservation, timeout/tier behavior, and non-streaming buffering require proof. | First managed-cloud spike after verified demand |
| Google Cloud API Gateway | **No direct strict adapter** | Documented OpenAPI extensions cover backend, auth, quota, and management configuration, not an in-path request/response payload callout. | Chained backend proxy only |
| Apigee | **Plausible policy/shared-flow adapter** | ServiceCallout latency/failure behavior, message mutation, deployment revision ownership, and streaming require proof. | Second managed-cloud candidate |

### AWS API Gateway

An HTTP API Lambda authorizer returns an allow/deny decision; it is not a body
mutation or response-filtering hook. HTTP API
[parameter mapping](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-parameter-mapping.html)
can read JSON paths but only changes request headers/query/path and response
headers/status. It also reserves headers such as `Content-Length` and
`Transfer-Encoding`. REST API mapping templates can reshape known bodies, but
they cannot invoke the TSZ processor and apply a dynamic guardrail result.

A Lambda or HTTP integration can proxy the entire request through TSZ and
return a complete response, as shown by the
[Lambda proxy response contract](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-develop-integrations-lambda.html).
That is a chained/full proxy deployment with an additional failure and
credential boundary, not a native BYG adapter that leaves the customer's
existing integration in place.

### Azure API Management

Azure API Management policies execute in `inbound`, `backend`, `outbound`, and
`on-error` sections. The
[`send-request` policy](https://learn.microsoft.com/en-us/azure/api-management/send-request-policy)
can call an external service with an explicit timeout from any section, while
[`set-body`](https://learn.microsoft.com/en-us/azure/api-management/set-body-policy)
can replace request or response bodies. `return-response` and `on-error` can
implement blocking and failure modes. Microsoft also documents first-party AI
gateway content-safety policies, confirming that prompt/response policy
processing is an intended APIM use case.

A TSZ policy adapter is plausible, but the spike must prove that the body is
read with `preserveContent` correctly, that no original bytes escape on a
closed failure, that processor credentials use named values or managed
identity, and that workspace/self-hosted/managed tiers do not silently differ.
Do not represent policy expressions as equivalent to the shared Go contract
until they pass its conformance scenarios.

### Google Cloud API Gateway

Google Cloud API Gateway's documented
[OpenAPI extensions](https://cloud.google.com/api-gateway/docs/oasv3-extensions)
configure API management, authentication, quotas, deadlines, and backend
integration. They do not expose a general inbound/outbound payload policy or
external processing callout. A TSZ-protected backend proxy is possible, but it
changes the customer flow to `API Gateway -> TSZ proxy -> provider`; classify
that as a chained proxy rather than a native adapter.

Google Cloud API Gateway and Apigee are separate products. Capabilities proven
for Apigee must never be attributed to Google Cloud API Gateway.

### Apigee

Apigee's
[`ServiceCallout`](https://cloud.google.com/apigee/docs/api-platform/reference/policies/service-callout-policy)
can invoke a service from a request flow and use its response later in the API
proxy. `AssignMessage` can operate on the current request or response, and
[`RaiseFault`](https://cloud.google.com/apigee/docs/api-platform/reference/policies/raise-fault-policy)
can terminate normal processing. This creates a plausible policy/shared-flow
adapter for buffered request and response enforcement.

The spike must model TSZ results using bounded variables, avoid logging message
content, define `continueOnError` and fault rules for each failure mode, and
verify that a policy bundle or shared-flow revision is attached and rolled back
atomically. Apigee hybrid and managed runtime compatibility must be recorded
separately.

## Required spikes and exit criteria

No candidate becomes supported until its spike records a pinned compatibility
matrix and passes all applicable shared contract and conformance scenarios.

| Priority | Spike | Exit criterion |
|---:|---|---|
| 1 | Kong request/response plugin | Mask and block fixtures pass before upstream/client; plugin order, body limits, timeout, cancellation, metadata, and KIC ownership are deterministic. |
| 2 | NGINX Gateway Fabric `PayloadProcessor` protocol | A local mock proves the exact wire contract and whether request/response mask, block, failure modes, identity, and metadata can map without a vendor-specific TSZ fork. |
| 3 | APISIX Go runner | Shared suites pass and documented post-response incompatibilities are either absent from the target profile or rejected during admission. |
| 4 | Istio Envoy reuse | Existing TSZ ext-proc passes on a pinned Istio ingress gateway with deterministic filter ordering and no new data-plane adapter. |
| 5 | Traefik Proxy middleware | A pinned experimental plugin safely bounds and rewrites request/response bodies; a production support decision explicitly accepts the plugin lifecycle risk. |
| Demand-gated | Azure APIM or Apigee | A named tier/runtime passes non-streaming conformance and has least-privilege deployment, private connectivity, secret storage, rollback, quotas, and cost documented. |

Every spike must additionally verify:

- downstream clients cannot choose or overwrite policy authority;
- gateway authentication, authorization, routing, retry, rate-limit, provider
  credentials, and usage metadata retain their original meaning;
- fail-closed is possible for request enforcement, and fail-open never forwards
  a partial mutation;
- compressed, oversized, empty, malformed, and disconnected traffic is bounded;
- audit, logs, metrics, traces, and native metadata contain no raw fixture data;
- streaming is admitted only for a tested capability and never described as
  zero leakage after bytes have been released;
- control-plane writes are deterministic, reversible, scoped to owned
  resources, and expose acceptance/programming status.

## Decision record

| Date | Decision | Reason |
|---|---|---|
| 2026-09-07 | Kong remains the selected next implementation spike. | Best current combination of demand proxy, request/response PDK surface, and KIC attachment; direct customer proof is still required. |
| 2026-09-07 | NGINX Gateway Fabric receives a parallel compatibility probe. | Experimental `PayloadProcessor` is a direct external request/response processing surface, but TSZ protocol and masking remain unproven. |
| 2026-09-07 | APISIX remains fallback; Istio reuses Envoy; Traefik remains experimental. | These choices minimize duplicated transport logic and make documented lifecycle risks explicit. |
| 2026-09-07 | Azure APIM leads managed candidates; AWS API Gateway and Google Cloud API Gateway are proxy-only. | APIM and Apigee expose programmable request/response policy flows; the simpler managed gateways do not expose the strict BYG hook. |
