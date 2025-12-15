# TSZ (Thyris Safe Zone) – Enterprise API Documentation

TSZ (Thyris Safe Zone) is an enterprise‑grade PII detection and guardrails gateway built by **Thyris.AI**. It acts as a zero‑trust middleware between your applications and external systems (LLMs, SaaS APIs, third‑party services).

This document provides a **customer‑ready**, **production‑oriented** API reference for all HTTP endpoints exposed by TSZ.

---

## 1. Base Information

**Base URL (default Docker compose):**

```text
http://localhost:8080
```

> In production you will typically expose TSZ behind an API Gateway / Ingress, such as:
>
> ```text
> https://tsz.your-company.com
> ```

**Content Type**

All JSON APIs use:

```http
Content-Type: application/json
```

**Authentication**

Most endpoints are not authenticated by default and are intended to run inside a trusted VPC / internal network. A subset of **admin** endpoints uses an API key header:

```http
X-ADMIN-KEY: <admin-api-key>
```

The key is configured via environment variable:

```env
ADMIN_API_KEY=your-secure-admin-key
```

You are strongly encouraged to **place TSZ behind your own API Gateway / mTLS / WAF** for external exposure.

---

## 2. Confidence & Guardrails Model (v2)

TSZ uses a hybrid confidence system for both PII detection and guardrail evaluations.

### 2.1 Key Concepts

- **`confidence_score`**: Final confidence between `0.00` and `1.00` (two decimal places, serialized as string).
- **`confidence_explanation`**: Explainable metadata describing how a confidence was produced (regex vs AI, thresholds, etc.).
- **Overall confidence:** `overall_confidence` on the top‑level response summarizes the risk of the entire request.
- **Thresholds (configurable via environment)**

  ```env
  CONFIDENCE_ALLOW_THRESHOLD=0.30
  CONFIDENCE_BLOCK_THRESHOLD=0.85
  ```

  - `< 0.30`  → **ALLOW** (ignored)
  - `0.30 – 0.85` → **MASK** (redact in output)
  - `≥ 0.85` → **AUTO‑BLOCK**

- **AI Confidence Cache:**
  - AI scoring is cached in Redis (TTL 24h) for performance and cost efficiency.
  - Cache key is derived from pattern and value to guarantee idempotent behaviour.

### 2.2 Decision Logic Summary

| Confidence        | Action   |
|-------------------|----------|
| `< 0.30`          | Ignore   |
| `0.30 – 0.85`     | Mask     |
| `≥ 0.85`          | Block    |

> Guardrails and explicit **BLOCK** rules always override generic thresholds.

---

## 3. Core Detection API

### 3.1 Detect PII and Sensitive Data

**Endpoint**

```http
POST /detect
```

This is the **primary production endpoint**. It performs:

- PII & secrets detection via hybrid engine (regex + AI)
- Redaction (masking) of sensitive entities
- Optional guardrail evaluation (AI‑based validators)
- Optional expected format validation (JSON schema / format guardrails)

#### 3.1.1 Request Body

```json
{
  "text": "string (required)",
  "rid": "string (optional)",
  "expected_format": "string (optional)",
  "guardrails": ["string" (optional ...)]
}
```

Field details:

- `text` (**required**): Raw text to be analyzed (user input, LLM output, log line, etc.).
- `rid` (optional): **Request ID** for audit log correlation. If omitted, `NO-RID` will be used in logs.
- `expected_format` (optional): A symbolic identifier for the expected output format of your application (e.g. a JSON schema name). Depending on your validators configuration, this can trigger schema / format validations.
- `guardrails` (optional): Array of **validator names** to execute in addition to standard PII detection, e.g. `"TOXIC_LANGUAGE"`.

#### 3.1.2 Response Body

