# Envoy Gateway Integration

This guide covers Envoy Gateway v1.8.3 with Gateway API v1.5.1. The checked-in
Kind example under `deployments/envoy-gateway/` is the reproducible reference
environment.

## Support status and integration level

The Envoy Gateway adapter maturity is **preview** for the version pair above. TSZ supports
both a Level 1 portable profile using a manually managed
`EnvoyExtensionPolicy` and a Level 2 native profile using
`TSZGuardrailPolicy`. Envoy AI Gateway and other Envoy-compatible products are
not included in this claim; each needs its own compatibility evidence.

Requests flow from the client to Envoy, through TSZ's `ext_proc` service for
request enforcement, to the selected upstream. Supported buffered responses
flow back through the same processor before reaching the client. Envoy remains
responsible for routing, authentication, rate limiting, retries, and provider
credentials. The adapter supports `ALLOW`, `AUDIT_ONLY`, `MASK`, and `BLOCK`,
buffered request/response mutation, bounded streaming modes, safe dynamic
metadata, metrics, and tracing. Ordering constraints and the different
guarantees of streamed response modes are documented below and in the
[streaming contract](../concepts/STREAMING.md).

The gateway-owned route header in the portable profile and trusted route
attribute in the native profile are policy-authority sources. Client headers
are not authoritative. Request enforcement defaults closed. Limits are pinned
in the active policy and deployment; TLS/mTLS and network isolation remain
operator responsibilities described in this guide.

## Deployment package

The supported Kubernetes packaging entry point is the Kustomize package in
[`deployments/envoy-gateway/kustomize/`](../../deployments/envoy-gateway/kustomize/).
It provides portable preview, native managed, and separate-data-namespace
production overlays. The package README explains its prerequisites, image and
Secret configuration boundaries, install commands, verification, and removal.

Operational deployment, observability, and incident-triage guidance is kept
in the [BYG operations guides](../operations/BYG_DEPLOYMENT.md) to keep this
integration guide focused on the Envoy-specific configuration contract.

## Network isolation

The Kind bootstrap applies the `same-namespace` overlay under
`deployments/envoy-gateway/kustomize/network-policy/`.
It selects `tsz-ext-proc` pods, denies all ingress and egress by default, then
permits only:

- ext_proc gRPC traffic on TCP `9002` from Envoy proxy pods in
  `envoy-gateway-system` bearing `security.thyris.ai/tsz-peer: "true"`;
- DNS to CoreDNS on TCP/UDP `53`;
- PostgreSQL and Redis in the reference namespace on TCP `5432` and `6379`.

The `EnvoyProxy` resource in `echo-demo.yaml` injects the peer label, so the
policy does not rely on Envoy Gateway implementation labels. This pod-label
configuration is verified against Envoy Gateway v1.8.3.

The reference deployment intentionally keeps PostgreSQL and Redis in
`tsz-byg-demo`. Kustomize overlays make the topology explicit:

```bash
# Current Kind topology
kubectl kustomize deployments/envoy-gateway/kustomize/network-policy/overlays/same-namespace

# Production topology: processor in tsz-system; PostgreSQL and Redis in tsz-data
kubectl kustomize deployments/envoy-gateway/kustomize/network-policy/overlays/separate-data-namespace
```

For another namespace layout, change the `namespace` and `tsz-data` values in
the separate-data-namespace overlay together; it retains the dependency
`podSelector` and does not grant namespace-wide egress.
Keep the HTTP health/metrics port (`8080`) out of the ext_proc ingress rule.
Expose it separately and only to an authenticated operational or Prometheus
workload when needed.

## Availability and autoscaling

The reference `tsz-ext-proc.yaml` includes an initial production scaling
profile: two to ten replicas, CPU and memory targets of 60% and 80%, a
five-minute scale-down stabilization window, and a PDB allowing at most one
voluntary pod eviction. CPU utilization is calculated from the container CPU
request, so do not remove that request while the HPA is enabled.

These values are intentionally conservative defaults, not a capacity promise.
Before production rollout, use processor request rate, p95/p99 latency, CPU,
memory, error rate, and concurrent-stream measurements to tune them. The PDB
only protects voluntary disruptions such as node drain and does not protect
against an unexpected node or process failure.

NetworkPolicy is an L3/L4 control and does not provide TLS. Production Envoy
to TSZ traffic must additionally use TLS or mTLS; this reference policy limits
which workloads may initiate the gRPC connection.

