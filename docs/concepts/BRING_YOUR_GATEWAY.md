# Bring Your Gateway

TSZ can protect traffic at an existing API gateway through Envoy's external
processing (`ext_proc`) protocol. The application does not call a separate TSZ
endpoint: Envoy sends the request to `tsz-ext-proc` before it reaches the
upstream service.

## Supported integration levels

| Level | Status | Description |
| --- | --- | --- |
| Envoy Gateway preview / portable | Supported | An operator creates the policy with `tsz-policy`, applies an `EnvoyExtensionPolicy`, and supplies the trusted policy header. |
| Envoy Gateway native / managed | Supported | A `TSZGuardrailPolicy` CRD is reconciled by `tsz-controller` into the policy snapshot, Envoy attachment, and native route-to-policy binding. |
| Other Envoy-compatible gateways | Experimental | A gateway must support Envoy `ext_proc`; operators use the portable profile until a typed adapter is available. |
| Gateway-specific adapters beyond Envoy Gateway | Planned | Capability discovery and additional native attachment adapters are future work. |

The Envoy Gateway installation guide is at
[integrations/ENVOY_GATEWAY.md](../integrations/ENVOY_GATEWAY.md).

## Policy identity sources

The two supported profiles use different, trusted sources for the mandatory
policy identity:

- Preview uses exactly one `X-TSZ-Policy` header set by a gateway-owned early
  header modifier. Client-supplied values must be overwritten before ext-proc.
- Native uses trusted Envoy `ext_proc` route attributes (currently
  `xds.route_name`). The controller persists the route-to-policy association in
  the policy store; no user `HTTPRoute` mutation is needed.

`tsz-ext-proc` has one **global** resolver mode:

```text
TSZ_POLICY_RESOLUTION_MODE=header|attribute
```

`header` is the default and selects the preview resolver. `attribute` selects
the native resolver. `TSZ_POLICY_RESOLVER` is accepted only as a backwards
compatible alias.

Do not run both profiles on the same route and do not use different resolver
modes per route. A mixed setup is difficult to audit and can leave a mandatory
route without a resolvable policy identity. Migrate the entire ext-proc
deployment and its attached routes as one unit.
