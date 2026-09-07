# TSZ (Thyris Safe Zone) – Open Source Roadmap

This document outlines the work required to release TSZ as a production‑ready open‑source project and to grow a healthy community around it.

The roadmap is split into phases. Each bullet is a concrete, actionable item.

---

## Phase 0 – OSS Foundations

**Goal:** Make the current codebase safe and clear to open‑source.

- [x] Apply the Apache License 2.0
- [x] Add `LICENSE` file and update all headers/README to reference the new license
- [x] Add `CONTRIBUTING.md` (how to run, how to submit issues/PRs, code style)
- [x] Add `CODE_OF_CONDUCT.md`
- [x] Add `SECURITY.md` with vulnerability disclosure policy
- [x] Clean secrets / private references (ensure no internal URLs, tokens, or customer data)
- [x] Create structured, enterprise‑ready documentation under `docs/`
- [x] Provide a complete Postman collection with realistic examples (`docs/TSZ_Postman_Collection.json`)

---

## Phase 1 – Core Product Hardening

**Goal:** Ensure the gateway is robust, testable and production‑ready for security‑sensitive (e.g. banking/PCI) adopters.

**Reference:** See [SECURITY_ROADMAP.md](SECURITY_ROADMAP.md) for the detailed security hardening plan, rebaselined to 17 August-25 October 2026 (10 weeks, Q3-Q4 2026).

### Subsection 1a: Functional Testing (Core Features)

- [ ] Define a Phase 1 test strategy (risk‑based, bank/PCI‑ready):
  - [ ] Define test categories and entry/exit criteria (unit, integration, e2e, non‑functional, security)
  - [ ] Set minimal coverage expectations for critical flows (PII/PCI, allow/mask/block decisions)
- [x] Add unit tests for core detection and decision logic:
  - [x] PII detection and redaction (emails, phones, national IDs, card numbers and other PCI‑relevant fields)
  - [x] Confidence thresholds and decision logic (allow / mask / block, including rounding and boundary conditions)
  - [x] Validators (BUILTIN, REGEX, SCHEMA, AI_PROMPT) including negative and edge cases)
  - [x] Templates import behavior (upsert semantics, idempotency and validation errors)
  - [x] Security event and SIEM model mapping)
- [x] Add integration tests (API + DB/Redis + AI client boundaries) for:
  - [x] `/detect` end‑to‑end with PII / non‑PII / borderline payloads)
  - [x] LLM gateway `/v1/chat/completions` including streaming and guardrail modes)
  - [x] Templates import + detection flow using built‑in template packs)
  - [ ] Allowlist/blocklist logic and pattern precedence)
  - [x] Auth-enabled integration mode coverage (Bearer token / RBAC headers)
- [x] Add end‑to‑end regression suites (CI‑friendly, runnable via `go test ./...` or `test-scripts/`):
  - [ ] Happy‑path flows for typical banking use cases (KYC, customer support chat, transaction memos, internal ops)
  - [x] Misuse/abuse scenarios (prompt injection, jailbreak attempts, sensitive data exfiltration)
  - [ ] Replay known incident patterns as regression tests where applicable)
- [x] Add basic benchmarks (requests per second, latency under load) (covered by `test-scripts` load test helper)
- [x] Add graceful error handling for external AI failures (timeouts, partial outages)
- [ ] Add non‑functional tests:
  - [ ] Load and stress tests for peak traffic and batch scenarios)
  - [ ] Basic resilience tests (timeouts, network failures, Redis/PostgreSQL outages)
- [x] Establish a standard test folder structure:
  - [x] Keep production code under `internal/...` and keep automated tests under `tests/` (unit, integration, e2e)
  - [x] Add `tests/integration/` for HTTP + DB/Redis + AI-boundary integration tests)
  - [x] Add `tests/e2e/` (and plan `tests/perf/`) for end‑to‑end and load tests)
- [x] Migrate existing scripts to the new structure:
  - [x] Convert `test-scripts/main.go` into `tests/e2e/sanity_suite_test.go` (keep script as an optional manual harness)
  - [x] Convert `test-scripts/gateway-test/main.go` into `tests/e2e/gateway_streaming_test.go` (or similar)
  - [x] Decide whether to keep additional demo scripts under `examples/` / `test-scripts/` as manual tools
