# TSZ LLM Data Exfiltration Red-Team Playground (Python)

This example demonstrates how **TSZ (Thyris Safe Zone)** prevents
**LLM-based data exfiltration attacks** before any language model is executed.

It simulates realistic attacker behavior where prompts attempt to extract
or leak sensitive information (PII) such as emails and credit card numbers,
and shows how TSZ blocks these attempts deterministically.

This is **not a toy demo** — it mirrors how TSZ is used in
**production security pipelines** for mission-critical AI systems.

---

## What This Example Shows

An end-to-end **LLM data safety flow**:

User input + exfiltration attempt
↓
/detect (TSZ scans & enforces guardrails)
↓
Decision: ALLOWED or BLOCKED
↓
LLM execution only if allowed


TSZ acts as a **hard security boundary** in front of the LLM.

---

## Key Capabilities Demonstrated

- Data exfiltration prevention (PII)
- Deterministic blocking before LLM execution
- Guardrail enforcement vs detection
- Request ID propagation
- Confidence scoring
- Explainable security decisions
- Zero-trust LLM execution model

---

## Attack Categories Covered

### 1. Direct Data Exfiltration
Explicit attempts to extract private data:
- Emails
- Credit card numbers

### 2. Disguised Exfiltration
Attempts to leak sensitive data indirectly:
- Summarization requests
- “Helpful” reformulations
- Masked instructions

### 3. Tool-Based Exfiltration
Attempts to move sensitive data outside the system:
- Sending data to webhooks
- External system calls

### 4. Compliance / Authority Bypass
Using roles or authority to override safeguards:
- “Compliance audit”
- “System review”
- “Ignore privacy rules”

---

## Understanding TSZ Blocking Decisions

TSZ can block requests **before** LLM execution in different ways.
This example focuses on **detection-based blocking**, which is critical
for data protection.

---

### Detection-Based Blocking

```bash
[BLOCK_SOURCE] DETECTION
[REASONS] EMAIL, CREDIT_CARD
```


- Concrete sensitive data is detected
- TSZ identifies exact data categories
- Ideal for audits and compliance
- Deterministic and explainable

In data exfiltration scenarios, **detection-based blocking is preferred**
because the risk is explicit and measurable.

---

## Why This Matters

LLMs should **never** decide whether sensitive data is allowed to leave
a system.

TSZ enforces:

- Policy-driven security
- Deterministic decisions
- Pre-LLM enforcement
- Auditable outcomes

This makes TSZ suitable for:
- Enterprise copilots
- Internal AI tools
- Regulated environments
- Agent-based systems

---

## Project Structure

```bash
examples/
llm-data-exfiltration-redteam-python/
main.py # Orchestrates attack execution
attacks.py # Exfiltration attack definitions
utils.py # Reporting & output formatting
README.md
```


---

## Prerequisites

- Python 3.9+
- Docker & Docker Compose
- TSZ running locally
- Official TSZ Python client

---

## Setup & Installation

Create a virtual environment and install dependencies:

```bash
cd examples/llm-data-exfiltration-redteam-python
python -m venv .venv
source .venv/bin/activate
pip install "tszclient-py @ git+https://github.com/thyrisAI/safe-zone.git@main"
```

## Running the Playground

```bash
python main.py
```

## Example Output

Data Exfiltration (Detection-Based Block)

```bash
[ATTACK] Direct PII exfiltration
[REQUEST_ID] RID-EXFIL-f0a58f79
[STATUS] BLOCKED
[BLOCK_SOURCE] DETECTION
[REASONS] CREDIT_CARD, EMAIL
[CONFIDENCE] 0.60
[LLM] ❌ Not executed (blocked by TSZ)
```