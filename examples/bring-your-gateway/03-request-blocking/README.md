# Request blocking before upstream delivery

This example sends a synthetic API-key fixture through Envoy Gateway. TSZ
returns an OpenAI-compatible HTTP 400 block before the request reaches the
local mock upstream. The runner proves non-delivery by comparing the mock's
safe request sequence. The expected result is HTTP 400. Safe and masked requests are covered by
`01-minimal-inspection` and `02-request-masking`.

The guarantee applies to configured, supported buffered request content. The
fixture is not a real credential. This local demo does not replace production
identity, TLS, network-policy, or failure-mode configuration.

Prerequisites, pinned versions, architecture, installation, telemetry checks,
troubleshooting, and production limitations are in the
[example-set guide](../README.md).

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/03-request-blocking
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/03-request-blocking
```

The runner activates `policy.json`, checks HTTP 400 and policy identity, and
fails if the mock sequence advances. Cleanup removes the focused resources;
the set guide documents full cluster removal.
