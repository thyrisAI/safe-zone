# Audit-only rollout

This example sends a synthetic email through Envoy Gateway under a route-owned
policy whose request findings use `AUDIT_ONLY`. The request must reach the local
mock upstream byte-for-byte unchanged with HTTP 200. TSZ still records a
bounded decision and category; it does not publish or retain the matched value.

Audit-only provides visibility, not confidentiality enforcement. It is suitable
for a measured rollout before switching the same policy to `MASK` or `BLOCK`.
It must not be described as protection against data leaving the boundary.

## Expected result

The expected result is HTTP 200 and a byte-identical body at the local mock
upstream, despite the synthetic PII finding.

Prerequisites and tested versions are inherited from the
[example-set guide](../README.md): Docker, Kind, kubectl, Helm, curl, jq, Envoy
Gateway v1.8.3 and Gateway API v1.5.1.

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/09-audit-only
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/09-audit-only
```

The runner verifies the route-owned policy, HTTP 200, upstream delivery and the
SHA-256 of the exact request body. Inspect bounded action/category metrics and
SIEM output as described in `20-observability`; neither may contain the email.

## Troubleshooting and cleanup

If the hash differs, inspect policy activation and mock readiness without
printing the body. Cleanup removes only this example's policy resources.
