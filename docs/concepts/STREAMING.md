# TSZ Streaming & Guardrails Concepts

This document explains how TSZ (Thyris Safe Zone) handles **streaming LLM responses** via the OpenAI-compatible gateway, and how guardrails interact with streaming in different modes.

It is intended for architects and engineers designing **enterprise-grade LLM integrations** that require real-time protection against PII leakage, toxic language, and other unsafe content.

---

## 1. Overview

TSZ exposes an OpenAI-compatible `/v1/chat/completions` endpoint that supports both:

- **Non-streaming responses** (`stream=false`)
- **Streaming responses** (`stream=true`, Server-Sent Events / SSE)

For streaming responses, TSZ can:

1. **Pass through** the upstream stream as-is (`final-only` mode)
2. **Synchronously validate and sanitize** output while streaming (`stream-sync` mode)
3. **Asynchronously validate** the full streamed output for audit/SIEM (`stream-async` mode)

These behaviours are controlled via HTTP headers and do not require additional environment variables.

---

## 2. Streaming Modes

### 2.1 Mode Selection

The following header controls how TSZ applies guardrails to streaming responses:

```http
X-TSZ-Guardrails-Mode: final-only | stream-sync | stream-async
```

- If omitted, the default is `final-only`.

Internally, the gateway interprets this as:

- `final-only`  → input guardrails + optional non-stream output guardrails; streaming output is proxied as-is
- `stream-sync` → input guardrails + **incremental output guardrails** while streaming
- `stream-async` → input guardrails + **async output guardrails** after stream completion

### 2.2 On-fail Behaviour (Streaming Only)

When using `stream-sync`, TSZ needs to know what to do when a violation is detected on the **output**.

This is controlled via:

```http
X-TSZ-Guardrails-OnFail: filter | halt
```

- Default is `filter` when omitted.

Semantics:

- `filter`
  - Unsafe segments (PII, secrets, toxic content, etc.) are **redacted** in the streaming output.
  - The stream continues and the client never sees raw unsafe content.

- `halt`
  - On a high-confidence violation, TSZ **terminates the stream**.
  - An OpenAI-style error payload is sent as an SSE event, followed by `data: [DONE]`.
  - This is the strictest mode, suitable for highly regulated scenarios.

> Note: Non-streaming (`stream=false`) requests always apply output guardrails over the full assistant response and ignore `X-TSZ-Guardrails-Mode` / `X-TSZ-Guardrails-OnFail`.

---

## 3. Guardrails Pipeline

TSZ guardrails are applied at two main stages for the LLM gateway:

1. **Input guardrails** (user messages) – always active when `X-TSZ-Guardrails` is set
2. **Output guardrails** (assistant messages) – behaviour depends on streaming mode

### 3.1 Input Guardrails (User Messages)

Before calling the upstream LLM, TSZ:

1. Extracts all `messages` with `role == "user"`.
2. Runs `/detect` with the configured guardrails (e.g. `TOXIC_LANGUAGE`):

   ```go
   resp := detector.Detect(models.DetectRequest{
       Text:       content,
       RID:        rid,
       Guardrails: guardrailsList,
   })
   ```

3. If `resp.Blocked == true`:
   - Returns an OpenAI-compatible error with code `tsz_content_blocked`.
   - Upstream is **not** called.
4. If not blocked but `RedactedText` is present:
   - The user message is sanitized and forwarded to the upstream LLM.

This stage is identical for streaming and non-streaming requests.

### 3.2 Output Guardrails (Assistant Messages)

**Non-streaming (`stream=false`)**

- TSZ reads the full upstream response, inspects `choices[].message.content`, and runs `/detect` on each assistant message.
- If blocked, returns `400` with `tsz_output_blocked` and does not forward the raw response.
- If redaction is needed, it updates `message.content` with `RedactedText` and returns the sanitized JSON.

**Streaming (`stream=true`)**

Behaviour depends on `X-TSZ-Guardrails-Mode`:

1. `final-only`
   - TSZ proxies the SSE stream as-is (`data: {...}` lines passed through).
   - Only input guardrails are applied.

