# Local rate limit and guardrails

This Kind integration test attaches a route-level Envoy `BackendTrafficPolicy`
and TSZ guardrails to the same `HTTPRoute`.

The first two PII-containing requests are accepted and masked by TSZ. The
third request creates Envoy's local `429`. That body is not an OpenAI response
shape, so TSZ's default closed response failure policy replaces it with a safe
HTTP `403`; TSZ does not implement or return the rate-limit response.

Run it with:

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/14-local-rate-limit
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/14-local-rate-limit
```