- [ ] Document performance characteristics, suggested resource sizing and the overall test strategy
- [x] Add an end‑to‑end sanity test suite (initially `test-scripts/`, later `tests/e2e/`) that exercises patterns, allowlist/blocklist, validators, templates, admin APIs and the LLM gateway

### Subsection 1b: Security Hardening (HTTP, Auth, Rate Limiting, Encryption, Audit Logging)

**Status:** See [SECURITY_ROADMAP.md](SECURITY_ROADMAP.md) for full details and the rebaselined 17 August-25 October 2026 timeline.

- [x] **Milestone 1: HTTP Security Hardening** (17-30 August 2026; Weeks 1-2)
  - [x] Add request size limits (10 MB default, configurable)
  - [x] Enforce per-handler timeouts (/detect: 30s, /chat: 5m)
  - [x] Configure HTTP server with ReadTimeout, WriteTimeout, MaxHeaderBytes
  - [x] Add security headers middleware (X-Frame-Options, X-Content-Type-Options, X-XSS-Protection, HSTS, Cache-Control)
  - [x] Add CORS middleware with configurable allowed origins
  - [x] Add input validation middleware (Content-Type, JSON body validation)

- [ ] **Milestone 2: Authentication & Authorization** (24 August-13 September 2026; Weeks 2-4)
  - [x] Create `internal/auth/auth.go` with Bearer token and API key validation
  - [ ] Add `api_keys` database table with hashed tokens
  - [x] Implement authentication middleware for all non-health endpoints
  - [x] Define permission model (detect:read, gateway:use, patterns:admin, etc.)
  - [x] Implement role-based access control (RBAC) enforcement
  - [x] Protect admin endpoints (`/admin/*`) with admin role requirement
  - [ ] Create API key management endpoints (create, list, revoke, rotate)

- [ ] **Milestone 3: Rate Limiting & DDoS Protection** (7-20 September 2026; Weeks 4-5)
  - [x] Implement global rate limiter in `internal/middleware/ratelimit.go`
  - [x] Configure per-endpoint rate limits (/detect: 1000 req/min, /chat: 100 req/min, /patterns: 50 req/min, /admin: 10 req/min)
  - [ ] Store rate limit state in Redis for distributed limiting
  - [x] Return 429 Too Many Requests on limit exceeded
  - [x] Implement IP-based and key-based rate limiting

- [ ] **Milestone 4: Data Protection & Encryption** (14-27 September 2026; Weeks 5-6)
  - [ ] Make TLS/HTTPS mandatory (port 8443, TLS 1.3 minimum)
  - [ ] Implement HTTP -> HTTPS redirect on port 80
  - [ ] Configure database connections with SSL mode (sslmode=require)
  - [ ] Configure Redis with TLS
  - [ ] Implement hashing + salting for API keys
  - [ ] Encrypt sensitive fields in database (optional: per-field encryption)
  - [ ] Remove all hardcoded credentials from code and configs

- [ ] **Milestone 5: Audit Logging & Monitoring** (21 September-4 October 2026; Weeks 6-7)
  - [ ] Create `audit_logs` database table with fields for user, action, resource, IP, timestamp
  - [ ] Log authentication events (successful/failed login, suspicious patterns)
  - [ ] Log authorization events (permission granted/denied, unauthorized attempts)
  - [ ] Log data access events (CRUD operations on patterns, validators, allowlist/blocklist)
  - [ ] Implement structured JSON logging in `internal/middleware/logging.go`
  - [ ] Enhance SIEM integration to forward audit logs (new `internal/guardrails/siem.go` features)
  - [ ] Implement log retention and rotation policies

- [ ] **Milestone 6: Vulnerability Management** (28 September-11 October 2026; Weeks 7-8)
  - [ ] Add GitHub Actions workflow for `govulncheck` (golang.org/x/vuln)
  - [ ] Add `gosec` (Go security analyzer) to CI/CD
  - [ ] Add OWASP dependency-check to CI/CD pipeline
  - [ ] Enable GitHub Dependabot or Renovate for automated dependency updates
  - [ ] Add Trivy for container image scanning
  - [ ] Define security SLA for vulnerability fixes (CRITICAL: 24h, HIGH: 7d, MEDIUM: 30d)

