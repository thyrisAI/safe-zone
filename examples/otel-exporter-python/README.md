# TSZ + OpenTelemetry Observability Example (Python)

This example demonstrates how **Thyris Safe Zone (TSZ)** integrates with **OpenTelemetry** to provide **full observability** into LLM security events.

It shows how you can:

- Detect risky user inputs (PII, prompt injection)
- Allow requests to continue (monitoring mode)
- Export security telemetry to OpenTelemetry
- Send traces to any backend (Jaeger, Tempo, Datadog, etc.)

This mirrors **real production security rollouts** where teams **observe first** before enforcing blocks.

---

## What This Example Shows

End-to-end flow:
```bash
User input
↓

TSZ /detect
↓

Risk detected (PII / Injection)
↓

Decision: ALLOWED (monitoring mode)
↓

Security telemetry exported to OpenTelemetry
```


---

## Why ALLOWED Mode is Intentional

This example **does NOT block** requests on purpose.

This is **best practice** for:

- Initial deployments
- Security monitoring
- SOC analysis
- Audit readiness
- Enterprise rollouts

### Real-world strategy

| Phase | Mode |
|--------|------|
| Phase 1 | Observe (ALLOWED) |
| Phase 2 | Block high-risk |
| Phase 3 | Strict enforcement |

This is exactly how:

- WAFs
- API Gateways
- Cloud security tools  
are deployed.

---

## Security Capabilities Demonstrated

- PII detection (email, etc.)
- Prompt injection detection
- Request ID propagation
- Confidence scoring
- OpenTelemetry tracing
- Export to observability stack

---

## Project Structure

```bash
examples/
otel-exporter-python/
main.py # TSZ + OpenTelemetry integration
README.md
```


---

## Prerequisites

- Python 3.9+
- TSZ running locally
- OpenTelemetry packages installed

---

## Setup

### 1. Start TSZ

```bash
docker compose up -d
```

### 2. Start Jaeger (OpenTelemetry Backend)

```bash
docker run -d \
  --name jaeger \
  -p 16686:16686 \
  -p 4318:4318 \
  -p 4317:4317 \
  jaegertracing/all-in-one:latest

Open UI:
http://localhost:16686
```

### 3. Setup Python & Dependencies

```bash
cd examples/otel-exporter-python

python -m venv .venv
source .venv/bin/activate

pip install \
  "tszclient-py @ git+https://github.com/thyrisAI/safe-zone.git@main" \
  opentelemetry-sdk \
  opentelemetry-exporter-otlp
```

### 4. Run example

```bash
python main.py
```

### Example Output

```bash
TSZ + OpenTelemetry Demo (Python)

[ATTACK] PII exfiltration
[REQUEST_ID] RID-OTEL-681da2da
[STATUS] ALLOWED
[REASONS] ['EMAIL']
[CONFIDENCE] 0.60
--------------------------------------------------

[ATTACK] Prompt injection
[REQUEST_ID] RID-OTEL-f23d2133
[STATUS] ALLOWED
[REASONS] []
[CONFIDENCE] 0.00
--------------------------------------------------
```

### 5. View traces in Jaeger

Open:

```bash
http://localhost:16686
```

Search service:

```bash
tsz-audit-demo
```

You will see:

- Request IDs
- Attack type
- Status
- Confidence score
- Reasons

---

### What This Means

Detection worked

```bash
REASONS: ['EMAIL']
```
- TSZ successfully detected PII

Request allowed

```bash
STATUS: ALLOWED
```

- No blocking policy configured
- Monitoring mode enabled
- Request would proceed to LLM
---

### Telemetry exported

All events are exported to OpenTelemetry:

- Request ID
- Risk type
- Confidence score
- Decision

This allows:

- Dashboards
- Alerts
- SIEM ingestion
- SOC monitoring
---

### Why This Example Matters

Security teams care about:

- Visibility first
- Audit logs
- Explainability
- Non-breaking rollout

This demo shows:

"Security without breaking production"

---

### Production Use-Cases

- Security dashboards
- SIEM export
- Compliance reporting
- Attack trend analysis
- SOC monitoring

---

### Summary

This example proves:

- TSZ detects threats
- No false blocking
- Full observability
- Enterprise rollout pattern
- Works with any OTEL backend