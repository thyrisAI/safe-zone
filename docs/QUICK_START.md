# TSZ Quick Start Guide

This guide helps you **deploy TSZ locally** and call the primary `/detect` endpoint in under 10 minutes.

TSZ is designed to run as a containerized microservice in your environment (Docker, Kubernetes, on‑prem, cloud). The steps below focus on a local Docker‑based setup.

For Kubernetes deployments, use the Helm chart in `deployment/helm/thyris-sz`
and follow `docs/DEPLOYMENT.md`.

If you already operate Envoy Gateway and want TSZ as an external guardrail
rather than an LLM proxy, use the BYG quick start below.

### BYG quick start

The repository includes a self-contained Kind environment with a pinned Envoy
Gateway v1.8.3, a local mock OpenAI-compatible upstream, PostgreSQL, Redis, and
TSZ. It uses no paid provider or real credential.

```bash
examples/bring-your-gateway/shared/run.sh \
  examples/bring-your-gateway/01-minimal-inspection
examples/bring-your-gateway/shared/run.sh \
  examples/bring-your-gateway/02-request-masking
examples/bring-your-gateway/shared/run.sh \
  examples/bring-your-gateway/03-request-blocking
```

The runner verifies the HTTP outcome and the content-safe mock-upstream
summary, including that masked PII is not forwarded and blocked requests never
reach the upstream. Remove the environment when finished:

```bash
deployments/envoy-gateway/kind-bootstrap.sh down
```

Production configuration, native `TSZGuardrailPolicy` attachment, TLS/mTLS,
failure policy, troubleshooting, and upgrade guidance are in
`integrations/ENVOY_GATEWAY.md`.

---

## 1. Prerequisites

- **Docker** and **Docker Compose** installed
- **Git** installed
- Optional: **Go 1.23+** if you want to run from source instead of Docker
- Optional: **Helm 3** and access to a Kubernetes cluster if you want to use the Helm chart

---

## 2. Clone the Repository

```bash
git clone https://github.com/thyrisAI/safe-zone.git
cd safe-zone
```

---

## 3. Configure Environment (Optional)

A default configuration is already provided in `docker-compose.yml` and `.env.example`.
If you want to override settings, copy the example file to `.env` (Docker Compose loads it automatically):

```bash
cp .env.example .env
```

Key environment variables:

```env
SERVER_PORT=8080
DB_DSN=postgres://postgres:postgres@localhost:5432/thyris?sslmode=disable&TimeZone=Europe/Istanbul
REDIS_URL=redis://:thyrisredis@localhost:6379/0

# Security middleware
SECURITY_HEADERS_ENABLED=true
CORS_ENABLED=true
MAX_REQUEST_SIZE_BYTES=10485760
HANDLER_TIMEOUT_DETECT_SECONDS=30
HANDLER_TIMEOUT_CHAT_SECONDS=300

# AuthN/AuthZ (set true in production)
AUTH_ENABLED=false
AUTH_REQUIRE_BEARER_TOKEN=true
AUTH_TOKEN_PERMISSIONS=token_detect=detect:read,token_admin=*
AUTH_PUBLIC_PATHS=/healthz,/ready

# AI Provider Configuration
# Options: OPENAI_COMPATIBLE (default) or BEDROCK
AI_PROVIDER=OPENAI_COMPATIBLE

# OpenAI-Compatible Provider (OpenAI, Azure OpenAI, Ollama, etc.)
AI_MODEL_URL=http://localhost:11434/v1
AI_API_KEY=ollama
AI_MODEL=llama3.1:8b

# AWS Bedrock Provider (only used when AI_PROVIDER=BEDROCK)
# AWS_BEDROCK_REGION=us-east-1
# AWS_BEDROCK_MODEL_ID=anthropic.claude-3-sonnet-20240229-v1:0
# AWS_BEDROCK_ENDPOINT_OVERRIDE=  # Optional: for VPC endpoints

# Confidence thresholds
CONFIDENCE_ALLOW_THRESHOLD=0.30
CONFIDENCE_BLOCK_THRESHOLD=0.85

# Optional admin API key for /admin endpoints
ADMIN_API_KEY=change-me-in-production
```

