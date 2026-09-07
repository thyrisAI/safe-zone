# Bring Your Gateway runnable examples

This directory is the runnable Envoy Gateway example set required by the
gateway-adapter release contract. It uses a local Kind cluster, a local mock
OpenAI-compatible upstream, synthetic fixtures, and no real credentials or
routable internal endpoints.

## Security scope and architecture

Requests follow this path:

```text
curl -> Envoy Gateway -> TSZ ext_proc -> local mock upstream
                         |                 |
                         + audit/metrics   + safe /inspect summary
```

The gateway owns policy identity. The shared runner deliberately supplies an
untrusted client value and verifies that the route-owned `default` policy wins.
Request masking is checked at the mock upstream; request blocks must never
reach it. Response masking and blocking are checked before bytes reach the
client. Examples use synthetic sensitive-looking strings only.

These are local demonstrations, not production manifests. They use a
development image, test credentials, a throwaway cluster, and local plaintext
ports. Production deployments must follow the
[Envoy Gateway integration guide](../../docs/integrations/ENVOY_GATEWAY.md),
including its TLS/mTLS, network isolation, secret-management, sizing, failure
policy, upgrade, and rollback requirements.

## Prerequisites and tested versions

Install Docker, Go, Kind, kubectl, Helm, curl, and jq. The bootstrap pins Envoy
Gateway v1.8.3 and Gateway API v1.5.1; Go is pinned by `go.mod`. No external AI
provider or API key is needed.

## Install and run

Run one example with the shared runner:

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/01-minimal-inspection
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/01-minimal-inspection
```

Run the complete adapter example set exactly as CI does:

```bash
examples/bring-your-gateway/smoke.sh
```

The first run creates the Kind cluster, builds the local TSZ image, installs
the pinned gateway, deploys TSZ and the mock upstream, activates the selected
policy, and sends `request.json`. Each directory declares its expected HTTP
status in `expected-status`.

## Required verification set

| Contract scenario | Runnable example | Expected result |
| --- | --- | --- |
| Safe request | `01-minimal-inspection` | HTTP 200; mock receives the unchanged safe body |
| Request masking | `02-request-masking` | HTTP 200; mock sees a mask token and no synthetic email |
| Request blocking | `03-request-blocking` | HTTP 400; mock request sequence does not advance |
| Response masking | `15-response-masking` | HTTP 200; raw synthetic PII is absent at the client |
| Response blocking | `16-response-blocking` | HTTP 403; safe TSZ error replaces the unsafe response |
| Fail open | `05-fail-open` | HTTP 200; dependency failure does not alter the safe upstream body |
| Fail closed | `06-fail-closed` | HTTP 400; dependency failure prevents upstream delivery |
| Audit-only rollout | `09-audit-only` | HTTP 200; finding is recorded while the upstream body remains byte-identical |
| Async SSE audit | `10-stream-async-audit` | HTTP 200; unsafe content is deliberately forwarded unchanged and inspected only after completion |
| Windowed SSE masking | `11-stream-window` | HTTP 200; unsafe value is masked before its window is released |
| Windowed SSE halt | `12-stream-halt` | Safe terminal 403; violation is absent and future SSE delivery stops |
| Telemetry | `20-observability` | Masked upstream request plus bounded metric and PII-safe SIEM event |

The suite also runs the additional numbered examples. Every numbered directory
is discovered automatically, so a focused example needs only its fixtures and,
when necessary, a custom `run.sh` or `cleanup.sh`.

## Upstream, logs, metrics, and traces

The shared runner compares the mock's `/inspect` sequence before and after each
request. Its inspection response exposes only bounded facts such as whether a
mask was observed and a body hash; it never returns or retains the raw request.

For local troubleshooting, inspect processor and gateway logs and metrics:

```bash
kubectl -n tsz-byg-demo logs deployment/tsz-ext-proc
kubectl -n envoy-gateway-system get pods
kubectl -n tsz-byg-demo port-forward service/tsz-ext-proc 8080:8080
curl -s http://127.0.0.1:8080/metrics | grep '^tsz_extproc_'
```

The observability example verifies the metrics and SIEM path. OpenTelemetry is
disabled by default; configure only a trusted local collector as documented in
the integration guide, then inspect the `tsz.extproc.request` and
`tsz.extproc.response` spans without enabling body capture.

## Troubleshooting

If bootstrap or an assertion fails, check Docker availability, cluster pods,
`EnvoyExtensionPolicy` acceptance, processor logs, and occupied local ports.
Do not print `request.json` or raw bodies while diagnosing a failure. Re-run the
individual example after cleanup; the scripts are designed to be repeatable.

## Cleanup

An individual cleanup preserves the shared Kind cluster for the next example:

```bash
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/01-minimal-inspection
```

Remove the complete local environment with:

```bash
deployments/envoy-gateway/kind-bootstrap.sh down
```
