# Go LLM Safe Pipeline Example

This example demonstrates a full, production-style LLM safety pipeline
using TSZ (Thyris Safe Zone) in Go.

It shows how to safely handle untrusted user input by enforcing
PII detection and prompt injection protection **before**
any prompt is sent to an LLM.

---

## Overview

The example implements the following flow:

User input
→ TSZ /detect
→ Redacted + validated prompt
→ TSZ LLM Gateway
→ Safe LLM response


All sensitive data is removed or neutralized inside TSZ,
ensuring a zero-trust boundary between your application and external models.

---

## What This Example Demonstrates

- PII detection and redaction
- Prompt injection neutralization
- Request ID (RID) propagation
- Zero-trust enforcement before LLM execution
- Use of the TSZ OpenAI-compatible LLM gateway
- Safe behavior even under malicious user instructions

This example intentionally uses explicit `/detect` calls
to make the security boundary clear and observable.

---

## Prerequisites

- TSZ running locally at `http://localhost:8080`
- PostgreSQL and Redis running (required by TSZ)
- Go (matching version in `go.mod`)
- An upstream LLM configured for TSZ (e.g. Ollama)

---

## LLM Configuration

TSZ does not ship with a model by default.
You must configure an upstream LLM.

Set one of the following environment variables:

```bash
export TSZ_MODEL=gemma3:1b
# or
export AI_MODEL=gemma3:1b
```

If using Ollama, list available models with:

```bash
ollama list
```

## How to Run

From this directory:

```bash
go run main.go
```

## Example Output

```bash
Redacted prompt (safe for LLM):
Email: [RID-GO-PIPELINE-001_EMAIL_xxx]
Credit Card: [RID-GO-PIPELINE-001_CREDIT_CARD_xxx]
[PROMPT_INJECTION placeholder]

Safe LLM response:
I cannot and will not share sensitive information...
```

## Notes

- This example does not assume any custom guardrails are configured.
- It works with a fresh TSZ installation using default behavior.
- LLM refusal responses are expected and indicate successful safety enforcement.
