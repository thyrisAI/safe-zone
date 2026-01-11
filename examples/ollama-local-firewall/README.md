# Ollama Local Firewall (Gemma 3 + TSZ)

Secure your **local LLMs** using TSZ firewall.

This example protects **Gemma 3 running on Ollama** using TSZ guardrails.

---

## Architecture

```bash
User → TSZ Gateway → Ollama (Gemma 3)
```

TSZ inspects:
- PII
- Sensitive patterns
- Prompt injection

---

## Features

- Local model security
- OpenAI compatible API
- Explainable blocking
- Request IDs
- Enterprise guardrails

---

## Prerequisites

### Install Ollama
```bash
brew install ollama
ollama serve
```
### Pull Gemma 3 or Model of your choice
```bash
ollama pull gemma:3b
```
---
## Start TSZ

```bash
docker compose up -d
```
---
## Setup Python
```bash
cd examples/ollama-local-firewall

python -m venv .venv
source .venv/bin/activate

pip install \
  openai \
  "tszclient-py @ git+https://github.com/thyrisAI/safe-zone.git@main"
```
---
## Run
```bash
python main.py
```
---
## Example Output
```bash
❌ REQUEST BLOCKED BY TSZ
Content blocked by security policy: EMAIL
```
---
## Why this matters

- Secure local LLMs
- No cloud dependency
- Production security controls
- Prevents data leakage
- Full observability
---
## Security Principles

| Principle     | Why |
|---------------|-----|
| Fail-closed   | Block immediately if any validator fails |
| Explainable   | Always return reasons and confidence scores |
| Traceable     | Every request has a unique Request ID (RID) |
| Zero-trust    | Inspect **every** prompt, no implicit trust |
---
## Use cases

- Offline AI security
- On-prem LLMs
- Privacy-first deployments
- Enterprise compliance
