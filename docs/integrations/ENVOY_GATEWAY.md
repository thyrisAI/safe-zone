# Envoy Gateway Integration

This guide covers Envoy Gateway v1.8.3 with Gateway API v1.5.1. The checked-in
Kind example under `deployments/envoy-gateway/` is the reproducible reference
environment.

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