- [ ] **Milestone 7: Production Hardening & Deployment** (5-18 October 2026; Weeks 8-9)
  - [ ] Remove default credentials from `docker-compose.yml` and `init.sql`
  - [ ] Document recommended network topology (reverse proxy, TLS termination, private subnets)
  - [ ] Run containers as non-root user
  - [ ] Set resource limits in Docker/Kubernetes (memory, CPU)
  - [ ] Add security tests in `tests/security/` (auth bypass, authz bypass, injection resistance)
  - [ ] Implement automated secret rotation (90 days for API keys/DB passwords, 30 days for certs)

- [ ] **Milestone 8: Documentation & Guidelines** (12-25 October 2026; Weeks 9-10)
  - [ ] Create `docs/SECURITY_OPERATIONS.md` (deployment, API key management, TLS setup, secrets management, monitoring)
  - [x] Update `docs/ARCHITECTURE_SECURITY.md` with auth, rate limiting, and audit logging details
  - [x] Add authentication examples to `docs/API_REFERENCE.md`
  - [ ] Create `docs/RUNBOOKS.md` with incident response procedures
  - [x] Update `CONTRIBUTING.md` with security checklist for PRs

### Q3-Q4 2026 Gateway Security Release

**Target:** 25 October 2026 (rebaselined in mid-August 2026). Deliver complete model-payload and tool-call inspection for the banking/hybrid deployment profile.

- [ ] Replace role-specific input scanning with inspection of the exact serialized payload that will reach the model:
  - [ ] Inspect text across `system`, `developer`, `user`, `assistant`, and tool-result messages according to content provenance and policy.
  - [ ] Inspect supported structured and multimodal content parts; reject unsupported or unknown content types fail-closed.
  - [ ] Apply schema and sensitive-data controls to retrieved context, attachments, auxiliary fields, and prior messages before forwarding.
  - [ ] Add regression tests proving prohibited content cannot bypass inspection by changing role, nesting, content type, or payload field.
- [ ] Add inline tool-call inspection before execution and tool-result inspection before model reuse:
  - [ ] Parse and validate `tool_calls`, tool identity, and arguments instead of treating them as opaque model output.
  - [ ] Enforce deny-by-default tool and MCP-server authorization using tenant/realm, agent, active flow, and policy scope.
  - [ ] Validate arguments against the approved tool JSON Schema; reject unknown properties, type mismatches, oversized values, and sensitive fields.
  - [ ] Run PII, secret, prompt-injection, and customer-policy inspection over tool arguments and returned content.
  - [ ] Require explicit confirmation or an external business-policy decision for configured high-impact actions.
  - [ ] Emit payload-minimized audit events for proposed, allowed, denied, failed, and completed tool calls.
- [ ] Enforce hybrid local-guard routing for semantic validation and PII confidence refinement:
  - [ ] Route any validation that can observe raw PII exclusively to a customer-controlled local guard model.
  - [ ] Prevent raw PII from being sent to cloud/external validator endpoints in the hybrid profile.
  - [ ] Make AI confidence refinement feature-gated, category-specific, and disabled by default for deterministic/high-certainty patterns.
  - [ ] Run deterministic detection and redaction before any separately approved external semantic processing.
  - [ ] Fail closed when the required local guard model is unavailable, misconfigured, or returns malformed output.
  - [ ] Add egress tests proving raw PII cannot leave the customer boundary through validation, confidence, gateway, tool, log, cache, or error paths.
- [ ] Harden customer-defined allowlists as governed policy objects:
  - [ ] Scope entries by customer/tenant, realm, environment, policy, data category, and use case; remove global cross-scope exemptions.
  - [ ] Require authenticated RBAC-protected administration and maker-checker approval for sensitive policy changes.
  - [ ] Add owner, purpose, ticket/reference, created-by, approved-by, expiry, version, and status metadata.
  - [ ] Support revocation and immediate cache invalidation without creating fail-open windows.
  - [ ] Define non-waivable categories that cannot be allowlisted, including card data and authentication secrets.
  - [ ] Record immutable before/after audit events and provide negative tests for scope leakage and overbroad exemptions.

---

## Phase 2 – Developer Experience & SDKs

**Goal:** Make TSZ easy to adopt from different application stacks.

