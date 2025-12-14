# Security Policy

TSZ (Thyris Safe Zone) is designed to be used in security‑sensitive environments, including financial services and privacy‑critical workloads. We take security very seriously and appreciate responsible disclosure of vulnerabilities.

---

## Supported Versions

This project is in active development. Until a formal versioning and release process is documented (see `ROADMAP.md`, Phase 7), we generally only provide security fixes on the latest `main` branch and the most recent tagged releases (if any).

If you are running TSZ in production and have specific support requirements, please reach out to the maintainers.

---

## Reporting a Vulnerability

If you believe you have found a security vulnerability in TSZ, **please do not open a public GitHub issue or discuss it in public channels.**

Instead, contact the maintainers privately so we can investigate and remediate the issue responsibly.

### How to Report

Please send an email with details of the vulnerability to:

- **security@thyris.ai** (preferred)
- Or, if that is unavailable, **support@thyris.ai**

Include as much information as possible to help us reproduce and understand the issue:

- A clear description of the vulnerability and its potential impact
- The version/commit of TSZ you are using
- Configuration details relevant to the issue (with any secrets removed)
- Steps to reproduce, including example requests, payloads or scripts if applicable

We will acknowledge receipt of your report as soon as possible, typically within a few business days.

---

## Vulnerability Handling Process

When a vulnerability report is received:

1. We will **confirm receipt** of the report and may ask for additional information if needed.
2. We will **investigate** the issue, assess its severity, and determine the impact.
3. We will **develop and test a fix**, including regression tests where appropriate.
4. We will **coordinate disclosure**, which may include:
   - Publishing a new release
   - Updating documentation and configuration guidance
   - Issuing a security advisory or changelog entry

We aim to keep reporters informed of our progress throughout this process.

---

## Scope and Expectations

Please focus your testing on:

- TSZ’s HTTP APIs and configuration
- The way TSZ handles and stores data
- Authentication, authorization and isolation boundaries around TSZ

Out of scope:

- Denial of Service (DoS) attacks based solely on overwhelming the system with traffic
- Vulnerabilities in third‑party dependencies that are not exploitable through TSZ
- Social engineering of maintainers or users

If you are unsure whether an issue is in scope, report it privately anyway — we would rather hear about a potential issue than miss a real one.

---

## Responsible Disclosure

We respectfully ask that you:

- Give us a reasonable amount of time to investigate and fix the issue before any public disclosure.
- Avoid accessing, modifying or destroying data that does not belong to you.
- Comply with applicable laws when testing and reporting vulnerabilities.

Thank you for helping us keep TSZ and its users safe.
