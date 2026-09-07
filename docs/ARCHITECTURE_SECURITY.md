# TSZ Architecture & Security Overview

This document provides a technical overview of the **architecture**, **data flows** and **security controls** of TSZ (Thyris Safe Zone).

It is intended for:

- Security architects, CISOs and compliance teams
- Platform / infrastructure engineers
- Solution architects integrating TSZ into critical systems

---

## 1. High‑Level Architecture

TSZ is a stateless **Go microservice** that exposes a REST API, backed by:

- **PostgreSQL** – configuration and metadata (patterns, validators, allowlist/blocklist, security events)
- **Redis** – low‑latency caching (AI confidence scores, pattern caches)

Typical deployment:

```text
[ Client / App ]  ->  [ TSZ Gateway ]  ->  [ LLM / External API ]
                         |    
                         +--> [ PostgreSQL ]
                         +--> [ Redis      ]
```

TSZ is usually deployed **inside your VPC / private network**, behind an API gateway or service mesh.

---

## 2. Core Components

### 2.1 AI Provider Layer

Located under `internal/ai/`, TSZ supports multiple AI providers through a unified interface:

- **Provider Abstraction** (`provider.go`): Defines the `ChatProvider` interface for AI operations
- **OpenAI-Compatible Provider** (`provider_openai.go`): Supports OpenAI, Azure OpenAI, Ollama, and any OpenAI-compatible endpoint
- **AWS Bedrock Provider** (`provider_bedrock.go`): Native integration with AWS Bedrock, supporting:
  - Anthropic Claude models
  - Amazon Titan models
  - Meta Llama models
  - Mistral models
  - Cohere models

**Provider Selection:**
- Configured via `AI_PROVIDER` environment variable
- `OPENAI_COMPATIBLE` (default): Uses HTTP client to connect to OpenAI-compatible endpoints
- `BEDROCK`: Uses AWS SDK with standard credential chain (environment variables, shared credentials, IAM roles)

**Security Benefits of Bedrock Integration:**
- Data stays within AWS boundaries
- Leverages AWS IAM for authentication and authorization
- Supports VPC endpoints for private connectivity
- Integrates with AWS KMS for encryption
- Full AWS CloudTrail audit logging

### 2.2 Detection & Guardrails Engine

Located under `internal/guardrails/`, the engine:

- Parses incoming `DetectRequest` payloads
- Applies **regex‑based patterns** (configured via `/patterns` or templates)
- Evaluates **allowlist** and **blocklist** entries
- Optionally calls **AI scorers / validators** (e.g. `TOXIC_LANGUAGE`) using the configured provider
- Computes per‑detection and overall **confidence scores**
- Decides whether the request should be **allowed, masked or blocked**

Key features:

- **Confidence thresholds** (configurable via env):
  - `CONFIDENCE_ALLOW_THRESHOLD` – under this, findings are ignored
  - `CONFIDENCE_BLOCK_THRESHOLD` – above this, content is auto‑blocked
- **Pattern‑level overrides** for enterprise policies (block/allow thresholds by pattern)
- **Rounding and explainability** (`confidence_explanation`) for auditability
- **Provider-agnostic AI validation**: Works with any configured AI provider

### 2.3 Data Model

Defined in `internal/models/`, the key types are:

- **DetectRequest / DetectResponse** – input/output of `/detect`
- **DetectionResult** – each PII or sensitive finding, with confidence and explanation
- **FormatValidator** – dynamic validators (BUILTIN, REGEX, SCHEMA, AI_PROMPT)
- **Pattern** – regex patterns with category (PII, SECRET, INJECTION, TOPIC)
- **AllowlistItem / BlacklistItem** – explicit allow/deny values
- **SecurityEvent** – internal model for security/logging use cases

### 2.4 Persistence Layer

Implemented in `internal/database/` and `internal/repository/`:

- Uses GORM for ORM mappings
- Stores:
  - Patterns
  - Validators
  - Allowlist / Blocklist
  - Guardrail templates (via imported patterns/validators)

The DB is required for full functionality in production; TSZ is designed to fail fast if DB is unavailable.

