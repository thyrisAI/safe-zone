# Asynchronous SSE audit

This example streams an OpenAI-compatible SSE response through Envoy without
delaying or mutating its events. After the bounded stream completes, TSZ
inspects the collected events on a background worker and emits an
`AUDIT_ONLY` response decision.

## Security guarantee and limitation

`AsyncAudit` provides visibility only. The synthetic email is expected to
reach the client unchanged before its audit finishes. This mode does not mask,
block, halt, or provide confidentiality; policy activation rejects `MASK` and
`BLOCK` response actions for `AsyncAudit`. Use `Windowed` or buffered response
processing when enforcement is required.

The observation buffer and worker queue are bounded. If either limit is
exceeded, delivery still continues and TSZ records an aggregate degraded
failure metric without logging response content.

Prerequisites: Docker, Kind, kubectl, Helm, curl, and jq. The shared bootstrap
pins Envoy Gateway v1.8.3 and Gateway API v1.5.1.

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/10-stream-async-audit --response-mode streamed
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/10-stream-async-audit
```

## Expected result and verification

The expected result is HTTP 200, a complete `data: [DONE]` event, and the
synthetic email still present in the client stream. The shared runner also
verifies that the request reached the local mock upstream and that one processor
replica records a response-stage `AUDIT_ONLY` metric after completion. Runtime
tests separately assert the PII-safe audit event contract.

## Troubleshooting and cleanup

If the stream changes or stalls, verify that the streamed
`EnvoyExtensionPolicy` is accepted and that the active policy mode is exactly
`AsyncAudit`. Inspect only bounded action/category metrics; do not print raw
response bodies in production. Cleanup removes only this example's focused
resources.
