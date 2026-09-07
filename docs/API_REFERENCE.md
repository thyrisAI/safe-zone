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

TSZ supports middleware-based authentication and RBAC.

```http
Authorization: Bearer <tsz-token>
```

Authentication behavior is controlled via:

```env
AUTH_ENABLED=false
AUTH_REQUIRE_BEARER_TOKEN=true
AUTH_TOKEN_PERMISSIONS=token_detect=detect:read,token_admin=*
AUTH_PUBLIC_PATHS=/healthz,/ready
```

- `AUTH_ENABLED=false` (default): open endpoints (recommended only for trusted internal networks).
- `AUTH_ENABLED=true`: all non-public endpoints require a valid token with matching permissions.
- Public endpoints are `/healthz` and `/ready` by default.
- Legacy `X-ADMIN-KEY` compatibility remains available for admin handlers.

Permissions:

- `detect:read`
- `gateway:use`
- `patterns:admin`
- `validators:admin`
- `allowlist:admin`
- `blacklist:admin`
- `templates:admin`
- `cache:admin`

**Request Security Controls**

- Write endpoints require `Content-Type: application/json`.
- Request body size limit is enabled (default: 10 MB, configurable with `MAX_REQUEST_SIZE_BYTES`).
- Per-endpoint timeouts are enforced (default: `/detect` 30s, `/v1/chat/completions` 300s).
- CORS is fail-secure by default (`CORS_ALLOWED_ORIGINS` empty => deny).
- Security headers middleware is enabled by default (`SECURITY_HEADERS_ENABLED=true`).

**Rate Limiting**

- Global and endpoint-level limits are enabled by default.
- Exceeded quotas return `429 Too Many Requests`.

You are strongly encouraged to place TSZ behind your own API Gateway / mTLS / WAF for external exposure.

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

  - `< 0.30`  -> **ALLOW** (ignored)
  - `0.30 – 0.85` -> **MASK** (redact in output)
  - `≥ 0.85` -> **AUTO‑BLOCK**

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

Auth requirement:
- If `AUTH_ENABLED=true`, requires permission `detect:read`.

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
- `breakdown`: Map of detection type -> count. Example: `{ "EMAIL": 2, "PHONE_NUMBER": 1 }`.
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

Auth requirement:
- If `AUTH_ENABLED=true`, requires permission `gateway:use`.

TSZ implements the **request and response shape** of the OpenAI `chat/completions` endpoint for both non‑streaming (`stream=false`) and streaming (`stream=true`) calls. Streaming support depends on the selected provider; for example, `AI_PROVIDER=BEDROCK` currently supports **non-streaming only**.

#### 3.2.1 High-Level Behaviour

1. Client sends an OpenAI‑style chat completion request to TSZ:
   - `model`: any model name (forwarded as‑is to upstream)
   - `messages`: array of chat messages
   - `stream`: `false` (standard JSON response) or `true` (SSE streaming)
2. TSZ runs `/detect` logic on **developer, system, user, assistant, and tool-result messages**, including assistant refusals and tool-call arguments, before calling the LLM:
   - PII & secret detection
   - Guardrails / validators (e.g. `TOXIC_LANGUAGE`)
3. If unsafe on input:
   - TSZ **blocks** the request and returns an OpenAI‑compatible error response.
4. If safe on input:
   - TSZ **redacts** sensitive message content, tool-call arguments, and tool results before forwarding the sanitized request.
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

TSZ supports multiple AI providers. The provider is selected via the `AI_PROVIDER` environment variable.

##### OpenAI-Compatible Provider (Default)

```env
AI_PROVIDER=OPENAI_COMPATIBLE
AI_MODEL_URL=https://api.openai.com/v1
AI_API_KEY=sk-...your-openai-key...
AI_MODEL=gpt-4
```

