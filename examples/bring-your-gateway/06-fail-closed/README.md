# Request fail-closed

This example induces a synthetic audit dependency failure while processing a
safe request. The request fail-closed policy returns HTTP 400 and the local
mock upstream must not receive it; this is the expected result. Masking is not
exercised; the dedicated
mask example is `02-request-masking`.

Fail-closed is the production request-enforcement default, but this focused
fixture covers only the injected audit failure. Operators must separately test
timeouts, unavailable processors, policy lookup, and their gateway's native
failure configuration.

Prerequisites, pinned versions, architecture, installation, telemetry checks,
troubleshooting, and production limitations are in the
[example-set guide](../README.md).

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/06-fail-closed
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/06-fail-closed
```

The runner checks HTTP 400 and proves non-delivery with the mock request
sequence. Cleanup clears the failure fixture and focused resources; the set
guide documents full cluster removal.
