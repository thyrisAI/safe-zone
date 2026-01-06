# TSZ LLM Red-Team Playground (Go)

This example demonstrates how **TSZ (Thyris Safe Zone)** protects LLM-based
applications written in **Go** against real-world **red-team attacks** such as
prompt injection and control-flow manipulation.

It acts as an interactive **red-team vs defense playground**, showing:

- how adversarial prompts are evaluated
- how TSZ guardrails are enforced
- why unsafe requests are blocked by policy even without explicit PII spans

This is **not a toy demo** — it mirrors how TSZ is used in
**production security pipelines** for backend and gateway services.

---

## What This Example Shows

### End-to-End LLM Safety Flow

User input + attack

↓

/detect (TSZ guardrails enforced)

↓

Decision: ALLOWED or BLOCKED

↓

LLM execution only if allowed


The LLM is **never executed** unless TSZ explicitly allows the request.

---

## Key Capabilities Demonstrated

- Prompt injection detection & blocking
- Policy / validator-based blocking
- Request ID propagation
- Confidence scoring
- Explainable security decisions
- Safe LLM gating for Go services

---

## Attack Categories Covered

### Prompt Injection Attacks

- Simple instruction override
- Recursive instruction injection
- Role-based system override
- Multi-turn memory poisoning

This example intentionally focuses on **control-flow and intent-based attacks**.
Concrete PII leakage is covered separately in the data-exfiltration examples.

---

## Understanding TSZ Blocking Decisions

TSZ can block requests in **two different ways**.
This playground makes the distinction explicit.

---

### Policy / Validator-Based Blocking

```bash
[BLOCK_SOURCE] POLICY_VALIDATOR
[REASONS] PROMPT_INJECTION_POLICY
```


- Unsafe intent or control-flow detected
- No specific sensitive text span required
- TSZ is confident the request is unsafe

This is critical for defending against prompt-injection attacks that do not
contain concrete PII.

---

### Detection-Based Blocking (not shown here)

Detection-based blocking is demonstrated in the
**data-exfiltration red-team examples**, where concrete sensitive data such as
emails or credit-card numbers are detected.

---

## Project Structure

```bash
examples/
go-llm-redteam-playground/
main.go # Single-file red-team playground
README.md # Documentation
```


The Go version is intentionally implemented as a **single-file example** to keep
it easy to read, run, and adapt.

---

## Prerequisites

- Go 1.21+
- TSZ running locally

---

## Running the Playground

Ensure TSZ is running locally (default port `8080`).

Optionally set the base URL:

```bash
export TSZ_BASE_URL=http://localhost:8080
```

Run the example:

```bash
cd examples/go-llm-redteam-playground
go run .
```

## Example Output

```bash
[ATTACK] Recursive instruction override
[REQUEST_ID] RID-GO-REDTEAM-002
[STATUS] BLOCKED
[BLOCK_SOURCE] POLICY_VALIDATOR
[REASONS] PROMPT_INJECTION_POLICY
[CONFIDENCE] 1.00
[LLM] ❌ Not executed (blocked by TSZ)
```

## Why This Example Matters

This playground demonstrates that:

- TSZ prevents unsafe control-flow before LLM execution
- Red-team attacks can be blocked without relying on PII detection
- Security decisions are explainable and auditable
- TSZ works consistently in Go-based production systems