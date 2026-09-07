# Windowed SSE response masking

This Kind example streams an OpenAI-compatible SSE fixture through Envoy's
`ext_proc` adapter. TSZ groups complete SSE events into a bounded window,
inspects the concatenated assistant deltas, and masks the synthetic email
address split across two events before releasing the safe portion downstream.

The response profile is `Streamed`; request processing remains `Buffered`.
The policy selects `Streaming.Mode: Windowed`, uses a 64-byte target window,
and permits only response `MASK` actions. The fixture is mounted into the
general-purpose local mock, so other streaming examples can supply different
event sequences without changing the mock binary.

For the full BYG Windowed security contract and cancellation semantics, see
[Streaming concepts](../../../docs/concepts/STREAMING.md#2-byg--envoy-ext_proc-windowed-enforcement).

## Security guarantee and limitation

The example verifies that `alice@example.com` is absent from the client SSE
output, a RID-scoped `EMAIL_` mask placeholder is present, and the stream
finishes with `data: [DONE]`. Windowed enforcement protects the complete
window that has not yet been sent to the client. It adds buffering latency and
bounded per-stream memory use; it is not a strict zero-leakage guarantee
because already emitted deltas cannot be retracted.

`request.json` is a safe request with an expected HTTP `200`; the synthetic
email exists only in the local SSE response fixture. The runner also verifies
that the mock upstream received that request.

Block/halt is not part of this example; see the
[12-stream-halt example](../12-stream-halt/README.md).

## Run, verify, and clean up

Prerequisites: Docker, Kind, kubectl, Helm, curl, and jq. The shared bootstrap
pins Envoy Gateway v1.8.3 and Gateway API v1.5.1.

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/11-stream-window --response-mode streamed
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/11-stream-window
```

The runner mounts `mock-sse-fixture`, activates `policy.json`, verifies the
HTTP status and SSE assertions, and confirms that the local mock received the
request. To troubleshoot, rerun the bootstrap and inspect the Envoy Gateway
and `tsz-ext-proc` pods. `examples/bring-your-gateway/smoke.sh` includes this
example, and the `BYG example smoke tests (Kind)` job in CI runs that suite.
