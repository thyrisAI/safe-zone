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

## Contributing

We welcome community contributions.

- Please read our [Contributing Guide](CONTRIBUTING.md) for details on how to set up a development environment, run tests and propose changes.
- By participating in this project, you agree to follow our [Code of Conduct](CODE_OF_CONDUCT.md).
- For reporting security issues, **do not** open a public GitHub issue. Instead, follow the process described in our [Security Policy](SECURITY.md).

---

## License

This project is licensed under the **Apache License, Version 2.0**. See the [LICENSE](LICENSE) file for the full text.

Unless otherwise noted, all contributions to this repository are also licensed under the Apache License 2.0.