```json
{
  "redacted_text": "My email is [EMAIL]",
  "detections": [
    {
      "type": "EMAIL",
      "value": "user@company.com",
      "placeholder": "[EMAIL]",
      "start": 11,
      "end": 27,
      "confidence_score": "0.78",
      "confidence_explanation": {
        "source": "HYBRID",
        "regex_score": "0.55",
        "ai_score": "0.90",
        "category": "PII",
        "pattern_active": true,
        "final_score": "0.78"
      }
    }
  ],
  "validator_results": [
    {
      "name": "TOXIC_LANGUAGE",
      "type": "AI_PROMPT",
      "passed": false,
      "confidence_score": "0.92"
    }
  ],
  "breakdown": {
    "EMAIL": 1
  },
  "blocked": false,
  "contains_pii": true,
  "overall_confidence": "0.81",
  "message": "string (optional; contains blocking reason, if any)"
}
```

Top‑level fields:

- `redacted_text`: The input `text` with detected entities replaced with placeholders (e.g. `[EMAIL]`). Omitted if nothing is redacted.
- `detections`: Array of **DetectionResult** objects (see below).
- `validator_results`: Array of **ValidatorResult** objects for any executed guardrails.
- `breakdown`: Map of detection type → count. Example: `{ "EMAIL": 2, "PHONE_NUMBER": 1 }`.
- `blocked`: Boolean flag indicating whether TSZ considers this request **unsafe**. If `true`, you should treat this as a hard block.
- `contains_pii`: `true` if any PII or sensitive entity was detected.
- `overall_confidence`: Confidence score for the overall risk.
- `message`: Optional human‑readable summary for block/allow decisions.

Detection object:

```json
{
  "type": "EMAIL",
  "value": "user@company.com",
  "placeholder": "[EMAIL]",
  "start": 13,
  "end": 29,
  "confidence_score": "0.78",
  "confidence_explanation": {
    "source": "HYBRID",
    "regex_score": "0.55",
    "ai_score": "0.90",
    "category": "PII",
    "pattern_active": true,
    "final_score": "0.78"
  }
}
```

Validator result object:

```json
{
  "name": "TOXIC_LANGUAGE",
  "type": "AI_PROMPT",
  "passed": false,
  "confidence_score": "0.92"
}
```

#### 3.1.3 Blocking Behaviour Examples

- If a detection exceeds the **block threshold** (default `0.85`):

  ```json
  {
    "blocked": true,
    "message": "Blocked due to high confidence detection: CREDIT_CARD",
    "overall_confidence": "0.93",
    "contains_pii": true
  }
  ```

- If toxic language is detected by an AI validator (e.g. `TOXIC_LANGUAGE`) with high confidence, `blocked` will also be `true`, and the message will reflect guardrail failure.

#### 3.1.4 Typical Integration Pattern

Example: protect an LLM API call in Python.

```python
import requests

TSZ_URL = "https://tsz.your-company.com/detect"

security_check = requests.post(TSZ_URL, json={
    "text": user_input,
    "rid": request_id,
    "guardrails": ["TOXIC_LANGUAGE"]
})

result = security_check.json()

if result.get("blocked"):
    raise SecurityError(result.get("message", "Unsafe content detected by TSZ"))

safe_text = result.get("redacted_text", user_input)
# send safe_text to your LLM provider
```

---

### 3.2 OpenAI-Compatible LLM Gateway (Chat Completions)

TSZ can also act as an **OpenAI-compatible gateway** for chat models. This allows you to point existing OpenAI SDKs to TSZ instead of directly to OpenAI or another provider.

**Endpoint**

```http
POST /v1/chat/completions
```

TSZ implements the **request and response shape** of the OpenAI `chat/completions` endpoint for both non‑streaming (`stream=false`) and streaming (`stream=true`) calls.

#### 3.2.1 High-Level Behaviour

1. Client sends an OpenAI‑style chat completion request to TSZ:
   - `model`: any model name (forwarded as‑is to upstream)
   - `messages`: array of chat messages
   - `stream`: `false` (standard JSON response) or `true` (SSE streaming)
