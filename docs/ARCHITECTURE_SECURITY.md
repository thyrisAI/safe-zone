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
[ Client / App ]  →  [ TSZ Gateway ]  →  [ LLM / External API ]
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

## 4. Security Controls

### 4.1 Network & Deployment

- TSZ is intended to run **inside a trusted network segment**.
- External access should be mediated by:
  - API gateways (rate limiting, authentication, IP allowlists)
  - Service meshes or mTLS for east‑west traffic
  - WAFs for generic HTTP protections

### 4.2 Authentication & Authorization

- Core endpoints (`/detect`, `/patterns`, `/allowlist`, `/blacklist`, `/validators`, `/templates/import`) are usually exposed only to internal services.
- Admin endpoints under `/admin` (e.g. `/admin/patterns/policy`) support an API key header:

  ```http
  X-ADMIN-KEY: <ADMIN_API_KEY>
  ```

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