- `AI_PROVIDER`: Set to `OPENAI_COMPATIBLE` (default) for OpenAI, Azure OpenAI, Ollama, or any OpenAI-compatible endpoint.
- `AI_MODEL_URL`: Base URL of an OpenAI‑compatible API. TSZ appends `/chat/completions`.
- `AI_API_KEY`: API key for the upstream service (sent as `Authorization: Bearer <key>`).
- `AI_MODEL`: Default model name used by internal AI validators; the gateway itself forwards the `model` field from the incoming request.

##### AWS Bedrock Provider

TSZ natively supports AWS Bedrock, allowing you to use models like Anthropic Claude, Amazon Titan, Meta Llama, Mistral, and Cohere directly through the AWS SDK.

```env
AI_PROVIDER=BEDROCK
AWS_BEDROCK_REGION=us-east-1
AWS_BEDROCK_MODEL_ID=anthropic.claude-3-sonnet-20240229-v1:0
# Optional: Custom endpoint for VPC endpoints
# AWS_BEDROCK_ENDPOINT_OVERRIDE=https://vpce-xxx.bedrock-runtime.us-east-1.vpce.amazonaws.com
```

- `AI_PROVIDER`: Set to `BEDROCK` to use AWS Bedrock.
- `AWS_BEDROCK_REGION`: AWS region where Bedrock is available (required).
- `AWS_BEDROCK_MODEL_ID`: Bedrock model identifier (e.g., `anthropic.claude-3-sonnet-20240229-v1:0`).
- `AWS_BEDROCK_ENDPOINT_OVERRIDE`: Optional custom endpoint URL for VPC endpoints or testing.

