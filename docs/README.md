# Welcome to TSZ (Thyris Safe Zone)

TSZ (Thyris Safe Zone) is an enterprise‑grade **PII Detection and Guardrails Gateway** developed by **Thyris.AI**. It acts as a zero‑trust, policy‑driven layer between your applications and external systems such as LLMs, SaaS APIs and third‑party services.

This `docs/` directory is the **canonical documentation hub** for TSZ. It is designed to be customer‑facing and enterprise‑ready.

---

## Who Is This For?

- **Application & Platform Engineers** integrating TSZ into microservices, APIs or AI pipelines
- **Security & Compliance Teams (CISO, DPO, Risk)** evaluating data protection and control guarantees
- **Solutions Architects & SREs** responsible for deployment, observability and reliability

---

## Documentation Map

The documentation set is organized as follows:

- **What is TSZ?** – Conceptual overview and core value proposition  
  `WHAT_IS_TSZ.md`

- **Quick Start Guide** – Install, configure and call TSZ in under 10 minutes  
  `QUICK_START.md`

- **API Reference (Enterprise)** – Full, production‑grade REST API reference  
  `API_REFERENCE.md`

- **Architecture & Security Overview** – Technical architecture, data flows and security controls  
  `ARCHITECTURE_SECURITY.md`

- **Postman Collection** – Ready‑to‑use collection for exploring the API  
  `TSZ_Postman_Collection.json`

- **Go Client (tszclient-go)** – Lightweight Go SDK for `/detect` and the LLM gateway  
  `../pkg/tszclient-go/README.md`

- **Python Client (tszclient_py / tszclient-py)** – Lightweight Python helper for `/detect` and the LLM gateway  
  `../examples/python-sdk-demo/main.py` and `pkg/tszclient_py/`

You can also view the high‑level marketing/overview document at the repository root:

- `PRODUCT_OVERVIEW.md`

For repository‑level information (license, contributing, security policy), see the root of the repository.

---

## Getting Started

1. **Read:** `WHAT_IS_TSZ.md` for a conceptual understanding.
2. **Deploy:** Follow `QUICK_START.md` to run TSZ locally using Docker.
3. **Explore:** Import `TSZ_Postman_Collection.json` into Postman and call the `/detect` endpoint.
4. **Integrate:** Use `API_REFERENCE.md` to wire TSZ into your applications and LLM/AI stack.

---

## Feedback & Support

For commercial support, integration help, or security reviews:

- **Website:** https://thyris.ai  
- **Email:** support@thyris.ai

If you are evaluating TSZ in a POC, we recommend starting with:

1. `WHAT_IS_TSZ.md`
2. `ARCHITECTURE_SECURITY.md`
3. `QUICK_START.md`
4. `API_REFERENCE.md`

---

## Open Source & Governance

TSZ is released as open source under the **Apache License 2.0**.

At the repository root you will find:

- `LICENSE` – full license text (Apache 2.0)
- `CONTRIBUTING.md` – how to set up a dev environment, run tests and contribute
- `CODE_OF_CONDUCT.md` – community standards and expected behavior
- `SECURITY.md` – vulnerability disclosure and security contact details

Please refer to those documents before contributing or reporting security issues.
