# Next gateway selection: Kong Gateway

- **Decision date:** 2026-09-07
- **Status:** Provisional selection for a compatibility and adapter spike
- **Selected target:** Kong Gateway with Kong Ingress Controller (KIC)

Kong Gateway is the next non-Envoy gateway selected for BYG validation. This
does not mean that a Kong adapter exists, is conformant, or is supported. The
decision authorizes customer discovery and a bounded compatibility spike; the
validation gates below control implementation and any support claim.
The full technical comparison is maintained in the
[gateway adapter evaluation](GATEWAY_ADAPTER_EVALUATION.md).

## Demand evidence and limitations

A public Safe Zone issue search for Kong, APISIX, NGINX, Traefik, and Istio
returned only [BYG issue #34](https://github.com/thyrisAI/safe-zone/issues/34),
which lists candidates. Its public comments record phase progress and do not
identify a customer request. Repository popularity must not be presented as a
confirmed TSZ customer commitment.

Until publishable customer interviews exist, this decision uses public project
interest as a demand proxy and then considers BYG product and implementation
fit. GitHub stars are directional community-interest signals, not market share,
production deployment counts, or customer demand.

The following snapshot was read from the public GitHub repository API on the
decision date:

| Candidate repository | Stars | Interpretation |
|---|---:|---|
| [Traefik](https://github.com/traefik/traefik) | 64,766 | Strongest general gateway/ingress signal; remains shortlisted. |
| [Kong Gateway](https://github.com/Kong/kong) | 44,092 | Strong API-gateway signal plus plugin and Kubernetes attachment fit; selected. |
| [Istio](https://github.com/istio/istio) | 38,373 | Strong service-mesh signal; investigate safe Envoy adapter reuse first. |
| [Apache APISIX](https://github.com/apache/apisix) | 17,091 | Strong alternative with an official external plugin runner. |
| [Envoy AI Gateway](https://github.com/envoyproxy/ai-gateway) | 2,001 | Strategically relevant, but an Envoy compatibility profile rather than a new transport adapter. |
| [NGINX Gateway Fabric](https://github.com/nginx/nginx-gateway-fabric) | 1,159 | Repository-specific signal; it does not represent the wider NGINX installed base. |

Verified customer or design-partner evidence overrides this proxy-based choice.
Only customer-approved, non-sensitive summaries may be recorded publicly. Never
publish customer names, traffic samples, internal hostnames, topology, or
commercial details here.

## Why Kong

Kong has the best combined evidence for the next new adapter:

1. It has a large public API-gateway community signal, second only to Traefik in
   this candidate snapshot and stronger than the other API-gateway-specific
   repositories evaluated here.
2. Kong's official plugin model exposes request and response lifecycle hooks and
   supports custom plugins in multiple languages. This makes a Level 1 adapter
   that calls the gateway-neutral TSZ processor plausible. See
   [Kong custom plugins](https://developer.konghq.com/custom-plugins/) and the
   [plugin lifecycle](https://developer.konghq.com/gateway/entities/plugin/).
3. KIC uses Gateway API resources as native inputs. `KongPlugin` can be attached
   to `HTTPRoute`, providing a concrete Level 2 investigation path using the BYG
   controller boundary. See the [KIC Gateway API guide](https://developer.konghq.com/kubernetes-ingress-controller/gateway-api/)
   and [`KongPlugin` reference](https://developer.konghq.com/kubernetes-ingress-controller/reference/custom-resources/).
4. Kong owns routing, authentication, rate limiting, retries, and upstream
   credentials while TSZ remains responsible for content guardrails. This
   preserves the BYG responsibility boundary.

Generic plugin extensibility is not proof of BYG conformance. Request and
response mutation, immediate responses, streaming, metadata, ordering, and
failure behavior must be tested explicitly.

## Candidate disposition

| Candidate | Disposition | Next evidence needed |
|---|---|---|
| Kong Gateway + KIC | **Selected for spike** | Validate portable plugin transport, then `HTTPRoute`-attached `KongPlugin` configuration and ownership. |
| Apache APISIX | **Fallback** | Its official [external plugin runner](https://apisix.apache.org/docs/apisix/external-plugin/) offers a strong Go integration path. Reconsider if Kong cannot meet response or lifecycle requirements. |
| Traefik | **Shortlisted** | Collect direct TSZ demand and validate a stable request/response extension surface. |
| Istio | **Reuse investigation** | Determine whether the existing Envoy transport can be attached safely before creating another adapter. |
| NGINX Gateway Fabric | **Parallel compatibility probe** | Its experimental [`PayloadProcessor`](https://docs.nginx.com/nginx-gateway-fabric/how-to/f5-ai-guardrails/) directly offloads request/response payloads; prove TSZ protocol, masking, metadata and failure semantics. |
| Envoy AI Gateway | **Existing-adapter compatibility track** | Validate filter ordering, transformations, routing/fallback, token metadata, and versions with the Envoy adapter. |
| Managed cloud gateways | **Demand-gated** | Evaluate the named product and tier; Azure APIM leads the technical shortlist, followed by Apigee. AWS and Google Cloud API Gateway require a chained proxy for strict enforcement. |

## Validation gates

Do not mark Kong supported or begin an unbounded implementation until the spike
records all of the following:

- At least one verified design-partner request or two independent customer
  requests for Kong. A maintainer may approve experimental work without this
  threshold only by recording the exception.
- A pinned Kong Gateway, KIC, Kubernetes, and Gateway API version matrix.
- Safe pass-through, request masking, and blocking before upstream delivery.
- Non-streaming response masking and blocking before client delivery.
- Exact `ALLOW`, `MASK`, `BLOCK`, and `AUDIT_ONLY` mappings plus deterministic
  fail-open and fail-closed behavior.
- Bounded bodies, timeouts, concurrency, cancellation, and processor-outage
  behavior.
- A trusted route/policy identity that a downstream client cannot override.
- Preservation of Kong authentication, rate limits, routing, retries, and
  upstream credentials.
- PII-safe audit, metrics, traces, and gateway metadata.
- An ordering study for Kong plugins and explicit handling of buffered,
  compressed, and streaming bodies.
- A documented choice between local plugin, external plugin server, and remote
  HTTP/gRPC processing, including upgrade and failure-domain tradeoffs.
- Contract and conformance tests plus a runnable mock-provider example required
  by the [adapter development contract](ADAPTER_DEVELOPMENT.md).

If request blocking or non-streaming response enforcement cannot be proven,
Kong leaves the native adapter track. An audit-only or chained-proxy profile may
remain only when its reduced guarantee is explicit; it must never be selected
silently for a strict policy.

## Re-evaluation

Review this decision before full implementation, when material customer evidence
arrives, or after 90 days, whichever happens first. Add a dated decision-log
entry instead of rewriting the original evidence.

For each demand signal, record the date, anonymized source class, gateway and
deployment model, required actions/protocols/streaming behavior, native-control
plane need, and willingness to test a preview. Do not store sensitive customer
information.

| Date | Outcome | Evidence |
|---|---|---|
| 2026-09-07 | Kong selected provisionally for the next adapter spike. | No direct public TSZ request; public-interest proxy plus plugin and Gateway API fit. |