- [x] Design a simple, stable public API contract (documented in `docs/API_REFERENCE.md`, including `/detect`, LLM gateway and configuration endpoints)
- [x] Create Go client helper (`tszclient-go`) for gateway and `/detect`
- [x] Create Python client (`tszclient-py`) with simple `detect()` and gateway helpers
- [x] Align Go/Python SDK authentication behavior with TSZ Bearer token + legacy admin-key compatibility
- [ ] Create Node/TypeScript client
- [x] Publish Go client usage documentation under `pkg/tszclient-go/README.md`
- [x] Add `examples/` directory with:
  - [x] Go `/detect` example (`examples/go-detect`)
  - [x] Go LLM gateway example (`examples/go-llm-gateway`)
  - [ ] Python FastAPI + TSZ integration
  - [ ] Node.js (Express/Fastify) + TSZ integration
  - [ ] Simple LLM proxy example (TSZ in front of OpenAI/Anthropic)
- [x] Document streaming and guardrail modes for the LLM gateway (`docs/concepts/STREAMING.md`)
- [x] Add a dedicated LLM gateway test harness (`test-scripts/gateway-test`) covering safe/unsafe, streaming and PII scenarios
- [ ] Document and implement in-code version reporting for SDKs (e.g. `tszclient-go` `Version` constant and aligning with tags)

### Bring Your Gateway (BYG) Integration Framework

**Goal:** Allow users to attach TSZ guardrails to an existing API or AI gateway without replacing its routing, authentication, rate limiting, provider management or operational tooling. Envoy Gateway is the first supported reference adapter. Envoy AI Gateway remains a compatibility track until its versioned validation matrix passes; the internal contract remains gateway-neutral.

- [x] Define and document the gateway-neutral BYG processing contract (request/response stages, actions, policy resolution, mutations, metadata, errors and adapter capabilities)
- [x] Implement the first native adapter using Envoy External Processing (`ext_proc`) and `EnvoyExtensionPolicy`
- [x] Keep Envoy/protobuf/Kubernetes-specific types outside the core guardrail engine so future gateway adapters do not require guardrail rewrites
- [x] Support request enforcement first, followed by buffered response enforcement and explicitly scoped streaming modes
- [ ] Publish and maintain an Envoy Gateway and Envoy AI Gateway compatibility matrix
- [x] Add reusable data-plane adapter contract tests for normalized request/response mapping, ownership, mutations, safe blocking and metadata
- [x] Add a cross-gateway conformance suite for request masking, blocking, response filtering, failure modes and telemetry
- [x] Build a native BYG control plane around the existing TSZ policy and audit capabilities:
  - [x] Add immutable, versioned compiled-policy snapshots backed by PostgreSQL and distributed through Redis invalidation/version notifications
  - [x] Reuse the existing detector, validators, templates, allowlist/blocklist and SIEM pipeline through a transport-neutral policy runtime
  - [x] Guarantee atomic policy activation, consistent request/response policy versions and last-known-good rollback
  - [x] Add a `TSZGuardrailPolicy` CRD following Gateway API `targetRefs`, section attachment, precedence and status conventions
  - [x] Add a compatible `TSZGuardrailPolicy` v1beta1 API, retain served v1alpha1, test storage upgrade and document rollback ([graduation record](docs/operations/TSZ_POLICY_API_UPGRADE.md))
  - [ ] Record CRD-specific adopter feedback and full-stack upgrade/rollback evidence before GA
  - [x] Add a TSZ Gateway Controller that resolves policies and reconciles owned `EnvoyExtensionPolicy` resources
  - [x] Publish `Accepted`, `ResolvedRefs`, `Programmed`, `PolicySynced`, conflict and degraded status conditions
  - [x] Add a PII-safe `io.thyris.tsz` dynamic metadata contract for Envoy access logs and telemetry
  - [x] Add adapter capability discovery so unsupported enforcement requirements are rejected rather than silently downgraded
  - [x] Add controller leader election, RBAC, readiness, metrics and version-skew handling
  - [x] Evaluate Envoy Gateway Extension Server support only as an experimental advanced profile due to xDS privilege and version-coupling risks
