# TSZ: The AI‑Powered Guardrails & Data Security Gateway

## Executive Summary

**TSZ (Thyris Safe Zone)** is an enterprise‑grade security layer designed to protect your organization's data while enabling the safe adoption of Generative AI. It sits between your applications and external systems (LLMs, APIs, third‑party services), acting as a real‑time filter that detects sensitive data, validates AI outputs and enforces strict compliance rules.

Unlike traditional regex‑only tools, TSZ leverages a **Hybrid Engine** that combines deterministic rules with **AI‑powered semantic analysis** to catch complex risks such as toxic language, financial advice, and prompt injection attacks.

TSZ is part of the **Thyris Data Zero (TDZ)** platform but can be deployed as a standalone microservice in your own environment.

---

## Key Value Proposition

TSZ allows you to:

- **Prevent data leakage** of PII, secrets and proprietary information before it reaches external systems.
- **Enforce safety and compliance policies** on AI inputs and outputs (toxicity, financial/medical advice, brand safety, etc.).
- **Standardize guardrails** across multiple applications, teams and regions.
- **Keep control of your data** by running the gateway inside your own VPC or on‑prem environment.

---

## Key Features

### 1. Intelligent PII Detection & Redaction

Protect Personal Identifiable Information (PII) with high precision.

- **Deterministic Detection:** Uses optimized regex patterns for emails, credit cards, national IDs, phone numbers, API keys and more.
- **Context‑Aware Masking:** Replaces sensitive data with secure placeholders (`[EMAIL]`, `[CREDIT_CARD]`, etc.) to maintain context for LLMs without exposing raw data.
- **Confidence Scoring & Explainability:** Each detection has an explainable confidence score, helping security teams understand why content was allowed, masked or blocked.

### 2. AI‑Powered Semantic Validation

Go beyond simple keywords. TSZ integrates with advanced LLMs to understand the **meaning** of content.

- **Toxic Language Detection:** Automatically blocks hate speech, harassment, violence and inappropriate content.
- **Domain‑Specific Checks:** Detects and blocks, for example, "Financial Advice", "Medical Diagnosis" or "Competitor Mentions" using configurable AI prompts.
- **Tone & Policy Enforcement:** Ensures AI responses align with your brand tone and internal communication standards.

### 3. Structured Data Enforcement

Ensure your AI applications communicate reliably with other systems.

- **JSON Schema Validation:** Validates that LLM outputs conform to your defined JSON schemas. If an LLM generates a string instead of an integer, TSZ blocks it before it breaks your app.
- **Format Assurance:** Guarantees valid JSON, XML or custom formats before data reaches downstream systems.

### 4. TSZ Hub (Template & Policy System)

Deploy protection in seconds instead of weeks.

- **Template Packs:** Import pre‑packaged rule sets such as "PII Protection Pack", "OWASP Top 10 for LLM", "FINRA/PCI/GDPR‑oriented compliance packs".
- **Portable Rules:** Export your custom configurations and share them across teams, environments and regions.

### 5. Multi-Provider AI Support

TSZ supports multiple AI providers, giving you flexibility in choosing your LLM backend:

- **OpenAI-Compatible Endpoints:** Works with OpenAI, Azure OpenAI, Ollama, and any OpenAI-compatible API
- **Native AWS Bedrock Integration:** Direct support for AWS Bedrock models including:
  - Anthropic Claude (Claude 3 Sonnet, Opus, Haiku)
  - Amazon Titan
  - Meta Llama
  - Mistral
  - Cohere
- **Unified Configuration:** Switch between providers with a single environment variable
- **AWS Integration Benefits:** 
  - Keep data within AWS boundaries for compliance
  - Use IAM roles and VPC endpoints
  - Leverage AWS KMS encryption
  - Benefit from AWS's security and audit capabilities

### 6. OpenAI-Compatible LLM Gateway with Streaming Guardrails

Safely connect your applications to any supported LLM provider using TSZ as a **drop‑in gateway**.

- **Input Protection:** Runs full `/detect` pipeline on user prompts before they reach the LLM (PII, secrets, toxic content, prompt injection, etc.).
- **Output Protection (Non‑Streaming):** Validates and, if necessary, redacts non‑streaming assistant responses before returning them to the client.
- **Output Protection (Streaming):** Supports multiple streaming modes for `/v1/chat/completions`:
  - **`final-only` mode:** Streams upstream tokens as‑is while still protecting the input side.
  - **`stream-sync` mode:** Applies guardrails on the **live stream**, redacting unsafe content or halting the stream on violations.
  - **`stream-async` mode:** For latency‑sensitive cases, streams raw tokens while validating the full output asynchronously for audit/SIEM.
- **OpenAI-Compatible:** Works with OpenAI SDKs by simply pointing `base_url` to TSZ; headers like `X-TSZ-Guardrails-Mode` control the streaming behaviour without any SDK changes.
- **Provider Agnostic:** The same gateway API works whether you're using OpenAI, Bedrock, or any other supported provider.

### 7. Enterprise‑Ready Platform

