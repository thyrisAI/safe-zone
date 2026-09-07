# Response masking

The local mock returns synthetic email PII. TSZ masks `choices[].message.content` before Envoy sends the non-streaming response to the client. This is buffered, non-streaming enforcement; it does not cover SSE responses.

Prerequisites: Docker, Kind, kubectl, Helm, curl and jq. The shared bootstrap pins Envoy Gateway v1.8.3.

Run and clean up:

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/15-response-masking
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/15-response-masking
```

A safe request is `request.json`; the mock response contains `alice@example.com` only as a synthetic fixture. The expected result is HTTP 200 with that raw value absent from the client response. The smoke test also verifies the mock upstream received the request, while the Envoy/TSZ logs and `io.thyris.tsz` metadata provide operational inspection. If setup fails, rerun the bootstrap and inspect the Envoy Gateway and `tsz-ext-proc` pods.

Request masking and request blocking are intentionally not exercised here; use
`02-request-masking` and `03-request-blocking`. This example's guarantee is
limited to the configured buffered OpenAI-compatible response field. The
[example-set guide](../README.md) documents architecture, pinned versions,
upstream inspection, metrics/traces, production limitations, troubleshooting,
and full cluster cleanup. The cleanup command above removes this example's
focused resources while preserving the shared cluster.
