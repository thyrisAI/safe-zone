# BYG Envoy Gateway Kind fixtures

These Kustomize resources support the reproducible local Kind examples. The
supported production packaging entry point is
`deployment/helm/thyris-sz`; these fixtures are intentionally kept beside the
examples instead of forming a second deployment package. Read
[`docs/integrations/ENVOY_GATEWAY.md`](../../../../docs/integrations/ENVOY_GATEWAY.md)
before running them.

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

The base pins no production image. `preview` and `native` retain the checked-in
`thyris-sz:local` image used by the Kind reference. Before using either outside
that environment, use the Helm chart with a released, immutable image digest.

## Render and install

Inspect the exact resources first:

```bash
kubectl kustomize examples/bring-your-gateway/cluster/kustomize/overlays/native
kubectl diff -k examples/bring-your-gateway/cluster/kustomize/overlays/native
```

Install the selected profile:

```bash
# Manual/portable profile in the reference namespace.
kubectl apply -k examples/bring-your-gateway/cluster/kustomize/overlays/preview

# Native controller-managed profile in the reference namespace.
kubectl apply -f config/crd/bases/security.thyris.ai_tszguardrailpolicies.yaml
kubectl apply -f config/rbac/role.yaml
kubectl apply -k examples/bring-your-gateway/cluster/kustomize/overlays/native

```

The reference manifests retain local-only database and Redis values for
reproducibility. Do not adapt these fixtures into a production deployment;
use the Helm chart with Secret-backed values and the mTLS configuration from
the integration guide.

Verify the rollout and, for native profiles, reconciliation:

```bash
kubectl -n tsz-byg-demo rollout status deployment/tsz-ext-proc --timeout=5m
kubectl -n tsz-byg-demo rollout status deployment/tsz-controller --timeout=5m
kubectl -n tsz-byg-demo get tszguardrailpolicy,envoyextensionpolicy
```

A healthy native policy reports `Accepted=True`, `ResolvedRefs=True`,
`Programmed=True`, and `PolicySynced=True`.

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

Remove only resources owned by the selected local overlay:

```bash
kubectl delete -k examples/bring-your-gateway/cluster/kustomize/overlays/native
```

This does not delete PostgreSQL, Redis, Envoy Gateway, Gateway API resources,
or manually created route attachments. Do not delete a `TSZGuardrailPolicy`
until its guarded route is deliberately being detached.
