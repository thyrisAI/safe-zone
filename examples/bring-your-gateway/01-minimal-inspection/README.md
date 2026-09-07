# Minimal safe request inspection

This example sends the synthetic safe request in `request.json` through Envoy
Gateway and TSZ to the local mock upstream. The expected result is HTTP 200 and
an unchanged upstream body. It establishes the safe-request baseline; masking
and blocking are demonstrated by `02-request-masking` and
`03-request-blocking`.

The policy allows all request categories, so this example does not promise to
mask or block sensitive content. It is for local demonstration only. The
runner verifies that the mock upstream received the request and that the
route-owned policy was used.

Prerequisites, pinned versions, architecture, installation, log/metric/trace
checks, troubleshooting, and production limitations are in the
[example-set guide](../README.md).

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/01-minimal-inspection
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/01-minimal-inspection
```

The first command installs shared prerequisites when needed, sends the request,
and checks HTTP 200 plus the local mock response. The second command removes
only this example's resources; use the set guide's cluster cleanup when done.