2. TSZ runs `/detect` logic on **user messages** (`role == "user"`) before calling the LLM:
   - PII & secret detection
   - Guardrails / validators (e.g. `TOXIC_LANGUAGE`)
3. If unsafe on input:
   - TSZ **blocks** the request and returns an OpenAI‑compatible error response.
4. If safe on input:
   - TSZ **redacts** sensitive content in user messages and forwards the sanitized request to the upstream LLM service.
5. For non‑streaming responses (`stream=false`):
   - TSZ runs `/detect` on the assistant output (using the same guardrails).
   - If unsafe, TSZ returns an OpenAI‑compatible error and does not forward the raw LLM response.
   - If safe, TSZ may redact the assistant content before returning it to the client.
6. For streaming responses (`stream=true`):
   - TSZ proxies the upstream SSE stream, with behaviour controlled by gateway headers (see below):
     - **`final-only` mode:** TSZ forwards the raw stream as‑is (input-only guardrails).
     - **`stream-sync` mode:** TSZ applies guardrails **while streaming** and only sends sanitized content.
     - **`stream-async` mode:** TSZ forwards raw stream to the client and validates asynchronously for logging/SIEM.

#### 3.2.2 Configuration

Upstream LLM service is configured via environment variables (see `internal/config/config.go`):

```env
THYRIS_AI_MODEL_URL=https://api.openai.com/v1
THYRIS_AI_API_KEY=sk-...your-openai-key...
THYRIS_AI_MODEL=llama3.1:8b
```

- `THYRIS_AI_MODEL_URL`: Base URL of an OpenAI‑compatible API (OpenAI, Azure OpenAI, Ollama, etc.). TSZ appends `/chat/completions`.
- `THYRIS_AI_API_KEY`: API key for the upstream service (sent as `Authorization: Bearer <key>`).
- `THYRIS_AI_MODEL`: Default model name used by internal AI validators; the gateway itself forwards the `model` field from the incoming request.

#### 3.2.3 Headers

TSZ gateway supports additional headers for observability and guardrails:

- `X-TSZ-RID` (optional):
  - Custom Request ID used for audit logs and correlation.
  - If omitted, TSZ generates a value such as `LLM-GW-20251213T030000.000`.

- `X-TSZ-Guardrails` (optional):
  - Comma‑separated list of validator names to apply, for example:
    ```http
    X-TSZ-Guardrails: TOXIC_LANGUAGE,ORDER_JSON_V1
    ```
  - These values are passed into `DetectRequest.guardrails`.

- `X-TSZ-Guardrails-Mode` (optional, streaming only):

  Controls how TSZ applies guardrails to **streaming** responses (`stream=true`). If omitted, defaults to `final-only`.

  | Value          | Description                                                                                 |
  |----------------|---------------------------------------------------------------------------------------------|
  | `final-only`   | Default. Input guardrails + non‑stream output guardrails only; streaming output is proxied as‑is. |
  | `stream-sync`  | Apply guardrails while streaming. Client receives only sanitized output; stream may be halted on severe violations. |
  | `stream-async` | Forward raw streaming response to the client, but validate the full stream asynchronously for logging/SIEM. |

- `X-TSZ-Guardrails-OnFail` (optional, streaming only):

  Controls what happens when **output** guardrails detect a violation in streaming mode. If omitted, defaults to `filter`.

  | Value      | Description                                                                                         |
  |------------|-----------------------------------------------------------------------------------------------------|
  | `filter`   | Redact unsafe parts (PII, toxic segments) and continue streaming sanitized content.                 |
  | `halt`     | Stop streaming early and send an OpenAI‑style error event (followed by a `data: [DONE]` marker).   |

> Non‑streaming requests (`stream=false`) ignore `X-TSZ-Guardrails-Mode` and always apply output guardrails over the full assistant response.

#### 3.2.4 Request Examples