- [x] Update the core documentation set:
  - [x] `README.md` — add Bring Your Gateway to the feature overview and getting-started paths
  - [x] `docs/README.md` — add the BYG documentation and examples to the documentation index
  - [x] `docs/WHAT_IS_TSZ.md` — explain gateway-neutral deployment and responsibility boundaries
  - [x] `docs/PRODUCT_OVERVIEW.md` — describe BYG as a product capability and explain why users keep their existing gateway
  - [x] `docs/ARCHITECTURE_SECURITY.md` — document trust boundaries, fail-open/fail-closed behavior, data flows, network isolation and mTLS
  - [x] `docs/API_REFERENCE.md` — document processor configuration, headers, metadata, actions and error contracts
  - [x] `docs/QUICK_START.md` — add a minimal Envoy-based BYG quick start
  - [x] `docs/concepts/BRING_YOUR_GATEWAY.md` — add the gateway-neutral architecture, adapter contract and capability model
  - [x] `docs/concepts/STREAMING.md` — document BYG windowed mask/halt and strict-streaming guarantees; keep legacy async mode distinct
  - [x] `docs/integrations/README.md` — index supported gateways and their compatibility levels
  - [x] `docs/integrations/ENVOY_GATEWAY.md` — installation, configuration, verification, troubleshooting and cleanup
  - [x] `docs/integrations/ENVOY_AI_GATEWAY.md` — record deferred status, filter-order risks and the required compatibility matrix without claiming support
  - [x] `SECURITY.md` — reference the BYG threat model and private vulnerability-reporting expectations
  - [x] `CHANGELOG.md` and release notes — identify the adapter maturity level and breaking configuration changes
- [ ] Provide runnable, self-contained BYG examples with prerequisites, expected output, verification and cleanup instructions:
  - [x] Envoy Gateway minimal request inspection
  - [x] Request PII masking before the upstream receives the payload
  - [x] Request blocking with an OpenAI-compatible error
  - [x] Buffered non-streaming response masking and blocking
  - [x] Async streaming audit with an explicit leakage warning
  - [x] Windowed streaming filtering and stream halt behavior
  - [x] Route-owned policy selection that cannot be disabled by a client header
  - [x] Fail-open, fail-closed and audit-only rollouts
  - [x] Envoy JWT/API-key authentication combined with TSZ guardrails
  - [ ] Envoy local/global rate limiting combined with TSZ guardrails
  - [ ] Multi-tenant and per-route guardrail policies
  - [ ] Shared TSZ processor deployment and sidecar-style deployment
  - [x] Envoy-to-TSZ TLS/mTLS and `NetworkPolicy`
  - [x] Prometheus, OpenTelemetry and SIEM correlation
  - [ ] Envoy AI Gateway single-provider flow
  - [ ] Envoy AI Gateway multi-provider routing and fallback
  - [ ] Envoy AI Gateway token quota/rate-limit preservation
  - [ ] Chained-proxy migration example and comparison with native `ext_proc`
  - [ ] Mock gateway adapter demonstrating how to implement the BYG contract
  - [ ] Native `TSZGuardrailPolicy` attachment to Gateway, listener and HTTPRoute targets
  - [ ] Policy precedence, conflict status and unsupported-capability rejection
  - [ ] Atomic policy update, last-known-good behavior and rollback
  - [ ] Multiple processor replicas receiving the same policy snapshot version
  - [ ] Envoy access logs consuming PII-safe `io.thyris.tsz` dynamic metadata
- [ ] Require every example to be CI-verifiable, free of real credentials, version-pinned where necessary and accompanied by sample safe/unsafe requests
- [x] Select Kong Gateway + KIC provisionally as the next non-Envoy adapter based on the documented demand proxy, subject to customer-validation and compatibility gates in `docs/integrations/NEXT_GATEWAY_DECISION.md`
- [x] Evaluate Kong, APISIX, NGINX, Traefik, Istio and managed cloud adapter paths, limitations and required compatibility spikes (`docs/integrations/GATEWAY_ADAPTER_EVALUATION.md`)
- [x] Require every new gateway adapter to register a complete integration guide and runnable safe/mask/block/response/failure/telemetry example set (`examples/bring-your-gateway/adapter-releases.json`, enforced by `go test ./tests/release`)

---

## Phase 3 – Policy Packs & Templates

**Goal:** Ship valuable, ready‑made guardrail packs.

- [x] Define and document a stable template format (JSON) for patterns and validators (`/templates/import`, `docs/API_REFERENCE.md`)
- [x] Implement template import API with upsert semantics for patterns and validators (`POST /templates/import`)
- [ ] Provide built‑in template packs:
  - [ ] PII Starter Pack (emails, phones, national IDs, etc.)
  - [ ] PCI Pack (payment data focus)
  - [ ] GDPR / privacy‑focused pack
  - [ ] Toxicity & brand safety pack
  - [ ] Prompt injection & jailbreak protection pack
