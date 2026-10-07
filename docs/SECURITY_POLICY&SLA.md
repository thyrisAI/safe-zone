# Security Policy

## Vulnerability Scanning

This repository runs automated security scanning on every push to `main`,
every pull request targeting `main`, and weekly on a schedule (Mondays,
06:00 UTC), via `.github/workflows/security.yml`. The pipeline covers:

| Tool | Scope | Category |
|---|---|---|
| [govulncheck](https://go.dev/security/vuln/) | Go standard library + imported modules, call-graph aware | Known vulnerabilities in Go code |
| [gosec](https://github.com/securego/gosec) | Go source code | Static analysis (SAST) |
| [OWASP Dependency-Check](https://owasp.org/www-project-dependency-check/) | Go modules, npm packages (`web/`) | Known vulnerabilities in dependencies |
| [Trivy](https://github.com/aquasecurity/trivy) | Container image | Vulnerabilities in OS packages and binaries |

## Pull Request Gate

`govulncheck` and `gosec` block a pull request on any finding. OWASP
Dependency-Check blocks on any dependency vulnerability with a **CVSS score
of 7.0 or higher** (`--failOnCVSS 7`). Trivy blocks on any `HIGH` or
`CRITICAL` severity finding in the built container image
(`ENFORCE_CONTAINER_SCAN: "true"`).

This PR-time gate is a first line of defense — it stops new HIGH/CRITICAL
issues from being introduced. It is separate from the remediation SLA below,
which governs how quickly an already-known or already-merged vulnerability
must be fixed.

## Remediation SLA

Once a vulnerability is identified (via CI scanning, Dependabot, or manual
report), it must be remediated within the following timeframes based on
severity:

| Severity | CVSS Range | SLA |
|---|---|---|
| **CRITICAL** | 9.0 – 10.0 | Fix within 24 hours |
| **HIGH** | 7.0 – 8.9 | Fix within 7 days |
| **MEDIUM** | 4.0 – 6.9 | Fix within 30 days |
| **LOW** | 0.1 – 3.9 | Fix in the next release cycle |

Severity is determined by the CVSS score reported by the scanning tool. If
multiple tools report the same vulnerability with different scores, the
highest reported score determines the SLA.

## Dependency Updates

Dependabot is configured (`.github/dependabot.yml`) to open weekly pull
requests for outdated Go modules, npm packages, GitHub Actions, and Docker
base images across all project directories. Patch and minor updates are
grouped to reduce PR noise; major version bumps are opened individually for
manual review.

## Handling False Positives / Accepted Risk

If a scanner flags something that is confirmed to be a false positive, or a
risk the team has consciously decided to accept:

- **gosec**: add a `//nolint:gosec` comment with a justification directly
  above the flagged line, or add the specific rule ID to the `-exclude` flag
  in the workflow with a comment explaining why.
- **OWASP Dependency-Check**: add a suppression rule (see
  [suppression documentation](https://jeremylong.github.io/DependencyCheck/general/suppression.html))
  with a written justification and a review date.
- **Trivy**: `ignore-unfixed: true` is already enabled, meaning findings
  without an available fix are not blocking. For a genuine false positive,
  use a `.trivyignore` file with a justification comment.

Suppressions must not be used to silence findings that are simply
inconvenient to fix before a deadline — they are reserved for confirmed
non-issues or explicitly accepted risk, and should be reviewed periodically.

## Reporting a Vulnerability

If you discover a security vulnerability in this project, please report it
privately rather than opening a public issue. Contact the maintainers
directly at [insert contact/email] with a description of the issue and
steps to reproduce.