For a local test run, the defaults are usually sufficient. For production, you should:

- Change all secrets/passwords
- Configure TLS / API gateway in front of TSZ
- Tune thresholds according to your risk appetite

---

## 4. Start TSZ Using Docker Compose

From the repository root:

```bash
docker-compose up --build -d
```

If you are using Docker Compose v2, you can run:

```bash
docker compose up --build -d
```

This will start:

- **TSZ API server** on `http://localhost:8080`
- **PostgreSQL** (for patterns, allowlist/blocklist, validators)
- **Redis** (for AI confidence caching and fast lookups)

For Kubernetes instead of Docker Compose:

```bash
helm upgrade --install thyris-sz deployment/helm/thyris-sz \
  --namespace thyris-sz \
  --create-namespace \
  --set image.repository=ghcr.io/thyrisai/thyris-sz \
  --set image.tag=0.1.0
```

See `docs/DEPLOYMENT.md` for production-style values, external PostgreSQL and
Redis configuration, ingress, and secret handling.

You can check container status with:

```bash
docker ps
```

---

## 5. Verify the Deployment

### 5.1 Health Check

```bash
curl http://localhost:8080/healthz
```

Expected response:

```text
UP
```

### 5.2 Readiness Check

```bash
curl http://localhost:8080/ready
```

Expected response:

```text
READY
```

If you see `Database not ready` or `Redis not ready`, wait a few seconds and retry.

---

## 6. First Detection Call (cURL)

Call the `/detect` endpoint with a simple text:

```bash
curl -X POST http://localhost:8080/detect \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer token_detect" \
  -d '{
    "text": "Contact me at john@example.com regarding order #99281.",
    "rid": "RID-QUICKSTART-001",
    "expected_format": "FREE_TEXT",
    "guardrails": []
  }'
```

If `AUTH_ENABLED=false`, you can remove the `Authorization` header.

Example response (simplified):

```json
{
  "redacted_text": "Contact me at [EMAIL] regarding order #99281.",
  "detections": [
    {
      "type": "EMAIL",
      "value": "john@example.com",
      "placeholder": "[EMAIL]",
      "start": 14,
      "end": 30,
      "confidence_score": "0.87"
    }
  ],
  "breakdown": {
    "EMAIL": 1
  },
  "blocked": false,
  "contains_pii": true,
  "overall_confidence": "0.87"
}
```

---

## 7. Explore with Postman

The repository includes a ready‑to‑use Postman collection:

- `docs/TSZ_Postman_Collection.json`

### 7.1 Import the Collection

1. Open **Postman**.
2. Click **Import**.
3. Select `docs/TSZ_Postman_Collection.json`.
4. A collection named **"TSZ – Thyris Safe Zone API (Enterprise – FULL)"** will appear.

### 7.2 Try the Detect Scenarios

Recommended first requests:

- **Detect – Clean Input (Minimal)**
- **Detect – Single EMAIL (Full Fields)**
- **Detect – AI Guardrail (Toxic Language) + PII**

Then explore:

- **Patterns** – create custom regex patterns
- **Allowlist / Blocklist** – manage trusted/forbidden values
- **Validators** – define AI guardrails like `TOXIC_LANGUAGE`
- **Templates** – import pre‑packaged guardrail sets
- **System** – check health, readiness and reload cache

Set these Postman collection variables before running protected endpoints:

- `base_url` (example: `http://localhost:8080`)
- `tsz_bearer_token` (example: `token_admin` when auth is enabled)

---

## 8. Basic LLM Integration Example

Below is a minimal Python example showing how to integrate TSZ in front of an LLM provider (e.g. OpenAI):

```python
import requests
import openai

TSZ_URL = "http://localhost:8080/detect"
OPENAI_MODEL = "gpt-4"

user_input = "My credit card is 4111 1111 1111 1111, can you save it?"

# 1) Send user input to TSZ
security_check = requests.post(TSZ_URL, json={
    "text": user_input,
    "rid": "RID-PY-001",
    "expected_format": "FREE_TEXT",
    "guardrails": ["TOXIC_LANGUAGE"]
})

result = security_check.json()

if result.get("blocked"):
    raise Exception(result.get("message", "Unsafe content detected by TSZ"))

safe_text = result.get("redacted_text", user_input)

# 2) Call the LLM with redacted input
response = openai.ChatCompletion.create(
    model=OPENAI_MODEL,
    messages=[{"role": "user", "content": safe_text}]
)

print(response.choices[0].message["content"])
```

