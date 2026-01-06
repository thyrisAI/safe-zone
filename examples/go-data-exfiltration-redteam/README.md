# TSZ Data Exfiltration Red-Team Demo (Go)

This example demonstrates how **TSZ (Thyris Safe Zone)** defends LLM applications against **data exfiltration attacks**, one of the most critical and realistic risks in production AI systems.

It simulates real-world attacker behavior and shows how TSZ blocks unsafe requests **before they ever reach the LLM**, using both **detection-based** and **policy-based** security controls.

This is not a toy demo — it mirrors how TSZ is deployed in real production security pipelines.

---

## What This Example Shows

**End-to-end LLM safety flow:**

Attacker Prompt
↓
/detect (TSZ guardrails enforced)
↓
Decision: ALLOWED or BLOCKED
↓
LLM execution (only if allowed)


---

## Key Capabilities Demonstrated

- Data exfiltration prevention
- PII detection (EMAIL, CREDIT_CARD)
- Policy-based intent blocking
- Guardrail enforcement
- Request ID propagation
- Confidence scoring
- Explainable security decisions
- Safe LLM gating (LLM never called if blocked)

---

## Attack Scenarios Covered

### 1. Direct PII Exfiltration
Attempts to directly extract sensitive data such as:
- Email addresses
- Credit card numbers

### 2. Disguised Summary Exfiltration
Attempts to leak sensitive data indirectly via:
- Summaries
- Reformatted responses
- “Harmless-looking” prompts

### 3. Tool-Based Exfiltration
Attempts to exfiltrate data via:
- Structured outputs
- Tool calls
- Indirect instructions

### 4. Compliance / Policy Bypass
Attempts to bypass safeguards using:
- “Compliance” language
- Authority framing
- Justification-based attacks

---

## Understanding TSZ Blocking Decisions

TSZ can block unsafe requests in **two fundamentally different ways**.
This example makes the distinction explicit.

---

### Detection-Based Blocking

```bash
[BLOCK_SOURCE] DETECTION
[REASONS] EMAIL, CREDIT_CARD
```

**What this means:**
- Concrete sensitive data was detected
- TSZ can point to exact detection categories
- Ideal for compliance, audit logs, and investigations

---

### Policy / Validator-Based Blocking

```bash
[BLOCK_SOURCE] POLICY_VALIDATOR
[REASONS] DATA_EXFILTRATION_POLICY
```


**What this means:**
- No explicit PII span is required
- TSZ identified unsafe intent
- The request is blocked proactively
- Prevents exfiltration before data exposure occurs

This distinction is critical in real security systems and is often misunderstood.
This demo makes it clear and observable.

---

## Project Structure

```bash
examples/
go-data-exfiltration-redteam/
main.go # Complete red-team demo (single-file example)
README.md # Documentation
```


> The example is intentionally implemented in a **single Go file** to make it easy to read, run, and adapt.

---

## Prerequisites

- Go 1.20+
- TSZ server running locally
- Safe Zone repository cloned
- Upstream dependencies available via `go mod`

---

## Running the Demo

### 1. Start TSZ Server

From the repository root:

```bash
go run main.go
```

(or via Docker, depending on your setup)

TSZ should be running on:

```bash
http://localhost:8080
```

### 2. Run the Red-Team Example

```bash
cd examples/go-data-exfiltration-redteam
export TSZ_BASE_URL=http://localhost:8080
go run main.go
```

## Example Output

### Detection-Based Block
```bash
[ATTACK] Direct PII exfiltration
[STATUS] BLOCKED
[BLOCK_SOURCE] DETECTION
[REASONS] [EMAIL CREDIT_CARD]
[CONFIDENCE] 0.84
[LLM] ❌ Not executed (blocked by TSZ)
```

### Policy-Based Block
```bash
[ATTACK] Tool-based exfiltration
[STATUS] BLOCKED
[BLOCK_SOURCE] POLICY_VALIDATOR
[REASONS] [DATA_EXFILTRATION_POLICY]
[CONFIDENCE] 1.00
[LLM] ❌ Not executed (blocked by TSZ)
```

## Why This Matters

Data exfiltration is one of the highest-impact risks in LLM systems.

This example shows how TSZ:

- Detects real sensitive data
- Understands malicious intent
- Enforces guardrails consistently
- Prevents unsafe LLM execution
- Provides explainable, auditable decisions