### 2.5 Caching Layer

Located in `internal/cache/`:

- **Redis** is used for:
  - AI confidence cache (24h TTL by default)
  - Pattern and allowlist/blocklist caches
- Reduces latency and external AI cost by preventing repeated scoring for the same values.

Cache invalidation is triggered when:

- Patterns/allowlist/blocklist change
- Guardrail templates are imported
- Admin triggers `/admin/reload`

---

## 3. Data Flow: Detect Request

### 3.1 Request Lifecycle

1. **Inbound HTTP**
   - Application sends a `POST /detect` request with:
     - `text`
     - Optional: `rid`, `expected_format`, `guardrails[]`

2. **Audit Context**
   - A `startTime` is recorded.
   - A `Request ID (RID)` is attached, either from the request or as `NO-RID`.

3. **Detection & Guardrails**
   - The engine runs:
     - Pattern matching (with allowlist/blocklist checks)
     - Confidence score computation and rounding
     - Guardrail validators (e.g. AI prompts, JSON schema checks)

4. **Decision & Response Construction**
   - Redacted text is generated.
   - `detections`, `validator_results`, `breakdown`, `blocked`, `contains_pii`, `overall_confidence`, `message` are filled.

5. **Audit Logging**
   - An `[AUDIT]` log entry is emitted with:
     - RID
     - Timestamp
     - Duration
     - Total detections
     - Breakdown by type

6. **Response**
   - JSON response is returned to the caller.

### 3.2 Blocking Logic

- **Pattern thresholds** and **global thresholds** (allow/block) are evaluated.
- Guardrails (validators) can force `blocked = true`.
- The final `blocked` flag is conservative: if in doubt, TSZ **blocks**.

---

### 3.3 Bring Your Gateway: Envoy Request and Response Flow

When Envoy Gateway is already the edge gateway, TSZ does not replace its
traffic-management controls. Envoy calls the internal `tsz-ext-proc` service
using Envoy External Processing (`ext_proc`) around the upstream request and
response.

```text
Client (untrusted)
  -> Envoy Gateway: TLS, authentication, authorization, rate limits and routing
  -> tsz-ext-proc: buffered request inspection
  -> Upstream / LLM provider
  -> tsz-ext-proc: buffered non-streaming response inspection
  -> Envoy Gateway
  -> Client
```

For an allowed response, Envoy forwards the original response. For a masked
response, TSZ changes only supported OpenAI, Anthropic Messages, or Gemini
GenerateContent text and structured tool-payload fields. For a blocked response,
Envoy returns a safe local `403` response
instead of the upstream body. The raw upstream response may reach Envoy and
the internal processor, but it is not released to the client before this
buffered check completes.

This guarantee applies to buffered, non-streaming OpenAI Chat Completions,
Responses API, Anthropic Messages, and Gemini GenerateContent fields documented
in the API reference, including text nested in supported multimodal content,
system instructions, assistant history, tool-call arguments, and tool results.
Provider streaming formats and inspection of multimodal image/audio/file data
remain outside the current guarantee.

Tool-call inspection validates the supported payload shape and applies content
guardrails to serialized arguments and returned text. Structured Anthropic and
Gemini tool payload masks must remain valid JSON objects. Inspection does not
authorize or execute tools, and it never changes tool identity.

Embeddings support is input-only for OpenAI-compatible `/v1/embeddings` and
`/embeddings` routes. Every string input is checked with the pinned request
policy; any blocking item blocks the entire request. Token ID inputs cannot be
inspected and produce processing errors subject to the configured failure mode.
The original request path selects this adapter and is retained across the stream,
so embedding vectors and provider error responses pass through unchanged without
being interpreted as assistant text. Response headers cannot select this bypass.
See the API reference for supported input shapes and routing requirements.

Buffered MCP Streamable HTTP JSON-RPC messages receive bidirectional content
inspection. TSZ checks `prompts/get` arguments and returned text messages, plus
`tools/call` arguments and returned text, embedded text resources and structured
content. It preserves JSON-RPC routing fields, tool identity, binary content and
annotations. This control does not authorize tool execution or select trusted
MCP servers; malformed covered content follows the route failure policy. MCP SSE
and stdio traffic are outside the Envoy adapter's enforcement boundary.