**Non‑streaming with input/output guardrails**

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-TSZ-RID: RID-GW-001" \
  -H "X-TSZ-Guardrails: TOXIC_LANGUAGE" \
  -d '{
    "model": "llama3.1:8b",
    "messages": [
      {"role": "user", "content": "My credit card is 4111 1111 1111 1111, you are an idiot"}
    ],
    "stream": false
  }'
```

Behaviour:

- TSZ detects both PII (credit card) and toxic language on the user message.
- Depending on configured thresholds and validators:
  - The request may be blocked, returning an OpenAI‑style error:

    ```json
    {
      "error": {
        "message": "Blocked due to high confidence detection: CREDIT_CARD",
        "type": "invalid_request_error",
        "param": null,
        "code": "tsz_content_blocked"
      }
    }
    ```

  - Or TSZ may redact the card number and forward a sanitized prompt to the upstream model.

**Streaming without guardrails (baseline)**

```bash
curl -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-TSZ-RID: RID-GW-STREAM-BASE" \
  -d '{
    "model": "llama3.1:8b",
    "messages": [
      {"role": "user", "content": "Stream a short response about TSZ gateway"}
    ],
    "stream": true
  }'
```

- With no `X-TSZ-Guardrails-Mode` header, TSZ defaults to `final-only` and proxies the upstream SSE stream as‑is.

**Streaming with synchronous guardrails (sanitized output)**

```bash
curl -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-TSZ-RID: RID-GW-STREAM-FILTER" \
  -H "X-TSZ-Guardrails: TOXIC_LANGUAGE,PII" \
  -H "X-TSZ-Guardrails-Mode: stream-sync" \
  -H "X-TSZ-Guardrails-OnFail: filter" \
  -d '{
    "model": "llama3.1:8b",
    "messages": [
      {"role": "user", "content": "Please stream a short answer that includes an insult and a fake credit card number like 4111 1111 1111 1111."}
    ],
    "stream": true
  }'
```

- TSZ accumulates the assistant output, applies guardrails on the growing text, and only streams **sanitized** content to the client.
- Unsafe portions may be replaced with placeholders or masked tokens (implementation‑dependent).

**Streaming with synchronous guardrails (halt on violation)**

```bash
curl -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-TSZ-RID: RID-GW-STREAM-HALT" \
  -H "X-TSZ-Guardrails: TOXIC_LANGUAGE,PII" \
  -H "X-TSZ-Guardrails-Mode: stream-sync" \
  -H "X-TSZ-Guardrails-OnFail: halt" \
  -d '{
    "model": "llama3.1:8b",
    "messages": [
      {"role": "user", "content": "Stream a response that is clearly toxic and unsafe."}
    ],
    "stream": true
  }'
```

- On a high‑confidence violation, TSZ stops streaming and sends an SSE error payload followed by `data: [DONE]`.

**Streaming with asynchronous validation**

```bash
curl -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-TSZ-RID: RID-GW-STREAM-ASYNC" \
  -H "X-TSZ-Guardrails: TOXIC_LANGUAGE,PII" \
  -H "X-TSZ-Guardrails-Mode: stream-async" \
  -d '{
    "model": "llama3.1:8b",
    "messages": [
      {"role": "user", "content": "Stream a long response that might contain sensitive content."}
    ],
    "stream": true
  }'
```

- TSZ forwards the raw stream directly to the client.
- In the background, TSZ runs detection/guardrails on the full streamed output and emits security events (e.g. to SIEM) using the same `RID`.

#### 3.2.5 Using With OpenAI SDK (Python)

You can configure the OpenAI Python SDK to use TSZ as a drop‑in gateway by changing the `base_url`:

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",  # TSZ gateway
    api_key="dummy-key"  # TSZ does not use this; upstream key comes from env
)

# Non-streaming example
resp = client.chat.completions.create(
    model="llama3.1:8b",
    messages=[{"role": "user", "content": "Hello, this is safe text"}],
)

print(resp.choices[0].message.content)

# Streaming example with guardrails
stream = client.chat.completions.create(
    model="llama3.1:8b",
    messages=[{"role": "user", "content": "Stream something potentially unsafe"}],
    stream=True,
    extra_headers={
        "X-TSZ-Guardrails": "TOXIC_LANGUAGE,PII",
        "X-TSZ-Guardrails-Mode": "stream-sync",
        "X-TSZ-Guardrails-OnFail": "filter",
    },
)

for chunk in stream:
    print(chunk.choices[0].delta.content or "", end="")
```

