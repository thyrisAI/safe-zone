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
| Envoy AI Gateway | Deferred | Not part of the Envoy Gateway-only MVP and not compatibility-tested. Provider transformations, filter ordering, token-usage metadata, routing and fallback preservation require a dedicated AI Gateway test environment. |
| Other Envoy-compatible gateways | Experimental | A gateway must support Envoy `ext_proc`; operators use the portable profile until a typed adapter is available. |
| Kong Gateway | Selected for validation | Provisional next non-Envoy adapter; no support claim exists until its demand and compatibility gates pass. |
| Other gateway-specific adapters | Planned | The native adapter registry and policy selector are available; concrete additional integrations remain Phase 7 work. |

The Envoy Gateway installation guide is at
[integrations/ENVOY_GATEWAY.md](../integrations/ENVOY_GATEWAY.md).
The native `TSZGuardrailPolicy` API is `security.thyris.ai/v1beta1`; the original
`v1alpha1` API remains served for compatibility. The
[API graduation record](../operations/TSZ_POLICY_API_UPGRADE.md) documents the
schema-preserving transition, verification evidence and open GA feedback gates.
The [Extension Server security evaluation](../security/BYG_EXTENSION_SERVER_EVALUATION.md)
records why direct xDS control-plane integration remains experimental.

Native attachments select an installed adapter with `spec.adapter`, defaulting
to `envoy-gateway`. See [native gateway adapters](../integrations/NATIVE_GATEWAY_ADAPTERS.md)
for capability rejection, compatibility, and the controller extension boundary.
Contributors adding another gateway must also follow the normative
[gateway adapter development contract](../integrations/ADAPTER_DEVELOPMENT.md).
The evidence, alternatives, and go/no-go criteria for the provisional Kong
selection are in the
[next gateway decision](../integrations/NEXT_GATEWAY_DECISION.md).
The broader
[gateway adapter evaluation](../integrations/GATEWAY_ADAPTER_EVALUATION.md)
records the technical disposition and required spikes for Kong, APISIX, NGINX,
Traefik, Istio, and managed cloud products without claiming support.

## MVP compatibility decision

The first BYG release supports **Envoy Gateway only**. Envoy AI Gateway is not
installed in the reference Kind environment and has not been tested with TSZ.
TSZ therefore makes no claim that its external processor preserves AI Gateway
provider transformations, token-usage metadata, model routing, fallback, or
filter ordering. Those properties are required acceptance tests before Envoy
AI Gateway can move from deferred to supported.

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