- [ ] Document each pack (what it covers, patterns/validators inside, recommended use cases)
- [ ] Add CLI or scripts to import/export templates easily (beyond the core HTTP API)

---

## Phase 4 – Observability & Operations

**Goal:** Make TSZ easy to run and operate in production, with full visibility into security events and system health.

- [ ] Add Prometheus metrics endpoint (e.g. `/metrics`):
  - [ ] Request count / latency per endpoint
  - [ ] Blocked vs allowed requests
  - [ ] Detection counts per pattern/category
  - [ ] Authentication and rate limiting metrics
- [ ] Provide example Grafana dashboards
- [ ] Improve logging structure (JSON logs option, log levels, centralized log forwarding)
- [ ] Provide production‑ready Helm chart / K8s manifests
- [x] Document backup & disaster recovery for PostgreSQL and Redis (see `docs/ARCHITECTURE_SECURITY.md`)
- [x] Add security event model and SIEM webhook integration for guardrail decisions (`internal/models/security_event.go`, `internal/guardrails/siem.go`, `SIEM_WEBHOOK_URL`)
- [ ] Enhance SIEM/webhook integration to include authentication, authorization, and audit events
- [ ] Document SIEM/webhook integration patterns and example dashboards
- [ ] Add alerting rules template for detection of suspicious activity (brute force, rate limit exceeding, privilege escalation)

---

## Phase 5 – Website & Content Hub

**Goal:** Provide a clear entry point for users to understand TSZ, discover policy packs/templates and access learning resources.

- [ ] Create a public website for TSZ:
  - [ ] High-level product overview and value proposition
  - [ ] Links to documentation, GitHub repo and SDKs
  - [ ] Getting started section for developers and security teams
- [ ] Build a "Policy Packs & Templates" hub:
  - [ ] List and describe available template packs (PII, PCI, GDPR, toxicity, jailbreak, etc.)
  - [ ] Provide links to JSON definitions and import instructions
  - [ ] Show versioning and changelog per pack
- [ ] Create a "Playground" or interactive demo page (optional initial version can be mock-only)
- [ ] Set up a blog/updates section:
  - [ ] Initial launch/announcement post (what TSZ is, why it exists)
  - [ ] Deep dives on policy packs (how to use them, design decisions)
  - [ ] Release highlights for major TSZ / SDK versions
- [ ] Decide on hosting strategy (e.g. GitHub Pages, Vercel, Netlify) and basic CI for website deployments

---

## Phase 6 – Security Certifications & Compliance

**Goal:** Build trust with enterprise and regulated customers through formal security certifications and compliance documentation.

**Note:** Phase 1 (Core Product Hardening) must be completed first. See [SECURITY_ROADMAP.md](SECURITY_ROADMAP.md) for the detailed security roadmap.

- [ ] Perform a formal threat model and risk assessment (document in `docs/THREAT_MODEL.md`)
- [ ] Commission or plan for an external security audit / penetration test by a third-party firm
- [x] Document recommended deployment patterns and network topologies (VPC/private subnets, API gateways, WAFs, mTLS, service meshes) in `docs/ARCHITECTURE_SECURITY.md`
- [ ] Provide configuration examples:
  - [ ] NGINX / Traefik / Envoy integration for TLS and auth
  - [ ] mTLS / service‑mesh deployment examples
  - [ ] Example Kubernetes network policies
- [ ] Build SOC2 Type II readiness:
  - [ ] Document control objectives and implementations
  - [ ] Establish audit trail and logging
  - [ ] Define SLAs for incident response and patching
- [ ] Prepare for industry certifications:
  - [ ] Plan for SOC2 Type II audit (12-month audit period)
  - [ ] Plan for FedRAMP compliance (if applicable for US government customers)
  - [ ] Consider GDPR/CCPA compliance documentation

---

## Phase 7 – Community & Releases

**Goal:** Grow an active community and maintain a healthy release cycle.

- [x] Define a versioning strategy (SemVer) and release cadence (per product: thyris-sz, tszclient-go, tszclient-py)
- [x] Set up CI/CD:
  - [x] Linting and formatting
  - [x] Tests and coverage reporting
  - [ ] Docker image build & publish (GitHub Container Registry / Docker Hub)
- [ ] Publish a clear `CHANGELOG.md`
- [ ] Add issue and PR templates
- [ ] Tag `good first issue` and `help wanted` items to welcome contributors
- [ ] Write a short blog post / announcement describing TSZ and its use cases
