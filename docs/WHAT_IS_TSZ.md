# What is TSZ (Thyris Safe Zone)?

TSZ (Thyris Safe Zone) is an **AI‑powered guardrails and data security gateway** built by **Thyris.AI**. It is designed to protect sensitive information while enabling organizations to safely adopt Generative AI, LLMs and third‑party APIs.

At its core, TSZ acts as a **zero‑trust, policy enforcement layer** that sits between your applications and external systems. Every request or response that crosses this boundary can be inspected, redacted, blocked or enriched according to your security and compliance policies.

---

## 1. Why TSZ Exists

Modern AI and API‑driven systems introduce new classes of risk:

- **PII and secrets leakage** through prompts, logs or model outputs
- **Prompt injection and jailbreak attacks** targeting LLM pipelines
- **Toxic or non‑compliant outputs** (hate speech, medical/financial advice, regulatory violations)
- **Unstructured, invalid responses** that break downstream systems (JSON that isn’t JSON, missing fields, wrong types)

Traditional security controls (WAFs, regex filters, static DLP tools) are not sufficient on their own. They either:

- Miss context‑dependent risks, or
- Generate too many false positives, or
- Cannot understand AI‑generated content.

TSZ addresses this gap with a **hybrid engine**:

1. **Deterministic rules** (high‑performance regex patterns, allowlists, blocklists)
2. **AI‑powered semantic analysis** (LLM‑backed validators and guardrails)
3. **Structured format enforcement** (JSON schema / format validation)

---

## 2. Key Capabilities

### 2.1 PII & Secrets Detection

TSZ detects and classifies sensitive entities such as:

- Email addresses, phone numbers, names
- Credit card numbers, bank details
- API keys, access tokens, secrets
- Organization‑specific or domain‑specific identifiers

Each detection receives a **confidence score** and an **explanation** describing how the score was derived (regex vs AI, thresholds, etc.).

### 2.2 Redaction & Masking

Before data leaves your environment, TSZ can **redact** sensitive values using placeholders, while preserving context for downstream systems (especially LLMs):

- `john.doe@company.com` -> `[EMAIL]`
- `4111 1111 1111 1111` -> `[CREDIT_CARD]`

This allows you to:

- Keep conversations meaningful for LLMs
- Prevent raw PII or secrets from ever reaching external providers

### 2.3 AI‑Powered Guardrails

TSZ integrates with AI models to perform semantic checks that go beyond keywords, for example:

- Toxic / abusive language
- Financial or medical advice
- Brand safety and tone of voice
- Domain‑specific safety rules (e.g. “no competitor mentions”)

Policies are expressed as **validators** that can be:

- **BUILTIN** (predefined rules)
- **REGEX** (pattern‑based)
- **SCHEMA** (JSON/XML schema validation)
- **AI_PROMPT** (LLM‑backed guardrail with a prompt)

### 2.4 Structured Response Enforcement

For AI applications that expect structured outputs (JSON, typed objects, etc.), TSZ can validate that responses conform to a pre‑defined format before they reach your application.

This prevents:

- Application crashes due to invalid JSON
- Silent failures caused by missing or wrongly typed fields

### 2.5 Templates & Reusable Policies

TSZ supports **guardrail templates** – portable bundles of patterns and validators that can be imported with a single API call. Example templates:

- PII Starter Pack
- Compliance Pack (PCI/GDPR)
- AI Safety Pack (toxicity, unsafe content)

Templates make it easy to:

- Share policies across teams and environments
- Bootstrap a new deployment within minutes

### 2.6 Runtime Security Controls

TSZ now includes runtime HTTP security controls for production hardening:

- Authentication and RBAC middleware for non-public endpoints
- Request size limits and endpoint timeouts
- Security response headers and fail-secure CORS policy
- Global and endpoint-level rate limiting

These controls are configurable via environment variables and should be enabled with least-privilege settings in production.

---

## 3. How TSZ Fits in Your Architecture

TSZ is typically deployed as a **microservice** inside your VPC or private network:

1. Your application sends incoming user input or outgoing AI responses to TSZ via the `/detect` API.
2. TSZ runs:
   - Fast pattern checks (regex, allowlist/blocklist)
   - Optional AI guardrails (toxicity, safety, domain rules)
   - Optional structure/format validation
3. TSZ returns:
   - Redacted text
   - Detection metadata and breakdown
   - Guardrail results
   - A `blocked` flag and an optional human‑readable `message`

Your application then decides how to proceed:

- If `blocked = true` -> reject the operation or ask the user to revise content.
- If `blocked = false` -> use `redacted_text` to call an LLM or external API.

### Bring Your Gateway

Organizations that already operate Envoy Gateway do not need to replace it
with TSZ. Bring Your Gateway (BYG) attaches the TSZ external processor to the
existing route. Envoy continues to own TLS, authentication, authorization,
routing, retries, quotas, rate limiting, load balancing, provider credentials,
and upstream selection. TSZ owns request/response content inspection, masking,
blocking, audit events, and PII-safe security metadata.

The portable profile uses a gateway-owned trusted policy header. The native
profile uses `TSZGuardrailPolicy`, `tsz-controller`, and owned
`EnvoyExtensionPolicy` resources. Client headers are never authoritative for a
mandatory route policy. Envoy Gateway is the supported reference integration;
Envoy AI Gateway remains deferred until its filter ordering, transformations,
routing/fallback, quota metadata, and supported versions pass compatibility
tests. See [Bring Your Gateway](concepts/BRING_YOUR_GATEWAY.md).

---

## 4. Who Uses TSZ?

TSZ is designed for organizations that:

- Handle **regulated data** (financial, healthcare, education, government)
- Require strong **data residency** and **data minimization** guarantees
- Need to integrate **LLMs securely** into existing applications
- Want to standardize guardrails across multiple teams and products

Common use cases:

- Secure prompt and response filtering for LLM chatbots
- Pre‑processing and redaction of logs or customer support tickets
- Centralized guardrail layer for multiple AI applications
- Compliance enforcement for content generation pipelines

---

## 5. Next Steps

If you are new to TSZ, we recommend the following path:

1. **Read the Quick Start** – `QUICK_START.md` to run TSZ locally with Docker.
2. **Explore the API** – import `TSZ_Postman_Collection.json` and call `/detect`.
3. **Review the Architecture & Security Overview** – `ARCHITECTURE_SECURITY.md` for a deeper technical view.
4. **Integrate with your stack** – follow `API_REFERENCE.md` for production integration details.
5. **Keep an existing Envoy Gateway** – follow `integrations/ENVOY_GATEWAY.md` and run the local Kind examples.

For a high‑level, executive‑oriented overview, see `../PRODUCT_OVERVIEW.md`.
