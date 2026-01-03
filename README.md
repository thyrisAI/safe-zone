# TSZ (Thyris Safe Zone)

TSZ (Thyris Safe Zone) is a PII Detection and Guardrails System engineered by **Thyris.AI**. It acts as a zero‑trust layer between your data and external systems, ensuring that sensitive information—Personal Identifiable Information (PII), secrets, and proprietary data—never leaves your secure perimeter unintentionally.

TSZ provides real‑time scanning, redaction, and blocking capabilities so that you can safely integrate LLMs and third‑party APIs into your existing applications.

---

## Features

- Real‑time detection of PII, secrets and sensitive patterns
- Redaction with context‑preserving placeholders (for example, `[EMAIL]`, `[CREDIT_CARD]`)
- Configurable guardrails using patterns, validators and templates
- Allowlist and blocklist management
- Hot reloading of rules via APIs
- High‑performance implementation in Go with Redis caching
- **Native AWS Bedrock integration** – Use Anthropic Claude, Amazon Titan, Meta Llama, Mistral, and Cohere models directly
- **Multi-provider AI support** – OpenAI-compatible endpoints (OpenAI, Azure OpenAI, Ollama) and AWS Bedrock
- **OpenAI-compatible LLM gateway** – Drop-in replacement for OpenAI API with built-in guardrails
- **CLI Tool** – Full management and scanning from the command line (`pkg/tsz-cli`)

---

## Getting Started

For all user and customer‑facing documentation, see the `docs/` directory:

- **What is TSZ?** – Conceptual and product overview  
  `docs/WHAT_IS_TSZ.md`
- **Product Overview (executive friendly)** –  
  `docs/PRODUCT_OVERVIEW.md`
- **Quick Start Guide** – Run TSZ locally and call `/detect`  
  `docs/QUICK_START.md`
- **API Reference (Enterprise)** – Full REST API documentation  
  `docs/API_REFERENCE.md`
- **Architecture & Security Overview** – Architecture, data flows, security controls  
  `docs/ARCHITECTURE_SECURITY.md`
- **Postman Collection** – Ready‑to‑use collection  
  `docs/TSZ_Postman_Collection.json`

If you are evaluating TSZ for the first time, we recommend the following order:

1. `docs/WHAT_IS_TSZ.md`
2. `docs/PRODUCT_OVERVIEW.md`
3. `docs/QUICK_START.md`
4. `docs/API_REFERENCE.md`

For a more detailed map of the documentation set, see `docs/README.md`.

---

## Client Libraries (SDKs)

TSZ provides official client libraries for common stacks:

- **Go client (`tszclient-go`)** – for Go services that want a typed wrapper around `/detect` and the LLM gateway.  
  See: `pkg/tszclient-go/README.md`.

- **CLI (`tsz`)** – Command-line interface for scanning and administration.  
  See: `pkg/tsz-cli/README.md`.

- **Python client (`tszclient_py` / package `tszclient-py`)** – for Python services that prefer a small `requests`-based helper instead of calling HTTP manually.
  Install from GitHub:

  ```bash
  pip install "tszclient-py @ git+https://github.com/thyrisAI/safe-zone.git@main"
  ```

  A runnable example lives under `examples/python-sdk-demo`.

---

## Testing

TSZ includes a comprehensive test suite with 55+ tests covering unit, integration, and end-to-end scenarios:

```bash
# Run all tests
go test ./tests/... -v

# Run specific test suites
go test ./tests/unit/...        # Unit tests (no dependencies)
go test ./tests/integration/... # Integration tests (requires TSZ + DB + Redis)
go test ./tests/e2e/...         # End-to-end tests (full system)
```

**Test Coverage:**
- **Unit Tests (40+)**: Core business logic, AI providers, configuration, caching
- **Integration Tests (15+)**: API endpoints, error handling, concurrent requests
- **E2E Tests (5)**: Full system workflows, streaming, health checks

For detailed information about the test suite, see `tests/README.md`.

## Contributing

We welcome community contributions.

- Please read our [Contributing Guide](CONTRIBUTING.md) for details on how to set up a development environment, run tests and propose changes.
- By participating in this project, you agree to follow our [Code of Conduct](CODE_OF_CONDUCT.md).
- For reporting security issues, **do not** open a public GitHub issue. Instead, follow the process described in our [Security Policy](SECURITY.md).

---

## License

This project is licensed under the **Apache License, Version 2.0**. See the [LICENSE](LICENSE) file for the full text.

Unless otherwise noted, all contributions to this repository are also licensed under the Apache License 2.0.
