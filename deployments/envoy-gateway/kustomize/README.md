# BYG Envoy Gateway Kustomize package

This package deploys the TSZ External Processor and, for the native profiles,
the TSZ Gateway Controller. It is the supported packaging entry point for the
Envoy Gateway BYG reference integration. Read
[`docs/integrations/ENVOY_GATEWAY.md`](../../../docs/integrations/ENVOY_GATEWAY.md)
before installing it.

The package does **not** install Envoy Gateway, a `Gateway`, an `HTTPRoute`,
PostgreSQL, Redis, or production credentials. Those resources belong to the
platform that adopts TSZ. The processor needs a reachable PostgreSQL and Redis
service, and the native profiles need Envoy Gateway and Gateway API installed
before their CRD and controller resources can be reconciled.

## Profiles

| Profile | Use case | What it installs |
| --- | --- | --- |
| `overlays/preview` | Manual/portable preview | Processor, Service, HPA, PDB and NetworkPolicy. The operator creates the `EnvoyExtensionPolicy` and trusted route identity. |
| `overlays/native` | Native managed installation in the reference namespace | Preview resources plus the controller. Install the TSZ CRD and generated RBAC prerequisite once, then attach a `TSZGuardrailPolicy`. |
| `overlays/production` | Native managed production topology | Native resources in `tsz-system`, with NetworkPolicy and service DNS configured for PostgreSQL and Redis in `tsz-data`. |

The base pins no production image. `preview` and `native` retain the checked-in
`thyris-sz:local` image used by the Kind reference. Before using either outside
that environment, replace it with a released, immutable image digest. The
`production` overlay deliberately renders
`ghcr.io/thyrisai/thyris-sz:REPLACE_WITH_RELEASE_TAG`; replace that placeholder
with a released tag or digest before applying it.

## Render and install

Inspect the exact resources first:

```bash
kubectl kustomize deployments/envoy-gateway/kustomize/overlays/native
kubectl diff -k deployments/envoy-gateway/kustomize/overlays/native
```

Install the selected profile:

```bash
# Manual/portable profile in the reference namespace.
kubectl apply -k deployments/envoy-gateway/kustomize/overlays/preview

# Native controller-managed profile in the reference namespace.
kubectl apply -f config/crd/bases/security.thyris.ai_tszguardrailpolicies.yaml
kubectl apply -f config/rbac/role.yaml
kubectl apply -k deployments/envoy-gateway/kustomize/overlays/native

# Production native profile. Edit the image and database/Redis credentials
# before applying it.
kubectl create namespace tsz-system --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f config/crd/bases/security.thyris.ai_tszguardrailpolicies.yaml
kubectl apply -f config/rbac/role.yaml
kubectl apply -k deployments/envoy-gateway/kustomize/overlays/production
```

Do not put database DSNs or Redis passwords in an overlay committed to source
control. The reference manifests retain their local demo values for
reproducibility. For production, patch each Deployment's `DB_DSN` and
`REDIS_URL` environment variables to consume Secret-backed values, and use
the mTLS configuration described in the integration guide.

Verify the rollout and, for native profiles, reconciliation:

```bash
kubectl -n tsz-system rollout status deployment/tsz-ext-proc --timeout=5m
kubectl -n tsz-system rollout status deployment/tsz-controller --timeout=5m
kubectl -n tsz-system get tszguardrailpolicy,envoyextensionpolicy
```

Use `tsz-byg-demo` instead of `tsz-system` for the preview and native reference
profiles. A healthy native policy reports `Accepted=True`,
`ResolvedRefs=True`, `Programmed=True`, and `PolicySynced=True`.

## Configuration boundaries

- Create Gateway and route resources separately, then attach a manual
  `EnvoyExtensionPolicy` in preview mode or a `TSZGuardrailPolicy` in native
  mode. Do not install both attachments on a route.
- The NetworkPolicy permits ext_proc traffic only from labeled Envoy pods and
  permits DNS, PostgreSQL and Redis egress. For another namespace layout,
  update the `separate-data-namespace` NetworkPolicy overlay and processor
  service DNS names together.
- HPA, PDB, health probes, resource requests/limits, failure mode and body
  limits originate in `tsz-ext-proc.yaml`. Tune them only after load testing.
- Apply the CRD and generated RBAC prerequisites before the controller, and
  never delete a CRD as an upgrade or rollback technique. The generator-owned
  prerequisite files remain in `config/` so they cannot drift from their
  source of truth. Follow the upgrade and rollback runbook in the integration
  guide.

## Removal

Remove only resources owned by the selected overlay:

```bash
kubectl delete -k deployments/envoy-gateway/kustomize/overlays/production
```

This does not delete PostgreSQL, Redis, Envoy Gateway, Gateway API resources,
or manually created route attachments. Do not delete a `TSZGuardrailPolicy`
until its guarded route is deliberately being detached.
