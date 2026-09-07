# Observability and SIEM

This runnable Kind example sends a masked synthetic PII request through Envoy,
then verifies the PII-safe Prometheus metric family and a local SIEM webhook
event. The event contains TSZ RID, Envoy request ID, policy/version, action and
category; it never contains the request body or matched value.

Run `examples/bring-your-gateway/20-observability/run.sh` and clean up with
`examples/bring-your-gateway/20-observability/cleanup.sh`. It needs Docker,
Kind, kubectl, curl and jq. The local mock provider confirms masking before
upstream delivery. The mock sink is test-only; use an approved TLS-protected
collector in production. Trace exporter configuration and trusted traceparent
propagation are documented in `docs/operations/BYG_OBSERVABILITY.md`.
These prerequisites and tested versions are shared with the complete set and
are pinned by its bootstrap.

The safe OpenAI-compatible request contains only the synthetic
`synthetic@example.com` fixture. The expected result is HTTP 200: the upstream
sees a mask rather than that value, `tsz_extproc_actions_total` is present, and
the mock SIEM receives a correlated event with no raw fixture. Blocking is not
part of this focused example; use `03-request-blocking` and
`16-response-blocking` for request and response blocks.

The [example-set guide](../README.md) documents architecture, tested Envoy and
Gateway API versions, installation, log/metric/trace inspection, production
limitations, and troubleshooting. The cleanup script removes the local SIEM
resources and processor webhook setting; use the guide's cluster cleanup after
the complete suite.
