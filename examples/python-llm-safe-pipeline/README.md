# Python LLM Safe Pipeline Example

This example demonstrates a **full, production-style LLM safety pipeline**
using **TSZ (Thyris Safe Zone)** with Python.

It shows how to safely process untrusted user input by enforcing
PII detection and prompt injection protection **before** any LLM is called.

---

## Overview

The example implements the following flow:

User input
→ TSZ /detect
→ Redacted + validated prompt
→ TSZ LLM Gateway (/v1/chat/completions)
→ Safe LLM response


All sensitive data is removed or neutralized **inside TSZ** before the
prompt reaches an upstream LLM.

---

## What This Example Demonstrates

- PII detection and redaction (email, credit card, etc.)
- Prompt injection detection and neutralization
- Request ID (RID) propagation for traceability
- Zero-trust enforcement before LLM execution
- Use of the TSZ OpenAI-compatible LLM gateway
- Safe behavior even under malicious user instructions

This example intentionally uses **explicit `/detect` + gateway calls**
to make the security boundary clear and observable.

---

## Prerequisites

- TSZ running locally at `http://localhost:8080`
- PostgreSQL and Redis running (required by TSZ)
- Python 3.9+
- An upstream LLM configured for TSZ (for example, via Ollama)

---

## Installation

Install the official TSZ Python client:

```bash
pip install "tszclient-py @ git+https://github.com/thyrisAI/safe-zone.git@main"
```

## LLM Configuration

TSZ does not ship with a model by default.
You must configure an upstream LLM.

Set one of the following environment variables to a valid model name:

```bash
export TSZ_MODEL=gemma3:1b
# or
export THYRIS_AI_MODEL=gemma3:1b
```

## How to Run

From the directory:

```bash
python main.py
```

## Example Output

```bash
Redacted prompt (safe for LLM):
Email: [RID-PY-PIPELINE-001_EMAIL_xxx]
Credit Card: [RID-PY-PIPELINE-001_CREDIT_CARD_xxx]
[PROMPT_INJECTION placeholder]

Safe LLM response:
I cannot and will not share sensitive information...
```

Even when the input includes malicious instructions, no raw PII reaches the LLM.

## Notes

- This example does not assume any custom guardrails are configured.
- It works with a fresh TSZ installation using default behavior.
- The example focuses on correctness and clarity rather than forcing
specific LLM responses.
- LLM refusal responses are expected and indicate successful safety enforcement.