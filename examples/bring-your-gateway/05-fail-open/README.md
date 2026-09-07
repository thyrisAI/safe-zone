# Explicit request fail-open

This example induces a synthetic audit dependency failure after processing a
safe request. Its explicit request fail-open policy returns HTTP 200 and sends
the byte-identical request to the local mock upstream; this is the expected
result. Masking and blocking are
not exercised; use `02-request-masking` and `03-request-blocking` for those
outcomes.

Fail-open trades confidentiality enforcement for availability and is never an
implicit fallback. This example proves only the declared local failure path;
production use requires a documented risk decision and monitoring.

Prerequisites, pinned versions, architecture, installation, telemetry checks,
troubleshooting, and production limitations are in the
[example-set guide](../README.md).

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/05-fail-open
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/05-fail-open
```

The runner hashes `request.json` and the mock's safe body summary and requires
them to match. Cleanup clears the injected failure fixture and focused
resources; the set guide documents full cluster removal.
