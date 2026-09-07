# Windowed SSE stream halt

This example streams a synthetic OpenAI-compatible SSE response through Envoy
Gateway. The response policy uses `BLOCK`; when TSZ detects the synthetic email
in an event-aligned window, it returns a safe terminal response and stops
future SSE delivery. The raw detected value must not appear in the terminal
body.

Windowed halt is best-effort streaming enforcement, not a zero-leakage
guarantee. Bytes released from earlier safe windows cannot be recalled. A route
that requires no unsafe byte to reach the client must use buffered,
non-streaming response enforcement. The fixture places the violation in the
first releasable window so the expected result is a safe HTTP 403.

Prerequisites: Docker, Kind, kubectl, Helm, curl and jq. The shared bootstrap
pins Envoy Gateway v1.8.3 and Gateway API v1.5.1.

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/12-stream-halt --response-mode streamed
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/12-stream-halt
```

The runner verifies the 403 `TSZ_RESPONSE_GUARDRAIL_BLOCKED` payload, absence
of the synthetic email, and that the request did reach the local mock upstream
before its unsafe response was halted. Inspect `tsz_extproc_stream_halts_total`
for operational evidence.

## Troubleshooting and cleanup

On failure, inspect Envoy/processor logs without printing the response fixture.
Cleanup removes only focused resources.