#### Trust boundaries and policy authority

- The client is untrusted. Its guardrail or policy headers never select the
  effective policy.
- Envoy is the policy-identity authority. The preview profile overwrites
  `X-TSZ-Policy`; the native profile supplies the trusted `xds.route_name`
  route attribute.
- `tsz-ext-proc` pins one compiled policy snapshot at request start and uses
  that same version for the response stage.
- TSZ owns content decisions (`ALLOW`, `MASK`, `BLOCK`, `AUDIT_ONLY`). Envoy
  owns authentication, authorization, rate limits, routing and provider
  credentials.

See [Bring Your Gateway](concepts/BRING_YOUR_GATEWAY.md) for the identity
models and [Envoy Gateway Integration](integrations/ENVOY_GATEWAY.md) for the
installation profiles.

---

## 4. Security Controls

### 4.1 Network & Deployment

- TSZ is intended to run **inside a trusted network segment**.
- External access should be mediated by:
  - API gateways (rate limiting, authentication, IP allowlists)
  - Service meshes or mTLS for east‑west traffic
  - WAFs for generic HTTP protections

### 4.2 Authentication & Authorization

- TSZ provides built-in authentication and authorization middleware.
- Protected endpoints use Bearer token auth when enabled:

  ```http
  Authorization: Bearer <token>
  ```

- Public endpoints are `/healthz` and `/ready` by default.
- Endpoint permissions are scoped (`detect:read`, `gateway:use`, `patterns:admin`, `validators:admin`, `allowlist:admin`, `blacklist:admin`, `templates:admin`, `cache:admin`).
- Admin compatibility with `X-ADMIN-KEY` remains available for legacy automation.

- For production, we strongly recommend:
  - Wrapping TSZ behind your own **identity and access management** (OAuth2, OIDC, mTLS)
  - Limiting admin API access to a small set of trusted operators and automation pipelines

### 4.3 Data Minimization & Redaction

- TSZ is built with **data minimization** in mind:
  - It only processes what is sent in the request.
  - All PII/secret handling is deterministic and observable via logs.
- When used as recommended, **raw PII should never leave your perimeter**:
  - TSZ returns **redacted_text** for downstream use.
  - External LLMs or APIs should receive only redacted content.

### 4.4 Logging & Auditability

- Every `/detect` call produces a structured log entry including:
  - Request ID (RID)
  - Detection counts and breakdown
  - Execution time
- These logs can be shipped to a **SIEM** for:
  - Monitoring data leakage attempts
  - Investigating incidents
  - Demonstrating compliance controls

### 4.5 Configuration & Policy Management

- Patterns, validators and templates are **data‑driven**:
  - No code deployment is required to change detection rules.
  - Policies can be hot‑reloaded via APIs.
- Policy changes are applied atomically and cache is invalidated to avoid inconsistent states.

### 4.6 External Processing Safety Controls

- Use `Buffered` body processing in both directions. The processor enforces
  `TSZ_MAX_BODY_BYTES` for requests and responses; oversize requests return
  `413`, while oversize upstream responses are replaced with a safe `502`.
- Request and response failure modes are separate. `closed` is the production
  default and blocks when TSZ cannot process a stage; `open` is an explicit
  availability-over-enforcement choice. A processing error is never evidence
  that content is safe.
- If Envoy invokes `ext_proc` only on the response path (for example after an
  earlier JWT or local-rate-limit rejection), TSZ has no request-pinned policy
  snapshot. Envoy Gateway v1.8.3 cannot expose a standard trusted local-reply
  marker before `ext_proc` in that filter order. TSZ uses global
  `TSZ_FAIL_MODE` for this degraded case: default `closed` returns a safe
  response-stage `403`; explicit `open` continues it. The processor emits a
  bounded metric and safe degraded audit event for observability.
