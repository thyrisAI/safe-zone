# TSZGuardrailPolicy API graduation and upgrade

## Decision and scope

The Phase 6 API graduation in [BYG issue #34](https://github.com/thyrisAI/safe-zone/issues/34)
introduces `security.thyris.ai/v1beta1` for `TSZGuardrailPolicy`. The beta API is
the preferred client version and the only storage version. `v1alpha1` remains
served, with a deprecation warning, so existing manifests and alpha clients
continue to work. This is a beta compatibility commitment, not GA (`v1`).

| API version | Served | Storage for new writes | Usage |
| --- | --- | --- | --- |
| `security.thyris.ai/v1beta1` | Yes | Yes | New manifests and the current controller |
| `security.thyris.ai/v1alpha1` | Yes | No | Existing clients and controller workload rollback |

The schemas, defaults, validation rules, status subresource and printer columns
are identical. Conversion uses Kubernetes' default `None` strategy: the API
server changes the representation's `apiVersion` without translating fields.
No conversion webhook or additional service/certificate dependency is needed.
Future schema changes must preserve lossless conversion or introduce a reviewed
conversion mechanism before they ship.

This changes only the TSZ API group. Envoy's `gateway.envoyproxy.io/v1alpha1`
and Gateway API types keep their own independent versions. TSZ policy snapshot
versions in PostgreSQL, policy identity, target precedence, finalizers and
runtime enforcement are unaffected by the API representation change.

## Additive native adapter selection

The subsequent Phase 6 native-adapter change adds optional `spec.adapter` to both
served versions with default `envoy-gateway` and an immutable-field validation.
All pre-existing fields and their defaults remain unchanged. Alpha/beta conversion
remains lossless. Upgrade the CRD before the controller; the API-server upgrade
test verifies that stored alpha policies acquire the Envoy default on read.
Only Envoy is registered in the shipped binary. Older controllers must not manage
future non-Envoy policies because they do not understand the selector. See
[native gateway adapters](../integrations/NATIVE_GATEWAY_ADAPTERS.md).

## Compatibility evidence and adopter feedback

Evidence recorded for the beta implementation on 2026-09-07:

| Evidence | Result and limitation |
| --- | --- |
| Frozen alpha schema from commit `62d03c7` | `api/testdata/v1alpha1-crd.yaml` is compared against both generated schemas, defaults, validation, status and printer columns, excluding only the explicit additive `spec.adapter` selector. Do not regenerate this baseline. |
| Kubernetes API server + etcd, envtest `1.35.0` | Automated in-place upgrade from the frozen CRD; alpha/beta reads, writes, status, defaults, invalid inputs, controller reference-failure reporting and storage rewrite/alpha client rollback are tested. |
| Existing controller behavior | Beta controller runs the existing policy resolution, precedence, conflict, compilation and last-known-good unit tests. An alpha-owned Envoy attachment keeps its UID and deterministic name when reconciled by beta. |
| Reference deployment | Kubernetes `1.35.5`, Envoy Gateway `1.8.3`, Gateway API `1.5.1` are pinned by the repository. The API-server tests above do not substitute for an end-to-end rollout through this complete stack. |
| Public adopter record | `docs/ADOPTERS.md` lists Thyris.AI using TSZ in production. It does not establish adoption of this CRD version. |
| API-specific feedback | The issue comments record Phase 1–5 implementation progress, but contain no CRD-specific adopter review or beta sign-off. No independent adopter compatibility feedback is claimed. |

The available evidence supports a schema-preserving beta implementation and a
continued alpha compatibility window. Field redesigns and GA are deferred until
actual API usage feedback is recorded. This preserves existing choices even
where schema and runtime validation differ; for example, `policyRef.version`
is optional in the schema but the controller still rejects an unpinned reference.
The graduation does not silently tighten admission for existing users.

Before declaring a production rollout validated, add a feedback entry with:

- A dated issue/PR link and the reviewing adopter or an authorized anonymized identifier.
- Kubernetes, Gateway API, Envoy, TSZ controller and processor versions.
- Inline/PostgresRef usage, target scopes, failure modes and streaming mode exercised.
- Upgrade and workload rollback results, observed status conditions and remaining issues.
- The resulting API decision, regression test and resolution for each issue.

Do not put policy contents, credentials or detected values in the feedback record.
GA requires documented real deployment feedback, successful full-stack upgrade
and rollback evidence, and resolution of outstanding API design/compatibility
issues. These are open gates; the beta implementation does not fabricate them.

## Upgrade an existing cluster

Use the intended kubeconfig context and the version-pinned release assets. The
commands below are operator actions; they are not run automatically by the
controller or by installing the CRD.

1. Record the current controller/processor image digests and take a policy backup:

   ```bash
   kubectl config current-context
   policy_backup_dir="$(mktemp -d ./tsz-policy-backup.XXXXXX)"
   kubectl get crd tszguardrailpolicies.security.thyris.ai -o yaml > "$policy_backup_dir/crd-before.yaml"
   kubectl get tszguardrailpolicies.v1alpha1.security.thyris.ai --all-namespaces -o json > "$policy_backup_dir/policies-before.json"
   ```

   Protect these files as configuration backups. Record existing policy conditions
   and verify safe/mask/block traffic before upgrading.

2. Apply the CRD that serves both versions **before** upgrading the controller:

   ```bash
   kubectl apply -f config/crd/bases/security.thyris.ai_tszguardrailpolicies.yaml
   kubectl wait --for=condition=Established crd/tszguardrailpolicies.security.thyris.ai --timeout=60s
   kubectl get crd tszguardrailpolicies.security.thyris.ai -o jsonpath='{range .spec.versions[*]}{.name}{" served="}{.served}{" storage="}{.storage}{"\n"}{end}'
   kubectl get tszguardrailpolicies.v1beta1.security.thyris.ai --all-namespaces
   ```

   Verify both endpoints are served and only beta is storage. The old controller
   can continue using alpha while this step completes. Applying the new controller
   against an alpha-only CRD is unsupported because beta discovery will fail.

3. Deploy the beta controller using the existing
   [workload upgrade procedure](../integrations/ENVOY_GATEWAY.md#workload-and-manifest-upgrade).
   The API transition does not require a processor or database schema change.
   Update GitOps and hand-maintained policy manifests to
   `apiVersion: security.thyris.ai/v1beta1`; no spec edits are required. Apply to
   the same namespace/name, preserving object UID and attachment ownership.

4. Verify `Accepted`, `ResolvedRefs`, `Programmed` and `PolicySynced`, attachment
   ownership, immutable policy versions, and safe/mask/block traffic. In-flight
   requests retain the snapshot selected at request start.

## Rewrite existing storage

Changing `storage: true` affects future writes; it does not rewrite old objects.
An alpha API write after this upgrade is also stored as beta. Both versions stay
listed in `status.storedVersions` until old storage has been migrated.

During a controlled migration window, ensure no administrator or automation can
change the CRD's storage version. After verifying the beta storage setting:

```bash
kubectl get tszguardrailpolicies.v1beta1.security.thyris.ai --all-namespaces --chunk-size=0 -o json > "$policy_backup_dir/policies-to-rewrite.json"
kubectl replace -f "$policy_backup_dir/policies-to-rewrite.json"
```

This lists and rewrites every policy, retaining `resourceVersion`, spec, metadata
and object identity. The status subresource is preserved by the API server. If
there are no policies, skip `replace`. On conflicts or any other failure, read a
fresh list and repeat; **do not** clear storage history after a partial rewrite.
Never use `replace --force`, which deletes and recreates resources.

Only after every existing object was successfully rewritten and beta remains the
sole storage version, remove the old storage entry:

```bash
kubectl patch crd tszguardrailpolicies.security.thyris.ai --subresource=status --type=merge -p '{"status":{"storedVersions":["v1beta1"]}}'
kubectl get crd tszguardrailpolicies.security.thyris.ai -o jsonpath='{.status.storedVersions}{"\n"}'
```

Patching `storedVersions` is bookkeeping, not the migration itself. The envtest
upgrade test exercises this sequence. Alpha remains served after migration;
clients may migrate their manifests separately.

## Rollback and alpha retirement

For workload rollback, retain the dual-version CRD and restore the recorded
alpha-based controller image. It can read and update both old and newly created
policies through the alpha endpoint; all writes still use beta storage. The
owner-reference compatibility test also covers switching the Envoy attachment
back to an alpha owner reference without creating a duplicate.

Do not delete the CRD or reapply the old alpha-only CRD. An old CRD can remove
the beta endpoint, conflict with `storedVersions`, and break newer clients.
Changing storage back to alpha would require a separate reverse migration and
is not part of normal workload rollback.

No alpha removal date is set in this change. Retirement requires a separately
announced release, confirmed absence of alpha clients, completed storage
migration, explicit rollback expectations and adopter migration feedback.

## Reproduce the checks

```bash
make generate manifests
git diff --exit-code -- api config/crd config/rbac
go test ./api/... ./internal/controller/...
make test-envtest
```

The CI policy API compatibility job runs these checks. `make test-envtest` fails
if its pinned API-server assets cannot be obtained; it must not report success
by silently skipping the upgrade test.

Reference: [Kubernetes CRD versioning and storage migration](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definition-versioning/).
