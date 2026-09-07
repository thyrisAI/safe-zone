# BYG Troubleshooting Guide

Use this guide to triage a BYG incident without weakening policy enforcement or
exposing guarded content. Start by recording the Envoy request ID, TSZ RID,
trace ID (when present), route, policy ID/version, Deployment revision, and
timestamp. Do not paste raw requests, responses, credentials, or detected PII
into tickets or logs.

## First response

1. Identify whether the route uses the preview/manual or native/managed
   profile. A shared processor cannot safely mix their resolver modes.
2. Check processor and controller readiness, recent restarts, and the active
   failure mode before changing an Envoy policy.
3. Reproduce only with a safe fixture or an approved masked test payload.
4. If enforcement is unavailable, follow the configured fail-open/fail-closed
   behavior; do not change it ad hoc during an incident.

```bash
kubectl -n tsz-system get pods
kubectl -n tsz-system get deployment tsz-ext-proc tsz-controller
kubectl -n tsz-system get tszguardrailpolicy,envoyextensionpolicy
kubectl -n tsz-system logs deployment/tsz-ext-proc --since=15m
kubectl -n tsz-system logs deployment/tsz-controller --since=15m
```

Use `tsz-byg-demo` for the reference profiles.

## Symptoms and checks

| Symptom | Check | Safe corrective action |
| --- | --- | --- |
| Route is not guarded | Confirm the `EnvoyExtensionPolicy` targets the intended route. In native mode inspect `Accepted`, `ResolvedRefs`, `Programmed`, and `PolicySynced`. | Fix the policy reference, target, or conflict; do not add a second manual attachment to a native-managed route. |
| `Programmed=False` or `PolicySynced=False` | Inspect the full `TSZGuardrailPolicy` status and controller logs. Check referenced policy/template/validator availability and adapter capabilities. | Correct the invalid reference or capability mismatch, then let the controller reconcile. The last known-good configuration should remain active during a failed update. |
| Envoy cannot connect to ext_proc | Check Service endpoints, processor readiness, NetworkPolicy peer label/namespace, and TLS certificate/SNI configuration. | Restore the matching Service, labeled Envoy peer, network rule, or approved certificate; never broaden ingress to all pods as a quick fix. |
| Timeout, 413, or failed processing | Compare body size and processing time with configured limits; inspect validator and dependency latency through traces. | Reduce unsupported payload size, tune limits after capacity testing, or repair the slow dependency. Do not silently convert a strict route to audit-only. |
| Request or response was not mutated as expected | Confirm the OpenAI payload shape, active policy version, action, and Envoy filter ordering. | Correct the adapter/policy configuration and test with a local mock upstream to observe what it received. |
| Response behavior differs after JWT/rate limiting | Determine whether Envoy produced a response-only local reply without a request-stage snapshot. | Review the documented `TSZ_FAIL_MODE` trade-off; do not assume TSZ can preserve a native local response body. |
| Streaming output was emitted before enforcement halted it | Identify the streaming mode and delivery boundary. | Use the documented windowed/halt guarantees; do not claim zero leakage for already emitted content. |
| Different replicas select different policy versions | Check Redis/PostgreSQL reachability, processor readiness, snapshot-distribution metrics, and audit version fields. | Restore dependencies and wait for convergence. Treat the route as degraded until the expected version is active everywhere. |

## Policy attachment and reconciliation

Native policies are declarative. Inspect status before editing resources:

```bash
kubectl -n tsz-system get tszguardrailpolicy <policy-name> -o yaml
kubectl -n tsz-system get envoyextensionpolicy -o yaml
```

Common causes are an invalid `targetRefs` target, overlapping policies,
unresolved policy references, unsupported adapter capabilities, or a controller
that cannot reach PostgreSQL/Redis. Resolve the stated condition reason rather
than deleting the controller-owned `EnvoyExtensionPolicy`. Detailed profile
behavior is in the [Envoy integration guide](../integrations/ENVOY_GATEWAY.md#native--managed-installation).

## Connectivity, TLS, and network policy

The processor has separate gRPC and health/metrics listeners. Verify the
Service port and endpoint for gRPC first, then inspect NetworkPolicy and mTLS
only for the ext_proc path. A readiness probe succeeding does not prove that
Envoy has a permitted, trusted gRPC connection.

Use the controlled mTLS example and its certificate requirements as the
reference. Certificate expiry, a wrong SNI, missing client CA, wrong Service
DNS name, and an Envoy pod without the required peer label are common causes.
The full configuration and test behavior are documented under
[TLS and mTLS](../integrations/ENVOY_GATEWAY.md#tls-and-mtls-for-the-external-processor).

## Timeouts, mutation, and streaming

Use correlated metrics and traces to distinguish a processor timeout from a
validator, database, Redis, or semantic-model delay. Verify body size before
raising a limit. A body-size rejection is deliberate, deterministic behavior;
raising limits changes the memory and latency envelope and requires load
testing.

For mutation problems, confirm both the request/response content adapter and
the Envoy filter order. Test through the local mock provider so that the
observed upstream body is evidence, not an assumption. For streaming, consult
[the streaming guarantee](../concepts/STREAMING.md#2-byg--envoy-ext_proc-windowed-enforcement)
before changing enforcement mode.

## Escalation and cleanup

Escalate with sanitized correlated IDs, policy version, rendered manifest
revision, Envoy Gateway/Gateway API version, timestamps, condition reasons,
and relevant bounded metrics. Include what a safe fixture did and whether the
upstream received it; exclude payload contents.

After resolution, remove temporary port-forwards, test policies, debug logging,
and diagnostic collector access. If a deployment or policy needs to be
reverted, use the documented [upgrade and rollback procedure](../integrations/ENVOY_GATEWAY.md#upgrades-and-rollback), not ad-hoc resource deletion.

