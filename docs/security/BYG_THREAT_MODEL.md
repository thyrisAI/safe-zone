# Bring Your Gateway threat model

## Scope and security objectives

This threat model covers the Envoy Gateway to TSZ external-processing data
plane, the native `TSZGuardrailPolicy` controller, compiled policy storage and
distribution, and PII-safe telemetry. It does not make Envoy AI Gateway or any
other evaluated gateway a supported integration.

The objectives are to prevent policy bypass, prevent raw sensitive content
from crossing the intended boundary, preserve deterministic policy ownership,
fail according to the pinned policy, and avoid exposing content through logs,
metrics, traces, status, or errors.

## Assets and actors

Assets include prompts and responses, credentials and tool payloads, immutable
policy snapshots, route-to-policy bindings, signing/TLS material, audit events,
and availability of the gateway path. Actors include external clients,
tenant/application operators, cluster administrators, policy administrators,
gateway/controller service accounts, the TSZ processor, upstream providers,
and telemetry or audit backends.

## Trust boundaries and entry points

1. Client to Envoy is untrusted. Client policy headers and metadata are not
   authoritative.
2. Envoy to `tsz-ext-proc:9002` carries raw content and must be private,
   network-restricted, and protected with TLS/mTLS in production.
3. Controller to Kubernetes and PostgreSQL is privileged control-plane access;
   least-privilege RBAC and immutable version references are required.
4. PostgreSQL is durable policy truth. Redis carries version notifications and
   cached snapshots but is not the only durable source.
5. Audit, SIEM, metrics, and traces cross an observability boundary and may
   receive only bounded metadata, never bodies or matched values.

## Threats and mitigations

| Threat | Impact | Required mitigation |
| --- | --- | --- |
| Client policy spoofing or guardrail-disable header | Mandatory policy bypass | Gateway-owned header overwrite in preview mode; trusted route attributes and controller-owned binding in native mode; reject duplicate/ambiguous identity. |
| Missing, stale, malformed, or incompatible snapshot | Unguarded forwarding or inconsistent enforcement | Fail closed by default, explicit fail-open only, atomic activation, immutable versions, last-known-good retention, degraded telemetry. |
| Cross-tenant or cross-route policy selection | Data-policy isolation failure | Namespace/target ownership checks, exact tenant and route scope, deterministic precedence and conflict status, negative tests. |
| Processor exposure or impersonation | Raw content disclosure or policy bypass | Cluster-private service, NetworkPolicy restricted to Envoy, TLS/mTLS, certificate rotation, no public ingress or admin API on the gRPC service. |
| Oversized or fragmented payload | Memory/CPU exhaustion or inspection bypass | HTTP/gRPC/body/window limits, complete SSE-event parsing, bounded overlap, concurrency limit, timeouts, cancellation and backpressure. |
| Fail-open misuse | Confidentiality loss during dependency failure | Closed production default, explicit per-policy exception, risk approval, degraded event/metric, outage test proving the upstream outcome. |
| Streaming leakage | Already-emitted unsafe bytes cannot be recalled | Document Windowed as best-effort, buffer event-aligned windows, halt future delivery on BLOCK, use buffered response mode for zero-leakage requirements. |
| Unsafe mutation | Invalid provider payload or broken routing | Content-adapter parsing, targeted field mutation, length correction, byte-identical ALLOW/AUDIT_ONLY paths, contract and conformance tests. |
| Telemetry exfiltration | Sensitive values leave the boundary | Stable `io.thyris.tsz` schema, bounded labels, no bodies/findings/raw errors, tests for synthetic values, trusted trace extraction. |
| Controller or Extension Server compromise | Cluster or xDS integrity loss | Narrow RBAC, controller-owned resources, leader election, no direct xDS Extension Server in the default profile. |
| Audit/semantic backend failure | Silent enforcement degradation | Explicit timeout and failure policy, local deterministic checks first, no guardrail error interpreted as safe, bounded detached operational audit. |

## Residual risks

- Windowed streaming cannot retract bytes released before a later window is
  classified. Buffered non-streaming enforcement is required for a strict
  no-leak guarantee.
- A cluster administrator or compromised Envoy/controller identity can alter
  the trusted routing boundary; Kubernetes and gateway administration remain
  outside TSZ's security perimeter.
- Semantic validators may be probabilistic and slower than deterministic
  rules. Their egress and failure behavior require explicit deployment policy.
- Preview mode depends on correct gateway header overwrite configuration;
  native mode reduces but does not eliminate operator misconfiguration risk.

## Verification and review triggers

Contract, conformance, Kind smoke, outage, mTLS/NetworkPolicy, policy conflict,
version-skew, and telemetry-safety tests provide current evidence. Re-review
this model for a new adapter, a new trusted identity source, direct xDS access,
a new streaming mode, a new external validator, or a change to failure-policy
defaults. Report bypass or exposure findings privately as described in
[SECURITY.md](../../SECURITY.md).