**AWS Credentials**: Bedrock uses the standard AWS credential chain:
- Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`)
- Shared credentials file (`~/.aws/credentials`)
- IAM role (when running on EC2, ECS, Lambda, etc.)

**Required IAM Permissions**:
```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "bedrock:InvokeModel",
                "bedrock:InvokeModelWithResponseStream"
            ],
            "Resource": "arn:aws:bedrock:*::foundation-model/*"
        }
    ]
}
```

**Supported Bedrock Models**:

| Model Family | Example Model ID | Notes |
|--------------|------------------|-------|
| Anthropic Claude | `anthropic.claude-3-sonnet-20240229-v1:0` | Recommended for most use cases |
| Amazon Titan | `amazon.titan-text-express-v1` | Good for general text generation |
| Meta Llama | `meta.llama3-8b-instruct-v1:0` | Open-source alternative |
| Mistral | `mistral.mistral-7b-instruct-v0:2` | Fast inference |
| Cohere | `cohere.command-text-v14` | Good for summarization |

> **Note**: Bedrock streaming support is planned for a future release. Currently, only non-streaming requests (`stream=false`) are supported with Bedrock. If a client sends `stream=true` while `AI_PROVIDER=BEDROCK`, TSZ returns an OpenAI-compatible `400` error with code `streaming_not_supported`.

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
  -H "Authorization: Bearer token_gateway" \
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
  -H "Authorization: Bearer token_gateway" \
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
  -H "Authorization: Bearer token_gateway" \
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
  -H "Authorization: Bearer token_gateway" \
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
  -H "Authorization: Bearer token_gateway" \
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
    api_key="token_gateway"  # TSZ auth token when AUTH_ENABLED=true
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

- Inspect and redact developer, system, user, assistant, tool-call, and tool-result content.
- Forward the sanitized request to the configured upstream LLM service.
- For non‑streaming calls, apply output guardrails to the full assistant message before returning.
- For streaming calls, behave according to the chosen `X-TSZ-Guardrails-Mode` and `X-TSZ-Guardrails-OnFail`.

Current limitations:

- String content in `role == "developer"`, `role == "system"`, `role == "user"`, `role == "assistant"`, and `role == "tool"` messages is scanned and redacted on input. Assistant refusal content and `tool_calls[].function.arguments` strings are also scanned.
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

### 3.3 Bring Your Gateway: Envoy External Processing

This integration is distinct from TSZ's built-in `/v1/chat/completions` proxy.
Envoy remains the public gateway and owns TLS, authentication, authorization,
routing, retries and rate limits. `tsz-ext-proc` is an internal Envoy
`ext_proc` service that evaluates content only.

The checked-in reference environment supports Envoy Gateway **v1.8.3** and
Gateway API **v1.5.1**. Installation and profile selection are documented in
[the Envoy Gateway integration guide](integrations/ENVOY_GATEWAY.md).

#### Kubernetes policy API versions

New native policy manifests use `apiVersion: security.thyris.ai/v1beta1` and
`kind: TSZGuardrailPolicy`. The beta controller uses this API version;
`v1alpha1` remains served with a deprecation warning for existing clients.
Both versions expose identical spec/status schemas, defaults and validation,
while all new writes are stored as `v1beta1`. This version is independent of
Envoy's API version and of immutable TSZ policy snapshot versions.

Install the dual-version CRD before upgrading the controller. Existing policy
manifests need only an `apiVersion` change. See the
[policy API upgrade guide](operations/TSZ_POLICY_API_UPGRADE.md) for compatibility
evidence, storage migration, supported rollback and the remaining GA feedback gates.

#### Native gateway adapter selector

`TSZGuardrailPolicy.spec.adapter` selects an installed native adapter. It defaults
to `envoy-gateway`, accepts a DNS-label name up to 63 characters, and is immutable.
Both served API versions expose the same field. The shipped controller registers
only `envoy-gateway`; other names report `Accepted=False` and `Programmed=False`
with reason `UnsupportedCapability`. Requested actions and target/section scopes
must be supported by that adapter, including actions in referenced snapshots.
Existing native resources remain unchanged when a new generation is rejected.
See [native adapter selection](integrations/NATIVE_GATEWAY_ADAPTERS.md) for the
extension boundary, compatibility and Phase 7 scope.

#### Response contract

For a strict no-leakage guarantee, use supported buffered, non-streaming OpenAI,
Anthropic Messages, or Gemini GenerateContent traffic. The request must use
`Content-Type: application/json`; unsupported content shapes are processing
failures, not silently allowed content. The portable Envoy BYG `AsyncAudit`
compiled-policy mode forwards SSE unchanged and performs bounded post-stream observation; it accepts only
response `ALLOW`/`AUDIT_ONLY` actions and provides no confidentiality guarantee.
The separate `Windowed` mode currently understands Chat Completions events
only and is best-effort: it cannot retract content that Envoy has already sent. `MASK` rewrites the
not-yet-released event window. `BLOCK` returns a safe terminal response and
halts future delivery; use buffered response processing when any prior leakage
is unacceptable.

The supported non-streaming content fields are:

| API | Request fields | Response fields |
| --- | --- | --- |
| Chat Completions | String `messages[].content`, `text` fields in supported multimodal content arrays for developer/system/user/assistant/tool messages, assistant top-level and content-part `refusal` fields, and `messages[].tool_calls[].function.arguments` | String `choices[].message.content`, assistant top-level and content-part `refusal` fields, and `choices[].message.tool_calls[].function.arguments` |
| Responses | String `instructions`, string `input`, `input_text`/`output_text`/`refusal` fields in supported developer/system/user/assistant message content arrays, `function_call.arguments`, and string or multimodal `function_call_output.output` in `input[]` | Assistant `output_text`, `refusal`, and `function_call.arguments` fields in `output[]`; top-level `output_text` is kept consistent when present |
| Embeddings (OpenAI-compatible) | Non-empty string `input` or non-empty array of non-empty strings; every item is inspected independently | Input-only: vectors, usage and provider errors pass through unchanged |
| MCP Streamable HTTP | `prompts/get` string arguments and `tools/call` JSON-object arguments | Prompt text and embedded text resources; tool-result text, embedded text resources and `structuredContent` objects |
| Anthropic Messages | Top-level string or text-block `system`; user/assistant string and text-block content; `tool_use.input`; string or text-block `tool_result.content` | Assistant text blocks and `tool_use.input` |
| Gemini GenerateContent | `systemInstruction` and `contents[].parts[].text`; `functionCall.args`, `functionResponse.response`, server `toolCall.args`/`toolResponse.response`, executable code and execution output | The corresponding supported fields in `candidates[].content.parts[]` |

TSZ changes only the extracted text string values and the derived Responses
API `output_text` value; item order, unknown fields and untouched JSON bytes
are preserved. Tool names and execution authorization are not changed. Tool
payloads in streaming events, the bytes or meaning of multimodal image/audio/file
data, and Responses, Anthropic, or Gemini streaming events are not covered by
this capability yet. Anthropic requests are selected using the required
`anthropic-version` header; Gemini requests are selected by their
`contents`/`systemInstruction` shape.

| Policy action | Envoy result |
| --- | --- |
| `ALLOW` | Continue without changing the body. |
| `AUDIT_ONLY` | Continue without changing the body. |
| `MASK` | Replace unsafe supported content fields and update `content-length`. |
| `BLOCK` | Replace the upstream response with a safe local `403` response. |

This scope does **not** guarantee Responses, Anthropic, Gemini, or MCP streaming enforcement.
Configure both request and response bodies as `Buffered`; do not attach this
profile to a route that requires an unbuffered or Responses SSE safety
guarantee.

#### Embeddings input guardrails

The BYG processor supports OpenAI-compatible `/v1/embeddings` and `/embeddings`
request paths (including query strings). Envoy must forward the `:path` request
header to ext_proc; other adapters must populate `ProcessingRequest.RequestPath`.
The request path is retained for response processing and is never taken from
response headers. Custom public paths must be rewritten to a supported path
before TSZ inspection. Endpoint selection is necessary because Responses API
requests also use `input`; model names are not used to guess the API.

Each text input runs through the same pinned request policy as chat content:
PII, secrets, custom patterns, allowlists, blocklists and configured validators.
`ALLOW` and `AUDIT_ONLY` preserve the body; `MASK` replaces affected input strings
and corrects `content-length`; a `BLOCK` in any item blocks the entire request.
Array order, model, dimensions, encoding format and unrelated fields are preserved.
Audit metadata uses adapter `openai_embeddings` and contains no input text.

Example request through the protected gateway:

```json
{"model":"text-embedding-3-small","input":["ordinary text","contact alice@example.com"]}
```

With a PII masking policy, only the email in the second input is redacted before
upstream delivery. A secret configured to block in any input prevents the whole
request from reaching the provider. Validate this with the local mock provider
and the existing route-owned policy setup.

The [OpenAI embeddings API](https://developers.openai.com/api/reference/resources/embeddings/methods/create)
also accepts token ID arrays. TSZ currently supports **text inputs only**: token
ID arrays, mixed arrays, empty inputs and malformed JSON produce processing
errors under the configured failure policy. Use `fail-closed` for enforcement;
`fail-open` can forward uninspected inputs on an error. Token decoding and
provider/model token-limit validation are not implemented by this adapter.
Embedding responses are outside content inspection, including when response
policies are enabled. This feature adds BYG inspection, not a standalone TSZ
`POST /v1/embeddings` proxy endpoint. Use buffered processing.

#### MCP prompt and tool-payload guardrails

The BYG processor supports individual, buffered MCP JSON-RPC 2.0 messages over
Streamable HTTP using the MCP **2025-06-18** content shapes. It identifies MCP
from the top-level `jsonrpc: "2.0"` field, so the public MCP endpoint may use any
path. The Envoy adapter retains the request method to interpret the matching
JSON-RPC response safely; response bodies cannot select their own method. The
following content is inspected with the policy snapshot pinned to the request:

- `prompts/get` request `params.arguments` string map
- `tools/call` request `params.arguments` JSON object
- `prompts/get` response message `content.text` and embedded
  `content.resource.text`
- `tools/call` response `content[].text`, embedded resource text and
  `structuredContent` JSON object, including results with `isError: true`

`ALLOW` and `AUDIT_ONLY` preserve the original body. `MASK` changes only the
extracted fields and updates `content-length`. A request-side `BLOCK` prevents
the MCP server call; a response-side `BLOCK` prevents the result from reaching
the client. Metadata uses adapter `mcp_jsonrpc` and never includes arguments,
prompt text, tool results or raw detections.

Tool and prompt names, JSON-RPC IDs, annotations and unrelated fields remain
unchanged. Image, audio and embedded-resource blob bytes are preserved without
inspection. Resource links are preserved. Unknown content variants, malformed
covered payloads, duplicate JSON keys and mutations that would make a structured
object invalid are processing errors and follow the configured failure policy.
MCP initialization, discovery, notifications and protocol-error responses pass
through because they do not contain prompt or tool payloads covered here.

This adapter performs content guardrails only. It does not authorize tool
execution, decide which MCP server or tool may be used, validate tool schemas,
or enforce MCP authentication and `Origin` checks; those controls remain with
the gateway and MCP client/server. Configure both request and response bodies as
`Buffered`. MCP SSE messages, stdio transport, JSON-RPC batching, resource-read
payloads and binary content inspection are outside this capability.

#### Envoy attachment and runtime settings

The manual attachment requires these fields:

| Field | Required value | Purpose |
| --- | --- | --- |
| `extProc.backendRefs` | `tsz-ext-proc:9002` | Internal gRPC processor service. |
| `messageTimeout` | e.g. `2s` | Envoy's per-message ext_proc deadline. Align it with the processor timeout. |
| `failOpen` | `false` | Envoy must not bypass TSZ when the processor is unavailable. |
| `processingMode.request.body` | `Buffered` | Enables request inspection and mutation. |
| `processingMode.response.body` | `Buffered` | Enables whole-response inspection and mutation before delivery. |
| `metadata.writableNamespaces` | `io.thyris.tsz` | Allows the processor to publish safe dynamic metadata. |

`tsz-ext-proc` validates its runtime configuration on startup:

| Variable | Default | Purpose |
| --- | --- | --- |
| `TSZ_FAIL_MODE` | `closed` | Fallback when no policy-specific failure mode is available. |
| `TSZ_MAX_BODY_BYTES` | `1048576` | Maximum buffered request **and** response body size. |
| `TSZ_MAX_STREAM_BUFFER_BYTES` | `262144` | Maximum per-stream SSE parser or window buffer. Overflow terminates the ext_proc stream with `RESOURCE_EXHAUSTED`; Envoy's external-processor failure behavior then applies. |
| `TSZ_MAX_GRPC_MESSAGE_BYTES` | `4194304` | Maximum ext_proc gRPC message size. |
| `TSZ_PROCESSING_TIMEOUT_MS` | `2000` | TSZ processing deadline for one ext_proc message. |
| `TSZ_MAX_CONCURRENT_STREAMS` | `100` | Maximum concurrent ext_proc streams per processor replica. |
| `TSZ_POLICY_RECONCILE_INTERVAL` | `30s` | Frequency of PostgreSQL full-cache reconciliation. |
| `TSZ_POLICY_RECONCILE_FAILURE_THRESHOLD` | `3` | Consecutive reconcile failures that make `/readyz` return `503`. |
| `TSZ_POLICY_MAX_STALENESS` | `5m` | Maximum age of the last successful reconciliation before `/readyz` returns `503`. |
| `TSZ_POLICY_RESOLUTION_MODE` | `header` | `header` for the preview profile; `attribute` for the native controller profile. |

Policy identity is never client authority. In the preview profile, Envoy
overwrites `X-TSZ-Policy` before calling ext_proc. In the native profile, TSZ
uses Envoy's trusted `xds.route_name` attribute. Do not accept a
client-supplied policy header as an override.

#### Failures, limits and safe telemetry

`failure_policy.request` and `failure_policy.response` are evaluated
independently. `closed` blocks when TSZ cannot safely process the relevant
stage; `open` permits the original traffic and must be an explicit risk
decision. A guardrail-engine error is never treated as a positive safety
finding.

An Envoy-native local reply can reach `ext_proc` without a preceding request
callback. There is then no immutable policy snapshot from which to read a
per-policy response failure mode. On Envoy Gateway v1.8.3 this response-only
case uses global `TSZ_FAIL_MODE` instead: the default `closed` returns the safe
response-stage `403`, while `open` explicitly continues the original reply.
The event is observable through
`tsz_extproc_response_without_request_state_total{outcome=fail_closed|fail_open}`
and a safe degraded audit record (`reason=response_without_request_state`).

The size limit is deterministic rather than a fail-open/fail-closed decision:

- An oversized request is rejected with `413` and `TSZ_REQUEST_BODY_TOO_LARGE`.
- An oversized upstream response is replaced with `502` and
  `TSZ_RESPONSE_BODY_TOO_LARGE`; no upstream response content is returned.

Response blocks use this intentionally small JSON shape:

```json
{
  "error": {
    "code": "TSZ_RESPONSE_GUARDRAIL_BLOCKED",
    "message": "Response blocked by guardrail policy."
  },
  "tsz_meta": {
    "rid": "RID-...",
    "envoy_request_id": "...",
    "policy_id": "...",
    "policy_version": 1
  }
}
```

TSZ publishes the following dynamic metadata under `io.thyris.tsz`:
`request_id`, `rid`, `policy_id`, `policy_version`, `adapter`, `stage`,
`action`, `categories`, `detection_count`, and `processor_latency_ms`.
It never contains message content, detected values, prompts, credentials or
validator output. In the Bring Your Gateway Envoy integration, RID and action
are intentionally published through this Envoy dynamic metadata and the TSZ
audit event; they are **not** emitted as client-facing `X-TSZ-RID` or
`X-TSZ-Action` response headers. Configure Envoy access logs or telemetry to
consume `io.thyris.tsz` when correlating a client-visible response with TSZ
enforcement.

Authentication failures and rate-limit responses originate in Envoy policies.
If TSZ receives such a response without safely processable policy state or
content, its documented failure-mode behavior can replace it with a safe TSZ
response.

---

## 4. Pattern Management API

Patterns represent **regex‑based detection rules** for PII, secrets, or other structured signals.

Auth requirement:
- If `AUTH_ENABLED=true`, all `/patterns` endpoints require `patterns:admin`.

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

Auth requirement:
- If `AUTH_ENABLED=true`, all `/allowlist` endpoints require `allowlist:admin`.

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

Auth requirement:
- If `AUTH_ENABLED=true`, all `/blacklist` endpoints require `blacklist:admin`.

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

Auth requirement:
- If `AUTH_ENABLED=true`, all `/validators` endpoints require `validators:admin`.

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

Auth requirement:
- If `AUTH_ENABLED=true`, `/templates/import` requires `templates:admin`.

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

Auth requirement:
- If `AUTH_ENABLED=true`, requires `cache:admin` permission.

**Description**

Manually clears in‑memory / Redis‑backed caches so that changes in the database are reflected immediately.

Current behaviour (subject to extension):

- Clears pattern cache
- Clears allowlist cache
- Clears blocklist cache

**Responses**

- `200 OK` with JSON payload:
  - `{"status":"ok","message":"All caches cleared"}`
- `405 Method Not Allowed` if called with a non‑POST method.
- `401 Unauthorized` if auth is enabled and token/key is missing or invalid.
- `403 Forbidden` if token lacks `cache:admin`.

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
  - Enable built-in auth (`AUTH_ENABLED=true`) and assign least-privilege token permissions.
  - Protect admin endpoints (`/admin/*`) via API gateway auth, network policies, or mTLS.
  - Consider enabling request/response logging only in controlled environments, as logs may contain redacted but still sensitive patterns.

For additional architecture and product‑level details, see `ARCHITECTURE_SECURITY.md` and `../PRODUCT_OVERVIEW.md`.
