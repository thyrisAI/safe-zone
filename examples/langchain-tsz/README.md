## LangChain + TSZ Secure Gateway (Python)

This example demonstrates how to secure LangChain applications using TSZ (Thyris Safe Zone) as a security firewall.

TSZ sits between your app and the LLM, automatically:

- Detecting PII
- Blocking data exfiltration
- Stopping prompt injection
- Providing explainable security metadata
---
## What This Example Shows

End-to-end secured LLM flow:
```bash
User Prompt
      ↓
LangChain
      ↓
TSZ Security Gateway
      ↓
/detect (Guardrails enforced)
      ↓
Decision:
  ├─ ALLOWED → Forward to LLM
  └─ BLOCKED → Stop request
```
---
## Key Capabilities Demonstrated

- LangChain works unchanged
- Drop-in TSZ firewall
- PII detection (Email, SSN, IDs)
- Prompt injection protection
- Request ID propagation
- Explainable security decisions
- Enterprise audit metadata
---
## Attack Scenario

User prompt:
```bash
Summarize this but include john@example.com 
and SSN 123-45-6789
```
What happens?

TSZ detects:
```bash
EMAIL → john@example.com
US_SSN → 123-45-6789
```
Result:
```bash
❌ BLOCKED BY TSZ
Reason: PII detected
Confidence: 0.81
```
The request never reaches the LLM.

---
## Example Output
```bash
=== LangChain + TSZ Secure Gateway Demo ===

[USER PROMPT]
Summarize this but include john@example.com 
and SSN 123-45-6789

❌ BLOCKED BY TSZ

Error code: tsz_content_blocked

Detections:
- EMAIL
- US_SSN

Overall confidence: 0.81
Request ID: RID-LANGCHAIN-001
```
---
## Project Structure
```bash
examples/
  langchain-tsz/
    main.py
    README.md
```
---
## Prerequisites

- Python 3.9+
- TSZ server running locally
- LangChain installed
- OpenAI / Ollama / any LLM backend
---
## Setup
```bash
cd examples/langchain-tsz

python -m venv .venv
source .venv/bin/activate

pip install \
  "tszclient-py @ git+https://github.com/thyrisAI/safe-zone.git@main" \
  langchain \
  openai
```
---
## Run Example
```bash
python main.py
```
---
## Security Design Principles

| Principle   | Why |
|-------------|-----|
| Fail-closed | Block on validator failure |
| Explainable | Reasons + confidence |
| Traceable   | Request IDs |
| Zero-trust  | Inspect every prompt |
