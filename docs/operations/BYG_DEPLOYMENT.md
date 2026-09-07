# BYG Deployment Operations Guide

This guide is the operational companion to the
[Envoy Gateway integration guide](../integrations/ENVOY_GATEWAY.md). It helps
platform operators choose a deployment topology, prepare dependencies, and
operate a Bring Your Gateway (BYG) installation. Use the integration guide for
the authoritative installation manifests, NetworkPolicy, TLS/mTLS, HPA/PDB,
and upgrade/rollback procedures; this guide does not duplicate them.

## Prerequisites and ownership

Before installing TSZ, the platform team must provide:

- a supported Envoy Gateway and Gateway API version;
- a Kubernetes namespace for the processor and, for the native profile, the
  controller;
- reachable PostgreSQL and Redis services, with credentials supplied from
  Kubernetes Secrets rather than committed manifests;
- a private image registry location and an immutable TSZ image tag or digest;
- a Gateway and `HTTPRoute` owned by the application or gateway team; and
- an approved certificate issuer if Envoy-to-TSZ TLS/mTLS is required.

TSZ owns guardrail processing and, in the native profile, reconciliation of
`TSZGuardrailPolicy` into the required Envoy attachment. The gateway team
continues to own TLS termination, authentication, authorization, routing, rate
limiting, provider credentials, and upstream availability.

## Select a topology

| Model | When to use it | Operational consequence |
| --- | --- | --- |
| Shared processor | Several protected routes or gateways call a centrally deployed `tsz-ext-proc` Service. | Scale and observe the processor as a shared service; isolate it with NetworkPolicy and use trusted route identity. This is the default production model. |
| Sidecar processor | One workload has an exceptional isolation or locality requirement. | The application team owns replica coupling, lifecycle, resource contention, and sidecar upgrades. It is not the reference deployment and needs separate capacity validation. |

Do not mix preview (`header`) and native (`attribute`) policy resolution modes
in one shared processor Deployment. Select the profile described in the
[integration guide](../integrations/ENVOY_GATEWAY.md#choosing-an-installation-profile)
and use a distinct Deployment when the modes must coexist.

## Install and verify

Use the packaged Kustomize profiles for a new installation:

```bash
kubectl kustomize deployments/envoy-gateway/kustomize/overlays/production
kubectl apply -k deployments/envoy-gateway/kustomize/overlays/production
```

The production profile requires an immutable image reference and Secret-backed
database/Redis settings before it is applied. Its complete prerequisites and
profile-specific commands are in the
[Kustomize package README](../../deployments/envoy-gateway/kustomize/README.md).

For a native installation, the operational acceptance check is not just a
ready Pod. Confirm the controller has reconciled every intended policy:

```bash
kubectl -n tsz-system rollout status deployment/tsz-ext-proc --timeout=5m
kubectl -n tsz-system rollout status deployment/tsz-controller --timeout=5m
kubectl -n tsz-system get tszguardrailpolicy,envoyextensionpolicy
```

Each attached `TSZGuardrailPolicy` must report `Accepted=True`,
`ResolvedRefs=True`, `Programmed=True`, and `PolicySynced=True`. Then send a
safe, a masking, and a blocking request through Envoy and verify the upstream
receives only the intended request. The exact checks are in the
[Envoy integration guide](../integrations/ENVOY_GATEWAY.md#native--managed-installation).

## Capacity and availability operations

Capacity planning starts with measured request rate, p95/p99 processor latency,
CPU, memory, error rate, and concurrent streams—not the example defaults.
Tune the processor deployment's HPA, resource requests/limits, and PDB only
after representative load testing. The current reference behavior and
limitations are documented under
[Availability and autoscaling](../integrations/ENVOY_GATEWAY.md#availability-and-autoscaling).

During maintenance, use readiness and rollout status as the traffic-drain
gate. A PodDisruptionBudget protects only voluntary disruptions; it does not
replace multi-zone scheduling, database/Redis availability, or a tested
processor-unavailability response. Treat `fail-open` as an explicit,
route-specific availability decision, not an upgrade workaround.

## Security and network operations

Keep the ext_proc gRPC listener private. The reference NetworkPolicy permits
only labeled Envoy data-plane pods, DNS, PostgreSQL, and Redis; it does not
make the health or metrics endpoint public. Review the actual rendered policy
whenever namespaces, Envoy labels, or dependency locations change.

For production traffic, configure TLS or mTLS and manage certificate issuance,
rotation, and expiry through the organization’s PKI. Do not reuse the Kind
example CA or place keys in a Kustomize overlay. The required traffic rules and
certificate behavior are documented in
[Network isolation](../integrations/ENVOY_GATEWAY.md#network-isolation) and
[TLS and mTLS](../integrations/ENVOY_GATEWAY.md#tls-and-mtls-for-the-external-processor).

## Change, backup, and cleanup

Use the [upgrade and rollback runbook](../integrations/ENVOY_GATEWAY.md#upgrades-and-rollback)
for processor/controller image changes and immutable policy rollback. Back up
PostgreSQL according to the organization’s approved procedure before a release
with database migrations. Redis distributes snapshot activation notifications;
it is not a replacement for the durable PostgreSQL backup.

To remove a package, delete only the selected Kustomize overlay. This does not
delete Envoy Gateway, routes, PostgreSQL, Redis, or manually created preview
attachments. Detach policies deliberately and preserve required audit records
before removing an environment.

