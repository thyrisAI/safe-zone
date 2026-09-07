# TSZ Streaming & Guardrails Concepts

This document explains how TSZ (Thyris Safe Zone) handles **streaming LLM responses** and how guardrails interact with each supported architecture.

It is intended for architects and engineers designing **enterprise-grade LLM integrations** that require real-time protection against PII leakage, toxic language, and other unsafe content.

---

## 1. Two distinct streaming architectures

TSZ has two streaming integrations. They have different control planes and
security contracts, and must not be treated as interchangeable.

Historically this document described only the legacy `/v1/chat/completions`
header-based model. The BYG / Envoy `ext_proc` modes below are a separate
architecture, added here as their own contract rather than as legacy header
modes.

| Integration | Configuration | Modes covered here |
| --- | --- | --- |
| Legacy OpenAI-compatible gateway (`/v1/chat/completions`) | Request headers | `final-only`, `stream-sync`, `stream-async`, and its header-driven `halt` behaviour |
| Bring Your Gateway (BYG), Envoy `ext_proc` | A stream-pinned `TSZGuardrailPolicy` | `AsyncAudit` observation and `Windowed` response enforcement |

The legacy gateway material begins in [Legacy gateway streaming](#3-legacy-openai-compatible-gateway). The BYG model is described separately below; it does **not** use the legacy streaming headers.

## 2. BYG / Envoy `ext_proc`

### AsyncAudit observation

`Streaming.Mode: AsyncAudit` forwards each SSE body chunk immediately and
unchanged. Completed, event-aligned OpenAI Chat Completions events are retained
only in a bounded in-memory buffer and inspected by a bounded background worker
after end-of-stream. The resulting response-stage decision is emitted as
PII-safe `AUDIT_ONLY` metadata/audit output.

The current preview exposes this mode through the portable compiled-policy
profile used by the example runner. The frozen, storage-compatible
`TSZGuardrailPolicy` v1alpha1/v1beta1 schema continues to admit `None` and
`Windowed` only; adding the native field requires a future API version rather
than silently changing the graduated schema.

This is visibility, not enforcement: detected content has already reached the
client. Policy validation rejects response `MASK` and `BLOCK` actions in this
mode rather than implying that an asynchronous decision can recall bytes. A
buffer or worker-queue overflow also preserves delivery and records a degraded
aggregate failure without logging content. See the runnable
[10-stream-async-audit example](../../examples/bring-your-gateway/10-stream-async-audit/README.md).

### Windowed enforcement

BYG Windowed enforcement operates on an Envoy external-processing response
stream. Enable it with the policy's `Streaming.Mode: Windowed` and choose a
bounded target size with `window_bytes` (the Kubernetes API field is
`windowBytes`). For example:

```json
"streaming": {
  "mode": "Windowed",
  "window_bytes": 4096
}
```

TSZ parses the upstream OpenAI `text/event-stream` response into **complete SSE
events**. It accumulates event-aligned assistant deltas into the configured
window, retaining a small trailing overlap for matches that span event/window
boundaries. The semantic validator processes that complete window before TSZ
releases its safe, event-aligned prefix to Envoy. This adds bounded latency and
per-stream memory use in exchange for detecting data split across deltas.

See the runnable [11-stream-window example](../../examples/bring-your-gateway/11-stream-window/README.md) for its fixture, policy, command, and assertions.

### Delivery boundary and strict-streaming decision

**Windowed enforcement is not strict streaming and makes no zero-leakage
guarantee.** An SSE delta already emitted by Envoy to the downstream client
cannot be recalled. Enforcement protects the window that has not yet been
released; it cannot repair a violation detected only after earlier content has
been delivered.

**Decision: strict (zero-leakage) streaming is not feasible for this BYG MVP.**
A genuine strict guarantee would require holding every response byte until the
complete response has been inspected. That is buffered, non-streaming response
enforcement, even if another component later replays the validated body as SSE.
Policies requesting `Strict` are therefore rejected at admission/policy
validation; TSZ never silently downgrades them to `Windowed` or audit-only.
Routes that require no leakage must use the buffered, non-streaming profile and
apply response `BLOCK`/`MASK` there.

### Block/halt and cancellation semantics

`Windowed` supports response masking and best-effort halt. A response `BLOCK`
decision returns a safe terminal response and stops future SSE delivery. This
does not retract events released from earlier safe windows and must not be
described as zero-leakage or mapped to the legacy gateway's header-driven
`halt` semantics. See the runnable
[12-stream-halt example](../../examples/bring-your-gateway/12-stream-halt/README.md).

If the downstream client disconnects and Envoy cancels the `ext_proc` RPC, TSZ
stops inline work such as the window buffer and semantic validation and returns
gRPC `CANCELED`. If its deadline expires, it returns `DEADLINE_EXCEEDED`.
Neither condition is a guardrail-engine error, so neither participates in the
fail-open/fail-closed enforcement decision. A cancellation audit event is
attempted detached and best-effort; audit delivery never blocks the stream.

---

## 3. Legacy OpenAI-compatible gateway

TSZ exposes an OpenAI-compatible `/v1/chat/completions` endpoint that supports
both non-streaming responses (`stream=false`) and streaming responses
(`stream=true`, Server-Sent Events / SSE). The remaining sections describe
this legacy, header-controlled gateway only.

### 3.1 Streaming modes

The following header controls how TSZ applies guardrails to streaming responses:

```http
X-TSZ-Guardrails-Mode: final-only | stream-sync | stream-async
```

- If omitted, the default is `final-only`.

Internally, the gateway interprets this as:

- `final-only`  -> input guardrails + optional non-stream output guardrails; streaming output is proxied as-is
- `stream-sync` -> input guardrails + **incremental output guardrails** while streaming
- `stream-async` -> input guardrails + **async output guardrails** after stream completion

### 3.2 On-fail behaviour (streaming only)

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

## 4. Legacy gateway guardrails pipeline

TSZ guardrails are applied at two main stages for the LLM gateway:

1. **Input guardrails** (developer, system, user, assistant, and tool messages) – always active when `X-TSZ-Guardrails` is set
2. **Output guardrails** (assistant messages) – behaviour depends on streaming mode

### 4.1 Input guardrails (messages and tool payloads)

Before calling the upstream LLM, TSZ:

1. Extracts string content from `developer`, `system`, `user`, `assistant`, and `tool` messages, plus assistant refusal content and `tool_calls[].function.arguments`.
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

### 4.2 Output guardrails (assistant messages)

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

## 5. Legacy gateway streaming scenarios

### 5.1 Baseline streaming (no guardrails)

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

### 5.2 Streaming with synchronous guardrails (filter)

Real-time protection while still returning a full answer:

```http
X-TSZ-Guardrails: TOXIC_LANGUAGE
X-TSZ-Guardrails-Mode: stream-sync
X-TSZ-Guardrails-OnFail: filter
```

- TSZ redacts unsafe segments on-the-fly.
- The user only sees sanitized output.

### 5.3 Streaming with synchronous guardrails (halt)

For stricter policies where unsafe output must not be delivered:

```http
X-TSZ-Guardrails: TOXIC_LANGUAGE
X-TSZ-Guardrails-Mode: stream-sync
X-TSZ-Guardrails-OnFail: halt
```

- On severe violation, TSZ sends an SSE error event and `[DONE]`.
- Client-side SDKs should handle this as an error case.

### 5.4 Streaming with asynchronous validation

When you cannot afford any latency overhead but still need compliance/audit:

```http
X-TSZ-Guardrails: TOXIC_LANGUAGE
X-TSZ-Guardrails-Mode: stream-async
```

- Client sees the raw stream.
- TSZ validates in the background and emits security events/logs.

---

## 6. Legacy gateway design considerations

### 6.1 Performance and latency

- `stream-sync` adds overhead proportional to the number of chunks and validator complexity.
- To keep latency low:
  - Prefer simple validators for streaming (e.g. TOXIC_LANGUAGE instead of heavy multi-stage chains).
  - Consider using `stream-async` for very long responses.

### 6.2 Memory and windowing

- TSZ accumulates assistant output in an in-memory buffer for `stream-sync`.
- In high-volume scenarios, it is recommended to:
  - Limit maximum buffer size.
  - Use a sliding window strategy (validate on the last N characters/tokens rather than the entire history).

### 6.3 Fail-open vs fail-closed

- If SSE JSON parsing fails, TSZ logs the error and, by default, **forwards the raw line** (fail-open).
- For highly regulated environments, you may choose to enforce stricter behaviour:
  - Treat parsing failures as violations.
  - Immediately halt the stream.

### 6.4 Guardrail configuration

- Streaming guardrails rely on the same underlying validators as `/detect`.
- You can reuse existing validators (e.g. `TOXIC_LANGUAGE`) or define new ones specifically for streaming scenarios.
- For best results, prompts used in `AI_PROMPT` validators should be **short and deterministic**.

---

## 7. Summary

- TSZ extends the OpenAI-compatible gateway with **enterprise-grade streaming guardrails**.
- With a small set of headers, you can choose between pass-through, synchronous protection, and asynchronous audit modes.
- The design is inspired by systems like Guardrails AI but implemented natively in TSZ, respecting your existing PII patterns, validators, and SIEM integration.

For concrete API examples and header details, see:

- `docs/API_REFERENCE.md` – *OpenAI-Compatible LLM Gateway* section
- `docs/TSZ_Postman_Collection.json` – ready-to-use Postman requests for all streaming modes
