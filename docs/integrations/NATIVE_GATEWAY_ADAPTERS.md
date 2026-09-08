# Native gateway adapter selection

Phase 6 of [BYG issue #34](https://github.com/thyrisAI/safe-zone/issues/34)
introduces an adapter boundary for declarative Gateway API attachment. The
controller shares target resolution, precedence, policy compilation, snapshot
activation and status handling across registered native adapters.

The shipped binary registers **`envoy-gateway` only**, using the existing
Envoy Gateway 1.8.3 resource implementation. This change enables additional
native adapters to be registered in controller code; it does not implement or
claim support for Kong, APISIX, NGINX, Traefik, Istio or managed cloud gateways.
Kong Gateway + KIC is the [provisional next adapter candidate](NEXT_GATEWAY_DECISION.md).
Its data-plane transport, compatibility testing, conformance suite and runnable
integration guide remain Phase 7 work; the shipped binary still registers no
Kong implementation.
See the [gateway adapter evaluation](GATEWAY_ADAPTER_EVALUATION.md) for the
candidate-specific transport, control-plane, capability, and risk assessment.

## Policy selection and compatibility

`spec.adapter` is an optional DNS-label string (1–63 characters). Its default
is `envoy-gateway`, including when a legacy object has not been API-defaulted.
Existing manifests, generated Envoy resource names, owner references, backend
configuration and trusted `xds.route_name` identities retain their behavior.

```yaml
apiVersion: security.thyris.ai/v1beta1
kind: TSZGuardrailPolicy
metadata:
  name: production-ai-policy
  namespace: ai-platform
spec:
  adapter: envoy-gateway
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: llm-api
  policySource: PostgresRef
  policyRef:
    name: banking-production
    version: 12
```

The example requires the target and immutable TSZ snapshot to exist. For a
runnable Inline example in the reference environment, follow the
[native installation guide](ENVOY_GATEWAY.md) and use the
[checkpoint policy](../../examples/bring-your-gateway/cluster/controller/checkpoint-inline-policy.yaml).

Both served API versions expose the same additive selector, preserving `None`
conversion. Install the regenerated CRD before the updated controller. The
frozen pre-graduation schema remains a compatibility-test baseline; its
existing fields are unchanged. See the [upgrade guide](../operations/TSZ_POLICY_API_UPGRADE.md).

The selector is immutable for the lifetime of the policy. To change adapters,
recreate the attachment as part of a controlled gateway migration, accounting
for the enforcement gap during deletion and recreation. This avoids orphaning
resources and runtime ownership under an in-place adapter switch. An older
controller that predates adapter selection must not manage non-Envoy policies;
its workload rollback is supported only for the default Envoy profile.

## Admission and reconciliation

The schema permits future adapter names without adding a fixed gateway enum.
A name is usable only when trusted controller code registers its implementation.
Policy authors cannot inject resource templates, advertise capabilities or
load plugins through the CRD. Unknown adapters never fall back to Envoy.

The controller checks the selected adapter's declarative-attachment support,
request inspection, requested masking and blocking, response inspection and
mutation, and streaming capabilities. It also validates target kind and
section support before compilation or native resource writes. For `PostgresRef`,
it checks the resolved immutable snapshot so actions behind a reference cannot
bypass capability validation. A Windowed `BLOCK` additionally requires the
adapter's immediate-response capability; it halts future delivery but cannot
retract bytes released from earlier windows.

Capability selection is an in-process negotiation between policy requirements
and the selected adapter's trusted, versioned descriptor. The controller first
negotiates the visible attachment spec, then repeats negotiation against the
fully resolved immutable policy definition before activation. The result names
the adapter/version and the complete ordered requirement set; a rejection lists
all missing capabilities so status is actionable. A successful result is passed
to the native adapter with the validated effective attachment settings. Because
adapters are compiled into the same controller and registry, there is no network
discovery handshake or client-supplied capability advertisement.

Adapter declarations are validated at registration. Unknown streaming modes and
impossible combinations such as body mutation without body inspection prevent
the controller from starting with that adapter. This keeps capability failures
deterministic and prevents a malformed declaration from weakening admission.

An unsupported name, target scope or enforcement requirement reports
`Accepted=False` and `Programmed=False`, reason `UnsupportedCapability`, with
the observed generation. Existing resources remain intact on this rejection;
the new generation has not been applied. Compilation failures retain the last
known good configuration. No unsupported action is converted to audit-only.

Gateway/listener/route/rule precedence and conflicts apply within an adapter.
An omitted selector and explicit `envoy-gateway` belong to the same conflict
domain. Native resources are reconciled with deterministic names and controller
owner references. Conflicts invoke only the selected adapter's owner-checked
removal; deleting the owning policy lets Kubernetes garbage-collect children.
Successful Envoy programming retains the `ExtProcConfigured` status reason.

## Controller extension boundary

[`internal/controller/nativeadapter`](../../internal/controller/nativeadapter/adapter.go)
defines the control-plane interface. An implementation supplies:

- A versioned descriptor with trusted capabilities and supported Gateway API
  target kinds/section scopes.
- Native reconciliation and owner-checked removal that preserve existing
  resources when an update fails.
- The trusted route-identity mapping used by its data-plane adapter and policy
  binding store. Identity keys must avoid collisions with other adapters sharing
  that store; never derive authority from arbitrary client headers.
- Owned resource prototypes for controller watches, resource counts for metrics,
  and the resource kind/programming reason used by status.

Register implementations at binary startup with `nativeadapter.NewRegistry`.
Duplicate names, incomplete descriptors and portable-only adapters are rejected.
Register each resource's Kubernetes scheme and add narrowly scoped RBAC with
that implementation. The controller watches only registered adapters' owned
resources; the shipped install requires no new non-Envoy CRDs or RBAC.

This boundary currently supports local Gateway API `Gateway`, `HTTPRoute` and
`GRPCRoute` targets, with each adapter declaring its supported subset. Custom
vendor target kinds and cross-namespace targets are not admitted. A future
integration needing those targets must extend target resolution, schema,
precedence and tests explicitly. Gateways without a stable declarative
attachment API should use a portable integration rather than register native
support they cannot provide.

Tests use a second, ConfigMap-backed adapter to exercise dispatch, independent
conflict domains, owned resource lifecycle, capability rejection, and failed
updates without changing the guardrail engine. This is a test fixture, not a
shipping gateway. The API-server tests cover selector defaulting, immutability,
and lossless alpha/beta upgrade behavior.

For the normative data-plane boundary, capability declaration rules, native
adapter lifecycle, testing obligations, and release checklist, see the
[gateway adapter development contract](ADAPTER_DEVELOPMENT.md).