## TLS and mTLS for the external processor

The runnable reference is
`examples/bring-your-gateway/19-mtls-and-network-policy`. It uses an isolated,
throwaway ECDSA P-256 CA to verify the TSZ gRPC server and authenticate Envoy
with a client certificate. It is a local demonstration, not a production CA.

Enable TSZ mTLS only by setting all three values below; the process fails at
startup if one is missing, unreadable, or invalid:

```yaml
env:
  - name: TSZ_GRPC_TLS_CERT_FILE
    value: /tls/tls.crt
  - name: TSZ_GRPC_TLS_KEY_FILE
    value: /tls/tls.key
  - name: TSZ_GRPC_TLS_CLIENT_CA_FILE
    value: /tls/client-ca.crt
```

TSZ then requires and verifies a client certificate on the gRPC listener with
TLS 1.2 or newer. Envoy Gateway v1.8.3 is configured through a namespaced
`Backend`: `caCertificateRefs` holds the TSZ server trust anchor,
`clientCertificateRef` holds Envoy's client certificate, and `sni` must match
a DNS SAN on TSZ's server certificate. The Kind bootstrap enables Envoy
Gateway's optional Backend API for this example.

In production, obtain and rotate both leaf certificates through the
organization's PKI (typically cert-manager backed by Vault, cloud private CA,
or another approved issuer). Do not commit private keys, reuse the example CA,
or configure `insecureSkipVerify`.

During `kind-bootstrap.sh verify-replica-lifecycle` and
`verify-controller-reconciliation`, the bootstrap creates two temporary probe
pods. A pod in `envoy-gateway-system` with the TSZ peer label must connect to
`tsz-ext-proc:9002`; an unlabeled pod in `tsz-byg-demo` must be rejected. The
bootstrap fails if either assertion is not true.

## Choosing an installation profile

| | Preview (manual) | Native (managed) |
| --- | --- | --- |
| Policy source | `tsz-policy` CLI plus a hand-applied `EnvoyExtensionPolicy` | `TSZGuardrailPolicy` CRD |
| Route identity | Trusted `X-TSZ-Policy` header set by a gateway `RequestHeaderModifier` / `ClientTrafficPolicy` | Envoy `ext_proc` `xds.route_name` attribute |
| Lifecycle | Imperative operator actions | Declarative reconciliation by `tsz-controller` |
| Status visibility | Policy CLI and processor logs | `kubectl get tszguardrailpolicy` conditions |
| Best fit | Evaluation, quick-start, or one manually managed route | Multi-route, multi-team, and production Kubernetes environments |

Choose one profile for an ext-proc deployment. The resolver is global:

```yaml
env:
  - name: TSZ_POLICY_RESOLUTION_MODE
    value: header # or attribute
```

Do not combine a manual attachment and a managed attachment on the same route.
Do not set `header` for some routes and `attribute` for others in a shared
`tsz-ext-proc` Deployment. Such a configuration can select the wrong identity
source or cause a mandatory policy to be unresolved.
git 
## Response-only local replies

Envoy Gateway can invoke `ext_proc` for a response even when an earlier native
filter, such as JWT authentication or local rate limiting, rejected the request
before TSZ received request headers. Such a callback has no pinned TSZ policy
snapshot. In Envoy Gateway v1.8.3, the standard
`BackendTrafficPolicy.responseOverride.source: Local` response filter runs
after `ext_proc` on the response path, so it cannot provide a trustworthy
marker to TSZ at that point.

TSZ therefore handles every response-only callback through the global
`TSZ_FAIL_MODE`: `closed` is the default and returns TSZ's safe response-stage
`403`; `open` is an explicit availability choice that continues the original
response. Both outcomes emit the bounded
`tsz_extproc_response_without_request_state_total{outcome=...}` metric and a
safe degraded response audit event with reason
`response_without_request_state`. Do not set `open` in production merely to
preserve JWT or rate-limit local replies; evaluate that availability trade-off
explicitly.

Likewise, if a native local reply follows a request callback but its body is
not a supported OpenAI response shape (for example Envoy's local rate-limit
body), the pinned response failure policy applies. Its default closed outcome
is also TSZ's safe response-stage `403`; it is not a promise to preserve the
original `429` body.

## Prometheus metrics