2. `stream-sync`
   - TSZ works as a **streaming validator**:
     - Reads each SSE `data: {...}` line.
     - Parses `choices[].delta.content` and appends it to an internal buffer (`rawBuffer`).
     - Periodically (on each chunk) runs `/detect` on the **accumulated text** to evaluate guardrails.
     - Maintains a separate `validatedSoFar` buffer representing what has already been safely streamed to the client.

   - On each chunk:

     ```go
     blocked, sanitized, msg := runOutputGuardrails(detector, rid, guardrailsList, rawBuffer.String(), onFail)
     ```

     - If `blocked && onFail == "halt"`:
       - TSZ sends an SSE error event with `tsz_output_blocked` and terminates the stream (`data: [DONE]`).
     - Otherwise:
       - TSZ computes the diff between `sanitized` and `validatedSoFar` and sends only the **new safe delta**.
       - The client never sees raw unsafe content.

3. `stream-async`
   - TSZ proxies the upstream SSE stream directly to the client (no modifications).
   - In parallel, TSZ buffers the entire stream (or extracted textual content) and, after completion, runs `/detect` asynchronously.
   - Results are used for logging and SIEM integration (e.g. security events tagged with the same `RID`).
   - This mode is useful when latency is critical but you still need retrospective guardrail insights.

---

## 4. Typical Streaming Scenarios

### 4.1 Baseline Streaming (No Guardrails)

Use when you only need TSZ as a **transparent LLM gateway**:

```http
POST /v1/chat/completions
Content-Type: application/json

{
  "model": "llama3.1:8b",
  "messages": [
    {"role": "user", "content": "Stream a short response about TSZ gateway"}
  ],
  "stream": true
}
```

- No `X-TSZ-Guardrails` header.
- Output is proxied as-is from the upstream LLM.

### 4.2 Streaming With Synchronous Guardrails (Filter)

Real-time protection while still returning a full answer:

```http
X-TSZ-Guardrails: TOXIC_LANGUAGE
X-TSZ-Guardrails-Mode: stream-sync
X-TSZ-Guardrails-OnFail: filter
```

- TSZ redacts unsafe segments on-the-fly.
- The user only sees sanitized output.

### 4.3 Streaming With Synchronous Guardrails (Halt)

For stricter policies where unsafe output must not be delivered:

```http
X-TSZ-Guardrails: TOXIC_LANGUAGE
X-TSZ-Guardrails-Mode: stream-sync
X-TSZ-Guardrails-OnFail: halt
```

- On severe violation, TSZ sends an SSE error event and `[DONE]`.
- Client-side SDKs should handle this as an error case.

### 4.4 Streaming With Asynchronous Validation

When you cannot afford any latency overhead but still need compliance/audit:

```http
X-TSZ-Guardrails: TOXIC_LANGUAGE
X-TSZ-Guardrails-Mode: stream-async
```

- Client sees the raw stream.
- TSZ validates in the background and emits security events/logs.

---

## 5. Design Considerations for Enterprise

### 5.1 Performance & Latency

- `stream-sync` adds overhead proportional to the number of chunks and validator complexity.
- To keep latency low:
  - Prefer simple validators for streaming (e.g. TOXIC_LANGUAGE instead of heavy multi-stage chains).
  - Consider using `stream-async` for very long responses.

### 5.2 Memory & Windowing

- TSZ accumulates assistant output in an in-memory buffer for `stream-sync`.
- In high-volume scenarios, it is recommended to:
  - Limit maximum buffer size.
  - Use a sliding window strategy (validate on the last N characters/tokens rather than the entire history).

### 5.3 Fail-Open vs Fail-Closed

- If SSE JSON parsing fails, TSZ logs the error and, by default, **forwards the raw line** (fail-open).
- For highly regulated environments, you may choose to enforce stricter behaviour:
  - Treat parsing failures as violations.
  - Immediately halt the stream.

### 5.4 Guardrail Configuration

- Streaming guardrails rely on the same underlying validators as `/detect`.
- You can reuse existing validators (e.g. `TOXIC_LANGUAGE`) or define new ones specifically for streaming scenarios.
- For best results, prompts used in `AI_PROMPT` validators should be **short and deterministic**.

---

## 6. Summary

- TSZ extends the OpenAI-compatible gateway with **enterprise-grade streaming guardrails**.
- With a small set of headers, you can choose between pass-through, synchronous protection, and asynchronous audit modes.
- The design is inspired by systems like Guardrails AI but implemented natively in TSZ, respecting your existing PII patterns, validators, and SIEM integration.

For concrete API examples and header details, see:

- `docs/API_REFERENCE.md` – *OpenAI-Compatible LLM Gateway* section
- `docs/TSZ_Postman_Collection.json` – ready-to-use Postman requests for all streaming modes
