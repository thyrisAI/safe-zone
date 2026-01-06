# TSZ LLM Red-Team Playground (Python)

This example demonstrates how **TSZ (Thyris Safe Zone)** defends LLM applications against real-world attacks such as **prompt injection** and **data exfiltration**.

It acts as an interactive **red-team vs defense playground**, showing:
- how attacks are detected
- how guardrails are enforced
- why some attacks are blocked by policy even without explicit spans

This is **not a toy demo** — it mirrors how TSZ is used in production security pipelines.

---

## What This Example Shows

End-to-end LLM safety flow:

User input + attack
↓
/detect (TSZ guardrails enforced)
↓
Decision: ALLOWED or BLOCKED
↓
LLM execution only if allowed

Key capabilities demonstrated:

- Prompt injection detection & blocking
- Data exfiltration prevention (PII)
- Guardrail enforcement vs detection
- Request ID propagation
- Confidence scoring
- Explainable security decisions

---

## Attack Categories Covered

### 1. Prompt Injection Attacks
- Simple prompt injection
- Recursive instruction override
- Role-based system override
- Multi-turn memory poisoning

### 2. Data Exfiltration Attacks
- Attempts to extract emails
- Attempts to leak credit card numbers

---

## Understanding TSZ Blocking Decisions

TSZ can block requests in **two different ways**.  
This example makes that distinction explicit.

### Detection-Based Blocking

```bash
[BLOCK_SOURCE] DETECTION
[REASONS] EMAIL, CREDIT_CARD
```

- Concrete sensitive data was found
- TSZ can point to exact detection types
- Ideal for audit logs and compliance

### Policy / Validator-Based Blocking

```bash
[BLOCK_SOURCE] POLICY_VALIDATOR
[REASONS] PROMPT_INJECTION_SIMPLE
```

- Unsafe intent or control flow detected
- No specific text span required
- TSZ is confident the request is unsafe

This distinction is critical in real security systems and is often misunderstood. The playground shows it clearly.

## Project Structure

```bash
examples/
  llm-redteam-playground-python/
    main.py        # Orchestrates attack phases
    attacks.py     # Attack definitions
    runner.py      # TSZ detect + guardrail enforcement
    utils.py       # Reporting and output formatting
```

## Prerequisites

- Python 3.9+
- TSZ running locally
- Python client installed

## Setup & Installation

Create a virtual environment and install the official TSZ Python client:

```bash
cd examples/llm-redteam-playground-python
python -m venv .venv
source .venv/bin/activate
pip install "tszclient-py @ git+https://github.com/thyrisAI/safe-zone.git@main"
```

## Running the Playground

```bash
python main.py
```

## Example Output

Prompt Injection (Policy-Based Block)

```bash
[ATTACK] Recursive injection
[STATUS] BLOCKED
[BLOCK_SOURCE] POLICY_VALIDATOR
[REASONS] PROMPT_INJECTION_SIMPLE
[CONFIDENCE] 1.00
[LLM] ❌ Not executed (blocked by TSZ)
```

Data Exfiltration (Detection-Based Block)

```bash
[ATTACK] Data exfiltration attempt
[STATUS] BLOCKED
[BLOCK_SOURCE] DETECTION
[REASONS] EMAIL, CREDIT_CARD
[CONFIDENCE] 0.77
[LLM] ❌ Not executed (blocked by TSZ)
```