- **Current dependency/readiness behavior:** `tsz-ext-proc` loads compiled
  snapshots from PostgreSQL once before it becomes ready, then retains that
  last-known-good in-memory cache if PostgreSQL or Redis becomes unavailable.
  `/readyz` intentionally does not ping those dependencies for every probe;
  `/healthz` reports only process liveness. This prevents a short network
  interruption from removing a processor that can still enforce an immutable
  snapshot. Instead, it returns `503 NOT READY` when **either** of these
  conditions holds: `TSZ_POLICY_RECONCILE_FAILURE_THRESHOLD` consecutive full
  reconcile failures occur (default `3`), **or** the last successful full
  reconcile is older than `TSZ_POLICY_MAX_STALENESS` (default `5m`). Every
  successful full reconcile resets the failure count and freshness timestamp.
  These settings are environment-configurable. The controller deliberately
  uses stricter dependency-ping readiness because it is a control-plane
  reconciler; `tsz-ext-proc` is a data-plane enforcer with a safe cached-policy
  fallback. Native route-policy resolution still queries PostgreSQL at request
  start, so it can fail closed while the header-based preview resolver
  continues using cached snapshots.
- Set Envoy `failOpen: false` as well as TSZ's closed failure policy. Either
  layer configured to bypass processing weakens the enforcement boundary.
- Restrict the ext_proc gRPC Service with Kubernetes `NetworkPolicy` so only
  Envoy workloads can reach it. Do not expose `tsz-ext-proc` or administration
  endpoints publicly.
- Use TLS or mTLS between Envoy and TSZ in production. Keep policy stores,
  Redis and administrative APIs in separate, least-privilege network paths.
- Emit only `io.thyris.tsz` safe metadata to Envoy logs and telemetry. Raw
  prompts, assistant text, detected values, credentials and validator output
  are prohibited from logs, metrics, traces, headers and dynamic metadata.

---

## 5. Operational Considerations

### 5.1 High Availability

- TSZ is stateless and can be scaled horizontally:
  - Multiple TSZ instances behind a load balancer
  - Shared PostgreSQL and Redis
- Use readiness (`/ready`) and liveness (`/healthz`) probes with your orchestrator.

### 5.2 Performance

- Written in Go with attention to low latency and high throughput.
- Performance levers:
  - Pattern complexity and number of active patterns
  - Use of AI validators (network calls, model latency)
  - Redis performance and hit rate

### 5.3 Observability

Recommended monitoring signals:

- Request rates to `/detect`
- Error rates and latencies
- DB and Redis connectivity and latency
- Cache hit/miss ratio for AI confidence scores
- Volume and type of detections (e.g. spike in CREDIT_CARD detections)

### 5.4 Backup & Disaster Recovery

- **PostgreSQL** contains critical configuration (patterns, validators, allowlists/blocklists). It should be backed up using your standard DB backup strategy.
- **Redis** is used mainly as a cache; it can be rebuilt from the database and is usually not critical for backups.

---

## 6. Hardening Recommendations

For production deployments:

1. **Network Isolation** – Run TSZ in a private subnet, reachable only from internal services and gateways.
2. **TLS Everywhere** – Terminate TLS at your gateway and use mTLS or service mesh for internal communication where appropriate.
3. **Secrets Management** – Store DB and Redis credentials in a secret manager (e.g. AWS Secrets Manager, HashiCorp Vault).
4. **Least Privilege** – TSZ’s DB user should have only necessary privileges.
5. **Admin API Protection** – Protect `/admin/*` endpoints with:
   - API gateway authentication and authorization
   - IP allowlists / network policies
   - Strong, rotated admin API keys
6. **Rate Limiting & Throttling** – Use gateway/WAF controls to prevent abuse.
7. **Compliance Alignment** – Map TSZ detections and logs to your regulatory controls (e.g. PCI, GDPR, HIPAA) as part of your broader security program.

---

## 7. References

- **What is TSZ?** – `WHAT_IS_TSZ.md`
- **Quick Start Guide** – `QUICK_START.md`
- **API Reference (Enterprise)** – `API_REFERENCE.md`
- **Product Overview (Business/Executive)** – `../PRODUCT_OVERVIEW.md`

For formal security reviews, threat modeling sessions, or architecture validation, contact **open-source@thyris.ai**.
