# Route-owned policy identity

This example sends a safe request with an intentionally untrusted client policy
header. Envoy overwrites that value with the route-owned `default` identity
before TSZ resolves the policy. The expected result is HTTP 200 from the local
mock upstream; the response proves the client cannot select or disable policy.
Masking and blocking behavior is covered by the dedicated request examples.

The guarantee depends on the gateway applying its trusted header mutation
before `ext_proc`. A client header is never a policy-authority source. This is
a local demonstration rather than production configuration.

Prerequisites, pinned versions, architecture, installation, upstream and
telemetry checks, troubleshooting, cleanup, and production limitations are in
the [example-set guide](../README.md).

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/04-route-owned-policy
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/04-route-owned-policy
```
