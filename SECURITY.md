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

- **open-source@thyris.ai**

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
- BYG adapter protocol handling, policy attachment and route-policy authority
- Fail-open bypasses, streaming leakage, unsafe metadata and processor exposure

For the BYG threat model covering assets, actors, trust boundaries, threats,
mitigations, and residual risks, see
[docs/security/BYG_THREAT_MODEL.md](docs/security/BYG_THREAT_MODEL.md).
Adapter or policy-bypass vulnerabilities must be reported privately using the
same process above; do not include real prompts, credentials, or detected data
in a report.

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

---

## Security Implementation Status

TSZ is actively implementing a comprehensive security hardening roadmap to achieve production-ready status. For details, see:

- **[SECURITY_ROADMAP.md](SECURITY_ROADMAP.md)** – Detailed security-hardening plan (authentication, authorization, rate limiting, TLS, audit logging, vulnerability scanning)
- **[ROADMAP.md](ROADMAP.md)** – Phase 1 Subsection 1b includes security milestones and timeline

### Current Limitations (Being Addressed)

The current version has the following security limitations that are being actively addressed as part of Phase 1:

- Warning **Authentication is Optional by Configuration**: Authentication/RBAC middleware is implemented, but can be disabled (`AUTH_ENABLED=false`). Production deployments should enable it and configure scoped tokens.
- Warning **Rate Limiting is Basic/In-Memory**: Global and endpoint-level limits are implemented, but distributed Redis-backed limiting is still pending.
- Warning **HTTP-only**: No TLS by default (fix: week 4-5)
- Warning **No Audit Logging**: Limited security event logging (fix: weeks 5-6)
- Warning **Dependency Scanning**: Not automated in CI/CD (fix: week 6-7)

Until these are implemented, TSZ should be deployed **only in secure, trusted environments** with the following precautions:

1. **Network Isolation**: Deploy behind a VPC or private network with firewall rules
2. **API Gateway**: Use an API gateway or reverse proxy (NGINX, Kong, AWS API Gateway) to:
   - Enforce TLS/HTTPS
   - Implement authentication (API keys, JWT)
   - Implement rate limiting
   - Add authorization and audit logging
3. **Database Security**: Use encrypted connections (sslmode=require) to PostgreSQL
4. **Redis Security**: Use TLS and strong authentication
5. **Access Control**: Restrict network access to the TSZ service to only trusted clients
6. **Monitoring**: Monitor logs for suspicious activity

### Timeline to Production Ready

See [SECURITY_ROADMAP.md](SECURITY_ROADMAP.md) for the complete timeline:

- **Weeks 1-2**: HTTP security (headers, size limits, timeouts)
- **Weeks 2-3**: Authentication & authorization
- **Week 3-4**: Rate limiting
- **Weeks 4-5**: TLS/HTTPS and encryption
- **Weeks 5-6**: Audit logging and SIEM integration
- **Weeks 6-7**: Automated vulnerability scanning
- **Weeks 7-8**: Production hardening (Docker, secrets management, security tests)
- **Week 8**: Documentation and contributing guidelines

**Target**: Q2-Q3 2026
