# TSZ Audit Logging & SIEM Export Example (Python)

This example demonstrates how **Thyris Safe Zone (TSZ)** can be used as a
centralized **audit and security decision engine** for LLM applications.

Every blocked request produces a structured audit event suitable for
security reviews, compliance, and SIEM ingestion.

---

## What This Example Shows

End-to-end audit flow:

User input  
→ TSZ `/detect`  
→ Security decision (BLOCKED / ALLOWED)  
→ Structured audit log  
→ SIEM-ready JSON export

---

## Security Signals Captured

- Request ID (X-TSZ-RID)
- Blocked vs Allowed decision
- Block source:
  - Detection
  - Policy validator
- Reasons (EMAIL, CREDIT_CARD, PROMPT_INJECTION, etc.)
- Confidence score
- Timestamp

---

## Why This Matters

Security teams need **evidence**, not just demos.

This example shows how TSZ:
- Produces explainable decisions
- Supports audit trails
- Integrates cleanly with SOC tooling
- Helps justify TSZ adoption internally

---

## Project Structure

```bash
examples/
audit-logging/
main.py
README.md
```


---

## Prerequisites

- Python 3.9+
- TSZ running locally at `http://localhost:8080`
- TSZ Python client installed

---

## Setup

```bash
cd examples/audit-logging/python
python -m venv .venv
source .venv/bin/activate
pip install "tszclient-py @ git+https://github.com/thyrisAI/safe-zone.git@main"
```

## Run the Example

```bash
python main.py
```

This will:

- Execute multiple attack scenarios
- Generate audit events
- Write audit_log.json

## Example Output

```bash
[ATTACK] PII exfiltration
[REQUEST_ID] RID-AUDIT-91fdc2ab
[STATUS] BLOCKED
[BLOCK_SOURCE] DETECTION
[REASONS] ['EMAIL']
[CONFIDENCE] 0.77
```

## SIEM Integration

The generated audit_log.json can be:

- Shipped via Fluent Bit / Filebeat
- Indexed in Splunk or Elastic
- Forwarded to any JSON-capable SIEM
- No custom parsing required.