---

## 9. Using the Go Client (tszclient-go)

If you are building Go services, you can integrate TSZ via the Go client instead
of calling the HTTP APIs manually.

### 9.1 Install & Configure

Inside this repository:

```go
import tszclient "github.com/thyrisAI/safe-zone/pkg/tszclient-go"

client, err := tszclient.New(tszclient.Config{
    BaseURL: "http://localhost:8080", // TSZ gateway URL
})
if err != nil {
    // handle error
}
```

### 9.2 Example – Call /detect from Go

```go
ctx := context.Background()

resp, err := client.Detect(ctx, tszclient.DetectRequest{
    Text:       "Contact me at john@example.com",
    RID:        "RID-GO-QUICKSTART-001",
    Guardrails: []string{"TOXIC_LANGUAGE"},
})
if err != nil {
    log.Fatalf("detect failed: %v", err)
}

if resp.Blocked {
    log.Printf("request blocked by TSZ: %s", resp.Message)
} else {
    log.Printf("redacted text: %s", resp.RedactedText)
}
```

For more details, see:

- `pkg/tszclient-go/README.md`
- `examples/go-detect` and `examples/go-llm-gateway`

---

## 10. Using the Python Client (tszclient_py / tszclient-py)

If you are building Python services, you can use the lightweight Python client
instead of calling the HTTP APIs manually.

### 10.1 Install

Install directly from this GitHub repository (from the `main` branch):

```bash
pip install "tszclient-py @ git+https://github.com/thyrisAI/safe-zone.git@main"
```

### 10.2 Example – Call /detect and the LLM gateway from Python

A runnable example is provided at `examples/python-sdk-demo/main.py`. In
summary, the usage pattern looks like this:

```python
from tszclient_py import TSZClient, TSZConfig, ChatCompletionRequest

client = TSZClient(TSZConfig(
    base_url="http://localhost:8080",
    api_key="token_admin",  # optional; needed when AUTH_ENABLED=true
))

# /detect example
resp = client.detect_text(
    "Contact me at john@example.com",
    rid="RID-QUICKSTART-PY-001",
    guardrails=["TOXIC_LANGUAGE"],
)
print("Redacted:", resp.redacted_text)

# LLM gateway example
chat_req = ChatCompletionRequest(
    model="llama3.1:8b",
    messages=[{"role": "user", "content": "Hello via TSZ gateway (Python)"}],
)
llm_resp = client.chat_completions(chat_req)
print(llm_resp["choices"][0]["message"]["content"])
```

For a full working demo (including headers, RIDs and guardrails), see:

- `examples/python-sdk-demo/main.py`

If auth is enabled in TSZ, set `TSZ_AUTH_TOKEN` before running the demo:

```bash
export TSZ_AUTH_TOKEN=token_admin
python -m examples.python-sdk-demo.main
```

---

## 11. Using the CLI Tool (tsz)

You can use the `tsz` command-line tool to interact with the API without writing code.

### 11.1 Build/Install

```bash
cd pkg/tsz-cli
go build -o tsz
# On Windows: tsz.exe
```

### 11.2 Run a Scan

```bash
./tsz scan --text "My email is user@example.com"
```

Output:

```json
{
  "redacted_text": "My email is [EMAIL]",
  "detections": [...]
}
```

For more commands (managing patterns, lists, templates), see `pkg/tsz-cli/README.md`.

---

## 12. Next Steps

From here, you can:

- Read the full **API reference** in `API_REFERENCE.md` for all endpoints.
- Review `ARCHITECTURE_SECURITY.md` for architecture, data flows and security considerations.
- Customize patterns, validators and templates to match your organization’s policies.

If you run into issues during Quick Start:

- Verify Docker containers are running (`docker ps`).
- Check TSZ logs (`docker-compose logs tsz` or application logs).
- Confirm DB and Redis are reachable and healthy.

For commercial support or architecture reviews, contact **open-source@thyris.ai**.
