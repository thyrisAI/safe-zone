# TSZ (Thyris Safe Zone) – Open Source Roadmap

This document outlines the work required to release TSZ as a production‑ready open‑source project and to grow a healthy community around it.

The roadmap is split into phases. Each bullet is a concrete, actionable item.

---

## Phase 0 – OSS Foundations

**Goal:** Make the current codebase safe and clear to open‑source.

- [ ] Choose and apply an open‑source license (recommended: Apache 2.0)
- [ ] Add `LICENSE` file and update all headers/README to reference the new license
- [ ] Add `CONTRIBUTING.md` (how to run, how to submit issues/PRs, code style)
- [ ] Add `CODE_OF_CONDUCT.md`
- [ ] Add `SECURITY.md` with vulnerability disclosure policy
- [ ] Clean secrets / private references (ensure no internal URLs, tokens, or customer data)
- [x] Create structured, enterprise‑ready documentation under `docs/`
- [x] Provide a complete Postman collection with realistic examples (`docs/TSZ_Postman_Collection.json`)

---

## Phase 1 – Core Product Hardening

**Goal:** Ensure the gateway is robust, testable and production‑ready for security‑sensitive (e.g. banking/PCI) adopters.

- [ ] Define a Phase 1 test strategy (risk‑based, bank/PCI‑ready):
  - [ ] Define test categories and entry/exit criteria (unit, integration, e2e, non‑functional, security)
  - [ ] Set minimal coverage expectations for critical flows (PII/PCI, allow/mask/block decisions)
- [ ] Add unit tests for core detection and decision logic:
  - [ ] PII detection and redaction (emails, phones, national IDs, card numbers and other PCI‑relevant fields)
  - [ ] Confidence thresholds and decision logic (allow / mask / block, including rounding and boundary conditions)
  - [ ] Validators (BUILTIN, REGEX, SCHEMA, AI_PROMPT) including negative and edge cases
  - [ ] Templates import behavior (upsert semantics, idempotency and validation errors)
  - [ ] Security event and SIEM model mapping
- [ ] Add integration tests (API + DB/Redis + AI client boundaries) for:
  - [ ] `/detect` end‑to‑end with PII / non‑PII / borderline payloads
  - [ ] LLM gateway `/v1/chat/completions` including streaming and guardrail modes
  - [ ] Templates import + detection flow using built‑in template packs
  - [ ] Allowlist/blocklist logic and pattern precedence
- [ ] Add end‑to‑end regression suites (CI‑friendly, runnable via `go test ./...` or `test-scripts/`):
  - [ ] Happy‑path flows for typical banking use cases (KYC, customer support chat, transaction memos, internal ops)
  - [ ] Misuse/abuse scenarios (prompt injection, jailbreak attempts, sensitive data exfiltration)
  - [ ] Replay known incident patterns as regression tests where applicable
- [x] Add basic benchmarks (requests per second, latency under load) (covered by `test-scripts` load test helper)
- [x] Add graceful error handling for external AI failures (timeouts, partial outages)
- [ ] Add non‑functional tests:
  - [ ] Load and stress tests for peak traffic and batch scenarios
  - [ ] Basic resilience tests (timeouts, network failures, Redis/PostgreSQL outages)
- [ ] Document performance characteristics, suggested resource sizing and the overall test strategy
- [x] Add an end‑to‑end sanity test suite (`test-scripts/`) that exercises patterns, allowlist/blocklist, validators, templates, admin APIs and the LLM gateway

---

## Phase 2 – Developer Experience & SDKs

**Goal:** Make TSZ easy to adopt from different application stacks.

- [x] Design a simple, stable public API contract (documented in `docs/API_REFERENCE.md`, including `/detect`, LLM gateway and configuration endpoints)
- [x] Create Go client helper (`tszclient-go`) for gateway and `/detect`
- [ ] Create Python client (`tsz-client`) with simple `detect()` and gateway helpers
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

**Goal:** Make TSZ easy to run and operate in production.

- [ ] Add Prometheus metrics endpoint (e.g. `/metrics`):
  - [ ] Request count / latency per endpoint
  - [ ] Blocked vs allowed requests
  - [ ] Detection counts per pattern/category
- [ ] Provide example Grafana dashboards
- [ ] Improve logging structure (JSON logs option, log levels)
- [ ] Provide production‑ready Helm chart / K8s manifests
- [x] Document backup & disaster recovery for PostgreSQL and Redis (see `docs/ARCHITECTURE_SECURITY.md`)
- [x] Add security event model and SIEM webhook integration for guardrail decisions (`internal/models/security_event.go`, `internal/guardrails/siem.go`, `SIEM_WEBHOOK_URL`)
- [ ] Document SIEM/webhook integration patterns and example dashboards

---

## Phase 6 – Security & Compliance

**Goal:** Build trust with security‑sensitive users.

- [x] Document recommended deployment patterns and network topologies (VPC/private subnets, API gateways, WAFs, mTLS, service meshes) in `docs/ARCHITECTURE_SECURITY.md`
- [ ] Provide configuration examples:
  - [ ] NGINX / Traefik / Envoy integration for TLS and auth
  - [ ] mTLS / service‑mesh deployment examples
- [ ] Perform a basic threat model and document key risks and mitigations
- [ ] (Stretch) Commission or plan for an external security review / audit

---

## Phase 7 – Community & Releases

**Goal:** Grow an active community and maintain a healthy release cycle.

- [ ] Define a versioning strategy (SemVer) and release cadence
- [ ] Set up CI/CD:
  - [ ] Linting and formatting
  - [ ] Tests and coverage reporting
  - [ ] Docker image build & publish (GitHub Container Registry / Docker Hub)
- [ ] Publish a clear `CHANGELOG.md`
- [ ] Add issue and PR templates
- [ ] Tag `good first issue` and `help wanted` items to welcome contributors
- [ ] Write a short blog post / announcement describing TSZ and its use cases

---

## Status and Next Steps

Immediate next steps recommended:

1. Decide on the open‑source license (Apache 2.0 strongly recommended for enterprise adoption).
2. Add `LICENSE`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md` and update `README.md` to reflect the new license.
3. Set up minimal CI (linting + tests + Docker build & publish) so external contributors can trust the build.
4. Add the first non‑Go SDKs and examples:
   - Python client (`tsz-client`) + FastAPI integration example
   - Node/TypeScript client + Express/Fastify integration example
5. Implement basic observability primitives:
   - `/metrics` endpoint with Prometheus counters/histograms
   - Example Grafana dashboards and SIEM documentation
