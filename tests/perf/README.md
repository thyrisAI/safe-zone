# BYG performance tests

This directory contains repeatable performance scenarios for the Envoy Gateway
reference deployment. Test output is written to `test-reports/perf/` and is
not committed.

## Regex-only request-path baseline

`extproc-regex-only.js` sends non-streaming, safe OpenAI Chat Completions
requests through Envoy Gateway, `tsz-ext-proc`, and the local mock OpenAI
upstream. The `01-minimal-inspection` policy has no AI semantic validator, so
the scenario exercises the deterministic request path.

Run it from the repository root:

```bash
make perf-extproc-regex-only
```

The runner requires Docker, Kind, `kubectl`, `curl`, `jq`, and `k6`. It creates or
refreshes the pinned Kind reference environment, activates the minimal policy,
attaches the `EnvoyExtensionPolicy`, verifies one functional request, opens a
temporary Envoy port-forward, and writes two k6 JSON summaries. It first
measures the protected route, then removes only the `EnvoyExtensionPolicy` and
measures the same warm route as its baseline. The attachment is restored on
every exit path. The command fails when protected p95 minus baseline p95
exceeds `TSZ_PERF_MAX_ADDED_P95_MS` (default `20`).

The default load profile is 25 requests per second for two minutes. Adjust it
without changing the scenario:

```bash
TSZ_PERF_RATE=50 TSZ_PERF_DURATION=5m make perf-extproc-regex-only
```

Additional knobs are `TSZ_PERF_PRE_ALLOCATED_VUS`, `TSZ_PERF_MAX_VUS`,
`TSZ_PERF_LOCAL_PORT`, `TSZ_PERF_RESULTS_DIR`,
`TSZ_PERF_MAX_ADDED_P95_MS`, and `TSZ_PERF_XDS_SETTLE_SECONDS`. Set
`TSZ_BYG_SKIP_BOOTSTRAP=1` only when the same prepared Kind environment is
already running.

## Processor-outage boundedness

`make perf-extproc-outage` prepares the fail-closed route, scales
`tsz-ext-proc` to zero, and sends concurrent normal and streaming request
shapes through Envoy. It asserts all results are `500` or `503`, the p99
latency remains under a bounded timeout budget, and Envoy's
`server.memory_allocated` growth remains below the configured limit. It also
reads the test-only mock upstream's content-free monotonic request counter
before and after the load; the counter must not change, proving that the
failure response was produced by Envoy before upstream forwarding. It also
asserts the live ext-proc xDS configuration has no gRPC `retry_policy`:
the outage policy is **zero request retries**. Envoy may reconnect its gRPC
transport once endpoints return, but it must not replay an in-flight client
request. `TSZ_OUTAGE_EXPECTED_REQUEST_RETRIES` defaults to `0` and rejects
other values because Envoy Gateway v1.8.3's `EnvoyExtensionPolicy.extProc`
profile has no explicit per-request retry field. The processor deployment is
restored to two replicas on exit.

Defaults are 50 RPS for 30 seconds, a 3-second p99 budget, and a 32 MiB
allocated-memory allowance. Tune the environment-specific values without
editing the scenario:

```bash
TSZ_OUTAGE_RATE=100 TSZ_OUTAGE_DURATION=2m \
TSZ_OUTAGE_TIMEOUT_BUDGET_MS=3000 \
TSZ_OUTAGE_MAX_MEMORY_DELTA_BYTES=$((64 * 1024 * 1024)) \
make perf-extproc-outage
```

Choose `TSZ_OUTAGE_TIMEOUT_BUDGET_MS` from the ext-proc message timeout plus
transport and proxy scheduling allowance. With the current `messageTimeout`
of 2 seconds, 3 seconds is a conservative local starting budget. In
production, set it from a percentile of healthy processing latency and the
request SLO, leaving room for retries only if a finite retry budget is
explicitly configured.

## Chaos scenario matrix

The expected failure-injection coverage and pass criteria are maintained in
[CHAOS.md](CHAOS.md). `processor-all-endpoints-unavailable` is automated by
`make perf-extproc-outage`; the remaining scenarios are deliberately listed
with their required injection mechanism and evidence so they can be added to
the same Kind harness without turning an ad-hoc incident exercise into test
coverage.

## Baseline result

Environment: local Kind reference deployment, Envoy Gateway v1.8.3, local mock
OpenAI upstream, k6 v2.2.0.

| Date | Profile | Load | Requests | HTTP/check failures | End-to-end p95 | Mean |
| --- | --- | --- | ---: | ---: | ---: | ---: |
| 2026-08-28 | regex-only request path | 25 RPS for 2 min | 3,001 | 0 | 12.105 ms | 7.023 ms |

The historical row predates the paired comparison and is an end-to-end
protected-path baseline, not proof of added latency. New runs produce protected
and unprotected summaries and enforce the issue's regex-only added-p95 target.
Record a new result only from those paired artifacts; do not subtract results
from different clusters, rates, durations, or request shapes.
