# Gateway integrations

This index is the source of truth for BYG adapter maturity. A gateway is
supported only when its entry is registered in
`examples/bring-your-gateway/adapter-releases.json` and the release checks,
contract suite, conformance suite, integration guide, and runnable examples
pass. Evaluation or selection does not mean support.

| Gateway | Level | Maturity | Tested version | Protocols | Request/response | Streaming | Native attachment |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Envoy Gateway | Level 2 | Preview, supported reference | 1.8.3; Gateway API 1.5.1 | OpenAI Chat Completions, Responses, Anthropic Messages, Gemini, embeddings input, MCP text payloads | Mask, block, audit-only | AsyncAudit visibility; Windowed mask and halt; not zero-leakage | `TSZGuardrailPolicy` to `EnvoyExtensionPolicy` |
| Envoy AI Gateway | Compatibility track | Deferred | Not established | Not claimed | Not claimed | Not claimed | Not claimed |
| Kong Gateway and KIC | Candidate | Selected for validation | Not established | Not claimed | Not claimed | Not claimed | Adapter not shipped |
| APISIX, NGINX, Traefik, Istio | Evaluation | Planned | Not established | Not claimed | Not claimed | Not claimed | Adapter not shipped |
| Managed cloud gateways | Evaluation | Demand-gated | Product/tier specific | Not claimed | Not claimed | Not claimed | Adapter not shipped |

Use the [Envoy Gateway guide](ENVOY_GATEWAY.md) for installation and the
[runnable example set](../../examples/bring-your-gateway/README.md) for local
verification. Envoy AI Gateway limitations and promotion criteria are recorded
in [its compatibility guide](ENVOY_AI_GATEWAY.md). Contributors must follow the
[adapter-development contract](ADAPTER_DEVELOPMENT.md).

Compatibility evidence and risk dispositions are maintained in
[Gateway Adapter Evaluation](GATEWAY_ADAPTER_EVALUATION.md). The next-adapter
selection is documented separately in [Next Gateway Decision](NEXT_GATEWAY_DECISION.md).