`tsz-ext-proc` exposes Prometheus metrics on its existing HTTP health port at
`GET /metrics`. The deployment manifest names this port `health` (default
`8080`); expose it only to the Prometheus scraper, not to public traffic.

```bash
kubectl -n tsz-byg-demo port-forward service/tsz-ext-proc 8080:8080
curl -s http://127.0.0.1:8080/metrics | grep '^tsz_extproc_'
```

The processor emits transaction, decision, detection-category, processing
duration, bounded failure-reason, timeout, body-size, active-stream, and
stream-halt metrics. `action`, `stage`, `policy`, `type`, `direction`, and
`reason` are the only labels. RID, Envoy request IDs, tenant/user identifiers,
request bodies, raw PII, credentials, and error text are never labels or
metric values.

## OpenTelemetry tracing

Tracing is disabled unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set. The endpoint
uses OTLP/gRPC and TLS by default; set `OTEL_EXPORTER_OTLP_INSECURE=true` only
for a local trusted collector without TLS.

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: otel-collector.observability.svc.cluster.local:4317
  - name: OTEL_EXPORTER_OTLP_INSECURE
    value: "false"
```

TSZ creates `tsz.extproc.request` and `tsz.extproc.response` spans, child
`tsz.guardrail.validator` spans, `tsz.semantic_model` spans for AI validators,
and instrumentation spans for PostgreSQL and Redis operations. A W3C
`traceparent` is continued only when Envoy supplies it through a configured,
trusted ext-proc attribute; TSZ does not trust a client header directly.

Safe span attributes include processing stage, policy ID/version, action,
detection count, degraded state, TSZ RID, and Envoy request ID. Request and
response bodies, raw PII, credentials, validator text, Redis command
arguments, and database query variables are never recorded in spans.

## Preview / portable installation

Use this profile when the gateway integration is managed manually or the
gateway does not have a native TSZ controller adapter.

1. Create, compile, and activate a policy with `tsz-policy`.
2. Deploy `tsz-ext-proc` with `TSZ_POLICY_RESOLUTION_MODE=header` (or omit the
   variable; `header` is the default).
3. Apply
   `deployments/envoy-gateway/tsz-ext-proc-envoy-extension-policy.yaml` after
   changing its `targetRefs` and backend reference for your namespace.
4. Configure a gateway-owned **early** header mutation that overwrites
   `X-TSZ-Policy` with the activated policy name. Do not trust a value sent by
   the downstream client. The `ClientTrafficPolicy` in `echo-demo.yaml` shows
   the Envoy Gateway pattern.
5. Verify the route through Envoy and inspect the processor policy cache.

The manual `EnvoyExtensionPolicy` remains a supported preview example. It is
not removed when installing the native controller; remove it deliberately as
part of migration.

## Native / managed installation

Use this profile when Kubernetes should be the attachment and lifecycle control
plane.

1. Install the generated CRD and RBAC:

   New installations use `security.thyris.ai/v1beta1`. The CRD also serves
   deprecated `v1alpha1` manifests. For an existing installation, apply the
   [policy API upgrade procedure](../operations/TSZ_POLICY_API_UPGRADE.md) before
   rolling out the beta controller.

   ```bash
   kubectl apply -f config/crd/bases/security.thyris.ai_tszguardrailpolicies.yaml
   kubectl apply -f config/rbac/role.yaml
   ```

2. Deploy `tsz-controller` using
   `deployments/envoy-gateway/controller/controller.yaml`.
3. Deploy `tsz-ext-proc` with
   `TSZ_POLICY_RESOLUTION_MODE=attribute`. The checked-in native manifest is
   `deployments/envoy-gateway/tsz-ext-proc.yaml`.
4. Remove any manual TSZ `EnvoyExtensionPolicy` for routes being migrated.
5. Apply a `TSZGuardrailPolicy`. The ready-to-run Kind example is
   `deployments/envoy-gateway/controller/checkpoint-inline-policy.yaml`.

For an inline policy, the controller creates and activates a deterministic,
controller-owned policy snapshot. For `policySource: PostgresRef`, it verifies
the specified immutable policy version and references it without writing a new
snapshot. In both cases it creates an owned `EnvoyExtensionPolicy` and stores
the trusted native route binding.

Verify reconciliation:

```bash
kubectl -n tsz-byg-demo get tszguardrailpolicy
kubectl -n tsz-byg-demo get tszguardrailpolicy checkpoint-inline -o yaml
kubectl -n tsz-byg-demo get envoyextensionpolicy -o yaml
```

Look for `Accepted`, `ResolvedRefs`, `Programmed`, and `PolicySynced`. A
conflicted attachment is explicitly reported as `Programmed=False` with reason
`Conflicted`; it must not leave a managed `EnvoyExtensionPolicy` behind.

## Upgrades and rollback

Treat a TSZ upgrade as three separate operations: the processor and controller
workloads, the Kubernetes API objects, and the active policy snapshot. Do not
combine all three changes in one unverified deployment. Record the currently
running image digests, manifest revision, Envoy Gateway and Gateway API
versions, active policy versions, and the output of the checks below before
starting. Back up PostgreSQL using the project-approved backup procedure
before applying a release that contains database migrations.

The reference environment has two `tsz-ext-proc` replicas, readiness probes,
and a PodDisruptionBudget. A rolling Deployment update therefore keeps a ready
processor available when the new image becomes ready. This is not a guarantee
of zero disruption: keep the configured Envoy failure policy in mind and
perform the upgrade in a maintenance window appropriate for a fail-closed
route.

### Pre-upgrade checks

1. Read the release notes and confirm that the target TSZ release supports the
   installed Envoy Gateway and Gateway API versions. This guide's tested
   reference is Envoy Gateway v1.8.3 with Gateway API v1.5.1.
2. Confirm that the current installation is healthy and that every managed
   policy is reconciled:

   ```bash
   kubectl -n tsz-byg-demo get deployment tsz-ext-proc tsz-controller
   kubectl -n tsz-byg-demo rollout status deployment/tsz-ext-proc
   kubectl -n tsz-byg-demo rollout status deployment/tsz-controller
   kubectl -n tsz-byg-demo get tszguardrailpolicy
   kubectl -n tsz-byg-demo get envoyextensionpolicy
   ```

   Inspect each `TSZGuardrailPolicy` if necessary. `Accepted`,
   `ResolvedRefs`, `Programmed`, and `PolicySynced` must be `True`; resolve a
   degraded or conflicted state before upgrading.
3. Send the documented safe, masking, and blocking requests through each
   protected route. Retain the resulting request ID, TSZ RID, and active
   policy version as the pre-upgrade baseline. Verify that a blocked request
   does not reach the upstream.
4. For the preview profile, also record the manually applied
   `EnvoyExtensionPolicy`, the gateway-owned trusted header mutation, and the
   active policy name. For the native profile, record the
   `TSZGuardrailPolicy` manifests and their `status.observedGeneration`.

### Workload and manifest upgrade

For the alpha-to-beta policy API transition, follow the
[version-specific upgrade and storage migration guide](../operations/TSZ_POLICY_API_UPGRADE.md).
It requires the dual-version CRD before the beta controller and retains alpha
client access for workload rollback.

1. Review the target manifests with `kubectl diff` (or the equivalent Helm or
   Kustomize render-and-diff step used by the deployment). Pin image tags or,
   preferably, image digests; do not upgrade a production installation using a
   floating tag.
2. If the release changes the CRD or RBAC, apply those compatible API assets
   before deploying the controller. Do not delete a CRD to upgrade or
   downgrade it: deleting it also deletes its policy objects.
3. Apply the target controller and processor manifests, then wait for each
   rollout to finish:

   ```bash
   kubectl apply -f config/crd/bases/security.thyris.ai_tszguardrailpolicies.yaml
   kubectl apply -f config/rbac/role.yaml
   kubectl apply -f deployments/envoy-gateway/controller/controller.yaml
   kubectl apply -f deployments/envoy-gateway/tsz-ext-proc.yaml
   kubectl -n tsz-byg-demo rollout status deployment/tsz-controller --timeout=5m
   kubectl -n tsz-byg-demo rollout status deployment/tsz-ext-proc --timeout=5m
   ```

   Replace these paths and namespace with the rendered, version-pinned assets
   used by the environment. Run any release-provided database migration exactly
   once and wait for it to succeed before relying on a new runtime that needs
   its schema.
4. Repeat the policy-condition checks and the safe/mask/block verification.
   Confirm that processor replicas converge on the expected immutable policy
   version and that Envoy still reaches the processor over the configured
   TLS/mTLS connection. Do not declare the upgrade complete merely because the
   Deployments are available.

### Application rollback

If the new controller or processor fails readiness, introduces incorrect
enforcement, or cannot maintain a supported policy version, stop the rollout
and restore the last known-good **workload** revision. For a Deployment
managed directly by Kubernetes this is:

```bash
kubectl -n tsz-byg-demo rollout undo deployment/tsz-controller
kubectl -n tsz-byg-demo rollout undo deployment/tsz-ext-proc
kubectl -n tsz-byg-demo rollout status deployment/tsz-controller --timeout=5m
kubectl -n tsz-byg-demo rollout status deployment/tsz-ext-proc --timeout=5m
```

For a Helm or GitOps deployment, use that system's recorded previous release
instead of mixing it with `kubectl rollout undo`. Reapply the corresponding
last known-good manifests, and restore PostgreSQL only if the release notes
explicitly require a reversible database rollback. A database restore is a
separate, potentially disruptive operation; do not use it as the first
response to a processor image failure.

Never roll back a CRD by deleting it. If a released CRD is not compatible with
the prior controller, stop and follow the version-specific release procedure;
the safe default is to keep the compatible CRD while rolling back only the
controller and processor images.

After an application rollback, verify the managed policy conditions,
`EnvoyExtensionPolicy` attachment, processor readiness, and the same
safe/mask/block requests used before the change. Investigate any policy-version
skew or `PolicySynced=False` before resuming normal change activity.

### Policy rollback

Policy rollback is independent of an application rollback. TSZ policy
snapshots are immutable. Activating a new snapshot supersedes the previous
one; rollback reactivates that previous compiled snapshot atomically and
notifies processor replicas through Redis. In-flight requests continue with
the version selected when they started, while a request and its response use
the same selected version.

For a preview-managed policy, run `tsz-policy` in an approved administrative
environment with the same PostgreSQL and Redis configuration as the processor:

```bash
tsz-policy -name <policy-name> -rollback
```

The command restores the **latest superseded** version only; it does not accept
an arbitrary version, recompile a policy, or modify an immutable definition.
It prints the restored policy name, version, and snapshot ID. If the required
version is not the latest superseded version, stop and create, validate,
compile, and activate a new policy from the approved historical definition
instead of editing a snapshot in place.

For a native-managed inline policy, revert the approved
`TSZGuardrailPolicy` manifest in source control and apply it. The controller
then compiles and activates the deterministic replacement snapshot. For
`policySource: PostgresRef`, change the reference only to an existing,
compatible immutable version and wait for `PolicySynced=True`. In either case,
do not delete the managed `EnvoyExtensionPolicy`; the controller owns it.

After a policy rollback, verify the active version in processor audit or
PII-safe `io.thyris.tsz` metadata, wait for every replica to converge, and run
safe, mask, and block requests. If Redis or PostgreSQL is unavailable, follow
the configured failure policy and treat the rollback as incomplete until the
controller and processor readiness checks recover.

## Migration between profiles

1. Confirm all target routes are covered by native `TSZGuardrailPolicy`
   objects and report `Programmed=True`.
2. Change every replica of `tsz-ext-proc` to
   `TSZ_POLICY_RESOLUTION_MODE=attribute`, then roll it out.
3. Remove the manual TSZ `EnvoyExtensionPolicy` and the old trusted-header
   identity mutation for the migrated routes.
4. Verify traffic and the CRD conditions before considering the migration
   complete.

To move back to preview, reverse the order: restore the manual attachment and
trusted header identity first, roll ext-proc to `header`, and only then delete
the managed CRD.

## Runnable verification and cleanup

The complete local example set is indexed in the
[Bring Your Gateway example guide](../../examples/bring-your-gateway/README.md).
It includes safe, request-mask, request-block, response-mask, response-block,
fail-open, fail-closed, and telemetry scenarios against a local mock provider.
Run all of them with:

```bash
examples/bring-your-gateway/smoke.sh
```

For a failed installation, first inspect `EnvoyExtensionPolicy` acceptance,
the Envoy and `tsz-ext-proc` pods, processor logs, and the policy conditions
described above. The detailed incident paths are in
[BYG troubleshooting](../operations/BYG_TROUBLESHOOTING.md).

Remove the throwaway reference cluster after verification:

```bash
deployments/envoy-gateway/kind-bootstrap.sh down
```

This command is scoped to the configured example Kind cluster. Production
cleanup must use the owning Helm/GitOps workflow, preserve audit evidence, and
remove attachments before the processor while accounting for the configured
failure mode.
