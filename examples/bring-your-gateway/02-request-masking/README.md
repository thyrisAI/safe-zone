# Request masking before upstream delivery

This example sends a synthetic email through Envoy Gateway. TSZ masks it before
the local mock upstream receives the request. The expected result is HTTP 200;
the runner verifies the mock observed an `EMAIL_` placeholder and did not
observe the raw fixture. A safe baseline is `01-minimal-inspection`; blocking
is covered by `03-request-blocking`.

The guarantee applies to the supported OpenAI-compatible buffered request
shape and configured detector. It is not a production deployment recipe and
does not cover unconfigured content types. The mock stores only a bounded safe
inspection summary.

Prerequisites, pinned versions, architecture, installation, telemetry checks,
troubleshooting, and production limitations are in the
[example-set guide](../README.md).

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/02-request-masking
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/02-request-masking
```

The runner activates `policy.json`, verifies HTTP 200 and upstream masking, and
fails if the synthetic email reaches the mock. Cleanup removes only the focused
resources; the shared cluster can be removed with the set guide's command.