- **High Performance:** Built with Go and Redis for low latency and high throughput.
- **Audit Logging:** Every request is logged with a unique Request ID (RID) for full compliance traceability and SIEM integration.
- **Flexible Deployment:** Runs as a Docker container in your VPC, on‑prem or in any cloud.
- **Data Residency & Sovereignty:** All processing happens inside your perimeter; only redacted content needs to leave.

---

## Technical Architecture (High Level)

TSZ operates as a lightweight microservice that can be deployed as a sidecar, gateway or shared platform service.

1. **Input:** Your application sends text (and optional metadata) to TSZ via the `/detect` API, or an OpenAI-compatible request via `/v1/chat/completions`.
2. **Layer 1 – Fast Checks:** Regex patterns, allowlists and blocklists run immediately.
3. **Layer 2 – AI Guardrails:** Optional AI‑powered validators evaluate toxicity, policy compliance and domain‑specific rules.
4. **Layer 3 – Structure Check:** Output format (e.g. JSON schema) is validated when `expected_format` and validators are configured.
5. **Output:** TSZ returns redacted text, detection metadata, guardrail results and a `blocked` flag, or a sanitized OpenAI-compatible response.

Integration points:

- Sits between your **frontend/backend** and **LLM providers**.
- Can be called synchronously for low‑latency use cases or asynchronously in batch pipelines.
- For LLMs, can act as the **only public endpoint**, with upstream providers completely hidden behind TSZ.

For detailed architecture and security considerations, see `ARCHITECTURE_SECURITY.md`. For streaming specifics, see `concepts/STREAMING.md`.

---

## Security & Compliance

TSZ is built with a "Zero Trust" philosophy and is designed to support compliance with frameworks such as GDPR, PCI‑DSS, HIPAA and sector‑specific regulations.

Key controls:

- **Data Minimization:** Only processes data sent to the gateway; supports redaction so raw PII never leaves your environment.
- **Centralized Policy Management:** Patterns, allowlists/blocklists, validators and templates are managed via APIs and stored in PostgreSQL.
- **Audit & Forensics:** Every detection can be correlated via RID in logs and SIEM tools.
- **Defense‑in‑Depth:** Designed to complement, not replace, existing WAF, DLP and IAM solutions.

---

## Integration Overview

Integrating TSZ into an existing AI or API workflow is intentionally simple.

### Before TSZ

```python
response = openai.ChatCompletion.create(
    model="gpt-4",
    messages=[{"role": "user", "content": user_input}]
)
```

### After TSZ

```python
# 1. Detect and redact PII / unsafe content
security_check = requests.post("http://tsz-gateway:8080/detect", json={
    "text": user_input,
    "rid": request_id,
    "expected_format": "FREE_TEXT",
    "guardrails": ["TOXIC_LANGUAGE"]
})

result = security_check.json()

if result.get("blocked"):
    # Handle according to your policy (show error, request revision, etc.)
    raise SecurityError(result.get("message", "Unsafe content detected by TSZ"))

safe_text = result.get("redacted_text", user_input)

# 2. Send only redacted content to the LLM
response = openai.ChatCompletion.create(
    model="gpt-4",
    messages=[{"role": "user", "content": safe_text}]
)
```

For streaming use cases, TSZ can also sit directly in front of the OpenAI-compatible SDK:

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://tsz-gateway:8080/v1",
    api_key="dummy-key"  # Upstream key is configured on TSZ side
)

stream = client.chat.completions.create(
    model="llama3.1:8b",
    messages=[{"role": "user", "content": safe_text}],
    stream=True,
    extra_headers={
        "X-TSZ-Guardrails": "TOXIC_LANGUAGE",
        "X-TSZ-Guardrails-Mode": "stream-sync",
        "X-TSZ-Guardrails-OnFail": "filter",
    },
)

for chunk in stream:
    print(chunk.choices[0].delta.content or "", end="")
```

TSZ can also be used to post‑process LLM **outputs**, validating that responses are safe and structurally correct before they are shown to users or passed to downstream systems.

---

## Related Documentation

For a full technical view of TSZ, see the rest of the documentation set in the `docs/` directory:

- **What is TSZ?** – Conceptual overview and key capabilities: `WHAT_IS_TSZ.md`
- **Quick Start Guide** – Deploy and call `/detect` in minutes: `QUICK_START.md`
- **API Reference (Enterprise)** – Full REST API documentation: `API_REFERENCE.md`
- **Streaming Concepts** – How streaming and guardrails interact in the gateway: `concepts/STREAMING.md`
- **Architecture & Security Overview** – Architecture, data flows and security controls: `ARCHITECTURE_SECURITY.md`
- **Postman Collection** – Ready‑to‑use collection for exploring the API: `TSZ_Postman_Collection.json`

---

## Contact & Support

**Thyris.AI Team**

- **Website:** https://thyris.ai  
- **Email:** open-source@thyris.ai

For enterprise evaluations, architecture design sessions or security reviews, please reach out to our team to schedule a dedicated workshop.
