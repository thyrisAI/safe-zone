# Changelog

All notable changes to Thyris Safe Zone are documented in this file. Dates for
released versions follow the GitHub release publication date. Component-only
SDK and CLI tags are mentioned under the Safe Zone release that introduced
them, rather than being treated as product releases.

## 2.1.0 - 2026-09-07

### Bring Your Gateway

- Added the gateway-neutral BYG processing contract and the first supported
  adapter for Envoy Gateway v1.8.3 using Envoy External Processing (`ext_proc`).
- Added portable `EnvoyExtensionPolicy` and native, controller-managed
  `TSZGuardrailPolicy` installation profiles.
- Added deterministic Gateway API target attachment, precedence and conflict
  handling, immutable policy snapshots, atomic activation, last-known-good
  retention, rollback, and Redis-backed version distribution.
- Added request and response inspection for OpenAI Chat Completions, OpenAI
  Responses, Anthropic Messages, Gemini, embeddings input, MCP text payloads,
  selected roles, tool results, and supported multimodal text fields.
- Added request/response `ALLOW`, `MASK`, `BLOCK`, and `AUDIT_ONLY` decisions,
  with trusted route-owned policy identity and safe OpenAI-compatible block
  responses.
- Added bounded portable `AsyncAudit` SSE observation, Windowed SSE masking,
  and best-effort stream halt. Async audit never mutates delivered content;
  Windowed enforcement cannot retract bytes released from earlier windows.
- Added stage-correct request and response audit/SIEM events, PII-safe
  `io.thyris.tsz` dynamic metadata, RID/Envoy request ID/trace correlation,
  Prometheus metrics, and OpenTelemetry tracing.
- Added body, timeout, stream-buffer, concurrency, cancellation, backpressure,
  graceful-shutdown, fail-open, and fail-closed handling.
- Added Kind-based Envoy examples for inspection, masking, blocking,
  authentication, rate limiting, audit-only rollout, streaming, mTLS,
  NetworkPolicy, scaling, observability, and response enforcement.
- Added reusable adapter contract, conformance, release-document, controller,
  protocol, and clean-cluster smoke tests.

### Kubernetes API compatibility

- Graduated `TSZGuardrailPolicy` storage to `v1beta1` while continuing to serve
  the schema-compatible `v1alpha1` API.
- Kept portable `AsyncAudit` outside the frozen v1alpha1/v1beta1 CRD schema;
  native exposure requires a future API version.

### Documentation and support status

- Added the BYG concept, deployment, operations, observability,
  troubleshooting, adapter-development, compatibility, security evaluation,
  threat-model, and migration documentation set.
- Maturity: Envoy Gateway is the preview supported reference adapter.
- Deferred Envoy AI Gateway support until filter ordering, provider
  transformations, routing/fallback, authentication, token usage, quotas, and
  streaming pass a pinned compatibility matrix.
- Selected Kong Gateway with KIC as the next validation candidate without
  claiming shipped support.

### Performance validation

- Added a paired protected-versus-unprotected regex-only benchmark that checks
  BYG-added p95 latency against a configurable 20 ms target.
- A new paired result still needs to be recorded in an environment with `k6`;
  this release must not claim the target from end-to-end latency alone.

## [2.0.0] - 2026-08-14

### Added

- Added the initial Safe Zone management dashboard, including overview,
  guardrail, pattern, configuration, and event views.
- Added dashboard API/configuration models, in-process metrics storage, and
  unit coverage for dashboard metrics.
- Added the production Helm deployment package with PostgreSQL, Redis,
  service, ingress, autoscaling, disruption-budget, Secret, and ServiceAccount
  templates.
- Added deployment guidance and dashboard production notes.

### Changed

- Updated the project roadmap, security roadmap, repository documentation, and
  licensing/adoption material.
- Removed the README sponsors section.

## [1.0.0] - 2026-04-23

### Added

- Added HTTP authentication and authorization controls, protected admin cache
  reload, CORS policy, security headers, request/body limits, input validation,
  and rate limiting.
- Added environment configuration and a detailed production security-hardening
  roadmap.
- Added the first `tsz-cli` release and expanded Go client management APIs.
- Added Go and Python examples for LLM red-team testing, data-exfiltration
  testing, audit/SIEM export, LangChain, Ollama, streaming firewall, secure RAG,
  and OpenTelemetry/Jaeger.

### Fixed

- Closed SIEM webhook response bodies to prevent connection leaks.
- Corrected CLI and gateway end-to-end tests, including gateway error handling
  and test workflow stability.
- Synchronized API, provider, quick-start, streaming, Postman, SDK, and example
  documentation with the hardened runtime.

## [0.2.0] - 2025-12-27

### Added

- Added native AWS Bedrock gateway support for Anthropic Claude, Amazon Titan,
  Meta Llama, Mistral, and Cohere model families.
- Added a provider abstraction shared by OpenAI-compatible and Bedrock-backed
  gateways.
- Added IAM-based Bedrock configuration, provider documentation, a runnable Go
  Bedrock example, and provider/configuration test coverage.

### Changed

- Updated Docker, Compose, API, architecture, product, quick-start, and
  environment documentation for multi-provider operation.

## [0.1.0] - 2025-12-15

### Added

- Published the initial Safe Zone service, guardrail APIs, OpenAI-compatible
  gateway, database schema, Docker packaging, and CI/test foundation.
- Added initial Go and Python clients and runnable integration examples.
- Added the open-source project foundation, contribution documentation, and
  automated release workflow.

[2.0.0]: https://github.com/thyrisAI/safe-zone/releases/tag/thyris-sz-v2.0.0
[1.0.0]: https://github.com/thyrisAI/safe-zone/releases/tag/thyris-sz-v1.0.0
[0.2.0]: https://github.com/thyrisAI/safe-zone/releases/tag/thyris-sz-v0.2.0
[0.1.0]: https://github.com/thyrisAI/safe-zone/releases/tag/thyris-sz-v0.1.0
