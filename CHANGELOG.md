# Changelog

All notable changes are recorded here. The project follows semantic versioning
for tagged releases; unreleased adapter capabilities remain subject to change.

## Unreleased

### Bring Your Gateway

- Maturity: preview. The support claim is limited to the tested Envoy Gateway
  integration described below.
- Envoy Gateway v1.8.3 is the preview reference integration, with portable
  `EnvoyExtensionPolicy` and native `TSZGuardrailPolicy` installation profiles.
- Added request/response masking and blocking, audit-only enforcement,
  bounded asynchronous SSE audit, Windowed SSE masking and halt,
  fail-open/fail-closed behavior, safe dynamic
  metadata, metrics, tracing, policy snapshots, Redis distribution, controller
  reconciliation, Kind examples, and adapter contract/conformance tests.
- Added OpenAI Chat Completions and Responses, Anthropic Messages, Gemini,
  embeddings-input, MCP, role/tool-result, and supported multimodal text-field
  scanning.
- Graduated `TSZGuardrailPolicy` storage to `v1beta1` while continuing to serve
  the compatible `v1alpha1` API. GA remains gated on adopter feedback and
  full-stack upgrade/rollback evidence.
- Envoy AI Gateway remains deferred and unsupported until its compatibility
  matrix passes. Kong + KIC is a provisional validation candidate; no Kong or
  other non-Envoy adapter is shipped.

### Compatibility notes

- BYG policies default to fail closed. Fail open requires explicit policy.
- Windowed streaming is not a zero-leakage guarantee; use buffered response
  enforcement when no unsafe byte may reach the client.
- AsyncAudit streams content unchanged before inspection and is visibility
  only; `MASK` and `BLOCK` are rejected for that mode. It is currently a
  portable-profile capability, not a change to the frozen v1alpha1/v1beta1 CRD schema.
- Direct Envoy Extension Server/xDS integration is not part of the default
  installation and remains experimental.