TSZ will:

- Inspect and redact the user content.
- Forward the sanitized request to the configured upstream LLM service.
- For non‑streaming calls, apply output guardrails to the full assistant message before returning.
- For streaming calls, behave according to the chosen `X-TSZ-Guardrails-Mode` and `X-TSZ-Guardrails-OnFail`.

Current limitations:

- Only `role == "user"` messages are scanned and redacted on input (system/assistant messages are left as‑is).
- Streaming support is focused on **textual content** in `choices[].delta.content`.

#### 3.2.6 Gateway Metadata (`tsz_meta`)

For non‑streaming calls (`stream=false`) and error responses, the gateway attaches additional metadata under a
`tsz_meta` field in the OpenAI‑compatible response body. This allows you to see the **same rich detection
information as `/detect`**, alongside the LLM result.

Example successful response (simplified):

```jsonc
{
  "id": "chatcmpl-58",
  "object": "chat.completion",
  "model": "llama3.1:8b",
  "choices": [
    {
      "index": 0,
      "finish_reason": "stop",
      "message": {
        "role": "assistant",
        "content": "I cannot provide information that would help you identify your email account password."
      }
    }
  ],
  "tsz_meta": {
    "rid": "RID-GW-001",
    "guardrails": ["TOXIC_LANGUAGE"],
    "input": [
      // Array of DetectResponse for each user message
    ],
    "output": [
      // Array of DetectResponse for each assistant message (non-streaming)
    ]
  }
}
```

The `input` and `output` arrays contain objects with the exact same shape as `/detect`’in `DetectResponse` modeli:

```jsonc
{
  "redacted_text": "My email is [RID-GW-001_EMAIL_xxx] what is my email domain",
  "detections": [
    {
      "type": "EMAIL",
      "value": "test@gmail.com",
      "placeholder": "[RID-GW-001_EMAIL_xxx]",
      "start": 12,
      "end": 26,
      "confidence_score": "0.78",
      "confidence_explanation": {
        "source": "HYBRID",
        "regex_score": "0.60",
        "ai_score": "0.95",
        "category": "PII",
        "pattern_active": true,
        "final_score": "0.78"
      }
    }
  ],
  "validator_results": [
    {
      "name": "TOXIC_LANGUAGE",
      "type": "VALIDATOR",
      "passed": true,
      "confidence_score": "0.70"
    }
  ],
  "breakdown": {
    "EMAIL": 1
  },
  "blocked": false,
  "contains_pii": true,
  "overall_confidence": "0.73"
}
```

In addition, two environment variables control the gateway behaviour:

- `PII_MODE` (core detection engine)
  - `MASK` (default): When PII is detected, `redacted_text` is produced; blocking is decided based on confidence thresholds and guardrail rules.
  - `BLOCK`: When PII is present and certain thresholds are exceeded, `DetectResponse.blocked = true` and the `message` field explains the reason.

- `GATEWAY_BLOCK_MODE` (HTTP response)
  - `BLOCK` (default): If any input/output `DetectResponse.blocked == true`, the gateway returns an HTTP 4xx with an OpenAI‑style `error` object.
  - `MASK`: HTTP 200, the LLM response is returned; problematic segments are masked and you can inspect `tsz_meta.*[].blocked` to see the status.
  - `WARN`: Behaviour is the same as `MASK`, but intended to be interpreted as a soft warning by the client.

This allows you to keep full `/detect`‑style scoring and guardrail results while controlling the gateway’s HTTP‑level
policy via configuration.

---

## 4. Pattern Management API

