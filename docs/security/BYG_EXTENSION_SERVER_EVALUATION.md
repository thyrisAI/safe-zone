# BYG Envoy Gateway Extension Server Security Evaluation

## Decision

**Status: Experimental; not part of the default BYG installation.**

TSZ uses its dedicated controller to reconcile stable
`EnvoyExtensionPolicy` resources from `TSZGuardrailPolicy` objects. No
currently required BYG capability needs direct Envoy Gateway control-plane or
xDS modification. The Extension Server must not be enabled, deployed, or
required by the supported preview or native/managed profiles.

## Scope and alternatives

| Concern | TSZ controller + EnvoyExtensionPolicy | Envoy Gateway Extension Server |
| --- | --- | --- |
| Policy attachment | Reconciles supported, declarative Gateway resources | Can participate in Gateway translation and modify generated xDS |
| Required BYG features | Supports ext_proc attachment, trusted policy identity, lifecycle/status, and rollback | Provides no required capability for the current Envoy Gateway release |
| Privilege | Kubernetes RBAC scoped to TSZ policy and generated resources | Can influence privileged proxy configuration |
| Compatibility | Bound to stable public resource APIs | Coupled to translation hooks and Envoy Gateway implementation/version behavior |
| Default decision | Supported native profile | Experimental only |

The controller path remains the required baseline because it supports the
current guardrail data plane without granting an additional component authority
to alter proxy configuration.

## Security evaluation

An Extension Server compromise, defect, or incompatible release can affect
proxy confidentiality, integrity, and availability by changing generated xDS
resources. This has a larger blast radius than a failed controller
reconciliation: a failed controller update preserves the last known good
attachment, while an unsafe xDS translation change can affect every attached
proxy.

The following risks prevent default adoption:

- Privileged xDS mutation can add, remove, reorder, or redirect proxy behavior.
- Translation-hook APIs are more sensitive to Envoy Gateway release changes
  than stable Gateway API and `EnvoyExtensionPolicy` resources.
- A new networked control-plane service increases credential, supply-chain,
  availability, and denial-of-service exposure.
- Debug output or generated configuration can expose routing, identity, or
  operational metadata if not strictly redacted.
- A failure in the extension path may create an unguarded, unavailable, or
  inconsistently configured route if failure semantics are not proven.

## Experimental-use gate

An experimental Extension Server evaluation is permitted only when a concrete,
documented requirement cannot be represented through supported Gateway API or
`EnvoyExtensionPolicy` APIs. The proposal must identify the missing
capability, demonstrate that the controller path cannot satisfy it, and include
a version-pinned compatibility test.

Before any experimental deployment, require:

- dedicated ServiceAccount and least-privilege RBAC;
- isolated network policy and mTLS between Envoy Gateway and the extension
  service where traffic is required;
- an allowlist of resource types and fields the extension may affect;
- PII-safe audit logs for every generated or changed resource;
- rollback to the controller-managed last known good configuration;
- integration, upgrade, downgrade, and malformed-input tests against every
  supported Envoy Gateway version; and
- a threat-model review covering compromise, privilege escalation, xDS
  corruption, availability loss, and version skew.

## Promotion criteria

The Extension Server remains experimental until all of the following are true:

1. A product requirement proves a supported declarative API cannot satisfy the
   capability.
2. The experimental gate controls and compatibility tests above are automated.
3. The security review accepts the residual xDS privilege and operational
   risk.
4. A supported upgrade and rollback path has been tested.
5. The default controller-managed installation remains available for users who
   do not need the capability.

Until then, TSZ documentation and deployment packages must continue to use the
controller-managed `EnvoyExtensionPolicy` path.

