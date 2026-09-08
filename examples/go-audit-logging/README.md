# TSZ Audit Logging & SIEM Export (Go)

This example demonstrates how **TSZ (Thyris Safe Zone)** produces
**security-grade audit logs** for blocked requests — without invoking any LLM.

It is designed for **security teams, SOC analysts, and compliance workflows**.

This is not a toy demo — it mirrors how TSZ is used in production environments
to generate evidence for audits, incident response, and SIEM pipelines.

---

## What This Example Shows

End-to-end **security decision flow** (no LLM involved):

User input  
v  
`/detect` (TSZ guardrails enforced)  
v  
Decision: **BLOCKED / ALLOWED**  
v  
Audit log exported for SIEM

---

## Key Capabilities Demonstrated

- Request ID propagation
- Detection-based vs policy-based blocking
- Block reasons & confidence scores
- Explainable security decisions
- JSON audit log export
- SIEM-ready output format

---

## Attack Scenarios Covered

### 1. PII Exfiltration
Attempts to extract sensitive information such as:
- Email addresses
- Personal data

Blocked via **DETECTION**.

---

### 2. Prompt Injection
Attempts to override system behavior or bypass safeguards.

Blocked via **POLICY VALIDATOR**.

---

## Understanding TSZ Blocking Decisions

TSZ can block requests in **two distinct ways**.

### Detection-Based Blocking

```bash
[BLOCK_SOURCE] DETECTION
[REASONS] EMAIL, CREDIT_CARD
```


- Concrete sensitive data detected
- Ideal for compliance and audit evidence
- Fully explainable

---

### Policy / Validator-Based Blocking

```bash
[BLOCK_SOURCE] POLICY_VALIDATOR
[REASONS] PROMPT_INJECTION_POLICY
```

- Unsafe intent detected
- No explicit text span required
- High confidence security decision

This distinction is critical for enterprise security systems.

---

## Project Structure

```bash
examples/
go-audit-logging/
main.go
README.md
```

---

## Prerequisites

- Go 1.21+
- TSZ running locally
- PostgreSQL + Redis (via Docker)

---

## Running the Example

### Start TSZ
```bash
docker compose -f deployment/docker/docker-compose.yml up --build
```

## Run the audit demo
```bash
cd examples/go-audit-logging
export TSZ_BASE_URL=http://localhost:8080
go run main.go
```

## Output Example
```bash
[ATTACK] PII exfiltration
[STATUS] BLOCKED
[BLOCK_SOURCE] DETECTION
[REASONS] [EMAIL]
[CONFIDENCE] 0.90
```

An audit_log.json file is generated and ready for SIEM ingestion.

## SIEM Compatibility

The generated JSON can be ingested directly into:

- Splunk
- Elastic / OpenSearch
- Datadog
- Chronicle
- Azure Sentinel

## Summary

This example shows how TSZ:

- Enforces guardrails
- Produces explainable decisions
- Generates audit-ready security logs
- Integrates cleanly into SOC workflows