Patterns represent **regex‑based detection rules** for PII, secrets, or other structured signals.

### 4.1 Create Pattern

**Endpoint**

```http
POST /patterns
```

**Request Body** (JSON)

```json
{
  "Name": "PHONE_NUMBER",
  "Regex": "\\+?[0-9]{10,13}",
  "Description": "International phone numbers",
  "Category": "PII",
  "IsActive": true,
  "BlockThreshold": 0.9,
  "AllowThreshold": 0.2
}
```

Field notes (backed by `models.Pattern`):

- `Name` (**required**, unique): Logical identifier.
- `Regex` (**required`**): Go‑compatible regular expression.
- `Description` (optional): Human readable description.
- `Category` (optional, default `"PII"`): e.g. `PII`, `SECRET`, `INJECTION`, `TOPIC`.
- `IsActive` (optional, default `true`): Whether rule is active.
- `BlockThreshold` / `AllowThreshold` (optional): Pattern‑level threshold overrides for enterprise policies.

**Responses**

- `201 Created` with the created Pattern object.
- `400 Bad Request` if JSON is invalid.
- `500 Internal Server Error` if DB operation fails.

### 4.2 List Patterns

**Endpoint**

```http
GET /patterns
```

**Response 200**

```json
[
  {
    "ID": 1,
    "Name": "EMAIL",
    "Regex": "[a-z0-9._%+-]+@[a-z0-9.-]+\\.[a-z]{2,}",
    "Description": "Standard email address",
    "Category": "PII",
    "IsActive": true,
    "BlockThreshold": 0.9,
    "AllowThreshold": 0.2,
    "CreatedAt": "2025-01-01T12:00:00Z",
    "UpdatedAt": "2025-01-01T12:00:00Z"
  }
]
```

### 4.3 Delete Pattern

**Endpoint**

```http
DELETE /patterns/{id}
```

Path parameters:

- `id` (integer, required): Pattern primary key.

**Responses**

- `204 No Content` on success.
- `400 Bad Request` if `id` is invalid.
- `500 Internal Server Error` if DB operation fails.

> All pattern operations automatically clear the patterns cache so changes are applied in real time.

---

## 5. Allowlist Management API

Allowlist items represent **trusted values** that should be ignored during detection.

### 5.1 Create Allowlist Item

**Endpoint**

```http
POST /allowlist
```

**Request Body**

```json
{
  "value": "support@company.com",
  "description": "Official support mailbox"
}
```

### 5.2 List Allowlist Items

**Endpoint**

```http
GET /allowlist
```

**Response 200**

```json
[
  {
    "ID": 1,
    "value": "support@company.com",
    "description": "Official support mailbox"
  }
]
```

### 5.3 Delete Allowlist Item

**Endpoint**

```http
DELETE /allowlist/{id}
```

Path parameters:

- `id` (integer, required)

**Responses**

- `204 No Content` on success.
- `400 Bad Request` if `id` is invalid.

> All allowlist operations clear the allowlist cache to ensure immediate effect.

---

## 6. Blocklist Management API

Blocklist (blacklist) items represent **explicitly forbidden values** that should be hard‑blocked.

### 6.1 Create Blocklist Item

**Endpoint**

```http
POST /blacklist
```

**Request Body**

```json
{
  "value": "confidential_keyword",
  "description": "Internal classified term"
}
```

### 6.2 List Blocklist Items

**Endpoint**

```http
GET /blacklist
```

**Response 200**

```json
[
  {
    "ID": 1,
    "value": "confidential_keyword",
    "description": "Internal classified term"
  }
]
```

### 6.3 Delete Blocklist Item

**Endpoint**

```http
DELETE /blacklist/{id}
```

Path parameters:

- `id` (integer, required)

**Responses**

- `204 No Content` on success.
- `400 Bad Request` if `id` is invalid.

> All blocklist operations clear the blocklist cache to ensure immediate enforcement.

---

## 7. Format Validators & Guardrails API

Format validators define **dynamic validation rules** (including AI‑powered guardrails) that can be invoked via the `/detect` endpoint.

### 7.1 Validator Model

Backed by `models.FormatValidator`:

```go
type FormatValidator struct {
    Name             string `json:"name"`
    Type             string `json:"type"` // BUILTIN, REGEX, SCHEMA, AI_PROMPT
    Rule             string `json:"rule"` // Regex, prompt text, or JSON Schema
    Description      string `json:"description"`
    ExpectedResponse string `json:"expected_response"` // e.g. "YES", "SAFE", "1"
}
```

### 7.2 Create Validator

**Endpoint**

```http
POST /validators
```

**Request Body**

```json
{
  "name": "TOXIC_LANGUAGE",
  "type": "AI_PROMPT",
  "rule": "Is this text toxic or abusive? Answer YES or NO.",
  "description": "Blocks abusive language",
  "expected_response": "NO"
}
```

**Responses**

- `201 Created` with the created validator.
- `400 Bad Request` if body is invalid.
- `500 Internal Server Error` on persistence error.

### 7.3 List Validators

**Endpoint**

```http
GET /validators
```

**Response 200**

```json
[
  {
    "name": "TOXIC_LANGUAGE",
    "type": "AI_PROMPT",
    "rule": "Is this text toxic or abusive? Answer YES or NO.",
    "description": "Blocks abusive language",
    "expected_response": "NO"
  }
]
```

### 7.4 Delete Validator

**Endpoint**

```http
DELETE /validators/{id}
```

Path parameters:

- `id` (integer, required; internal numeric ID)

**Responses**

- `204 No Content` on success.
- `400 Bad Request` if `id` is invalid.
- `500 Internal Server Error` on delete failure.

---

## 8. Guardrail Templates API

Guardrail templates are **portable collections** of patterns and validators, enabling you to roll out complex policies with a single import.

### 8.1 Import Template

**Endpoint**

```http
POST /templates/import
```

**Request Body**

```json
{
  "template": {
    "name": "PII Starter Pack",
    "description": "Detects basic PII and blocks abusive language",
    "patterns": [
      {
        "Name": "EMAIL",
        "Regex": "[a-z0-9._%+-]+@[a-z0-9.-]+\\.[a-z]{2,}",
        "Category": "PII",
        "IsActive": true
      }
    ],
    "validators": [
      {
        "name": "TOXIC_LANGUAGE",
        "type": "AI_PROMPT",
        "rule": "Is this text toxic or abusive? Reply YES or NO"
      }
    ]
  }
}
```

Semantics:

- If a pattern / validator with the same `Name` / `name` already exists, it will be **updated**.
- Otherwise, it will be **inserted**.
- The whole operation runs in a transaction; on failure, no partial state is left.

**Response 200**

```json
{
  "message": "Template imported successfully",
  "name": "PII Starter Pack"
}
```

---

## 9. Admin & System APIs

### 9.1 Health Check

**Endpoint**

```http
GET /healthz
```

**Description**

Basic liveness probe. Returns `UP` when the HTTP server is reachable.

**Response 200 (text/plain)**

```text
UP
```

### 9.2 Readiness Check

**Endpoint**

```http
GET /ready
```

**Description**

Readiness probe used by orchestrators to ensure TSZ is ready to serve traffic.

Checks:

- PostgreSQL connectivity (`Ping()`)
- Redis connectivity (`PING`)

**Responses**

- `200 OK` with body `READY` when both DB and Redis are reachable.
- `503 Service Unavailable` with a short error message if any dependency is not ready.

### 9.3 Reload Cache

**Endpoint**

```http
POST /admin/reload
```

**Description**

Manually clears in‑memory / Redis‑backed caches so that changes in the database are reflected immediately.

Current behaviour (subject to extension):

- Clears pattern cache
- Clears allowlist cache

**Responses**

- `200 OK` with an empty body on success.
- `405 Method Not Allowed` if called with a non‑POST method.

### 9.4 Update Pattern Policy (Admin)

**Endpoint**

```http
POST /admin/patterns/policy
```

**Authentication**

Requires a valid admin API key header:

```http
X-ADMIN-KEY: <ADMIN_API_KEY>
```

**Request Body**

```json
{
  "pattern_id": 1,
  "block_threshold": 0.9,
  "allow_threshold": 0.2
}
```

**Responses**

- `200 OK` with updated pattern info
- `400 Bad Request` if JSON is invalid
- `401 Unauthorized` if admin key is missing/invalid
- `404 Not Found` if pattern does not exist
- `500 Internal Server Error` on persistence error

---

## 10. Data Model Reference

### 10.1 DetectRequest

```json
{
  "text": "string",
  "rid": "string",
  "expected_format": "string",
  "guardrails": ["string"]
}
```

### 10.2 DetectResponse

```json
{
  "redacted_text": "string",
  "detections": [<DetectionResult>],
  "validator_results": [<ValidatorResult>],
  "breakdown": {"string": 0},
  "blocked": false,
  "contains_pii": true,
  "overall_confidence": "0.00",
  "message": "string"
}
```

### 10.3 DetectionResult

```json
{
  "type": "string",
  "value": "string",
  "placeholder": "string",
  "start": 0,
  "end": 0,
  "confidence_score": "0.00",
  "confidence_explanation": { /* see below */ }
}
```

### 10.4 ConfidenceExplanation

Backed by `models.ConfidenceExplanation` and `models.Confidence` (custom JSON marshalling to 2 decimals).

Example structure as exposed by the current implementation:

```json
{
  "source": "HYBRID",         
  "regex_score": "0.55",      
  "ai_score": "0.90",         
  "category": "PII",          
  "pattern_active": true,      
  "final_score": "0.78"       
}
```

### 10.5 ValidatorResult

```json
{
  "name": "string",
  "type": "string",
  "passed": true,
  "confidence_score": "0.00"
}
```

### 10.6 Pattern

```json
{
  "ID": 1,
  "Name": "string",
  "Regex": "string",
  "Description": "string",
  "Category": "PII",
  "IsActive": true,
  "BlockThreshold": 0.9,
  "AllowThreshold": 0.2,
  "CreatedAt": "2025-01-01T12:00:00Z",
  "UpdatedAt": "2025-01-01T12:00:00Z"
}
```

### 10.7 FormatValidator

```json
{
  "ID": 1,
  "name": "string",
  "type": "BUILTIN | REGEX | SCHEMA | AI_PROMPT",
  "rule": "string",
  "description": "string",
  "expected_response": "string"
}
```

### 10.8 AllowlistItem

```json
{
  "ID": 1,
  "value": "string",
  "description": "string"
}
```

### 10.9 BlacklistItem

```json
{
  "ID": 1,
  "value": "string",
  "description": "string"
}
```

---

## 11. Operational & Compliance Notes

- **Logging & Auditability**
  - Every `/detect` call produces an audit log entry with: `Request ID (RID)`, timestamp, execution duration, total detections and per‑type breakdown.
  - Use `rid` to correlate TSZ events with upstream application logs and SIEM.

- **Performance**
  - Built with Go and leveraging Redis caching for AI confidence scores.
  - Safe to use synchronously in latency‑sensitive paths; still recommended to benchmark in your environment.

- **Deployment**
  - Typically deployed as a Docker container alongside your application stack (Kubernetes, ECS, on‑premise, etc.).
  - Use readiness (`/ready`) and liveness (`/healthz`) endpoints for orchestrator probes.

- **Security**
  - Run TSZ inside a private network segment.
  - Protect admin endpoints (`/admin/*`) via API gateway auth, network policies, or mTLS.
  - Consider enabling request/response logging only in controlled environments, as logs may contain redacted but still sensitive patterns.

For additional architecture and product‑level details, see `ARCHITECTURE_SECURITY.md` and `../PRODUCT_OVERVIEW.md`.
