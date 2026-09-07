# ext_proc chaos scenario matrix

These scenarios run against the pinned Kind/Envoy Gateway reference deployment.
Every scenario must send traffic through Envoy and verify both the client result
and the test-only mock upstream's `/inspect` sequence counter. When no Ready
processor endpoint is usable, the counter must not advance; scenarios with a
healthy replica explicitly verify the expected forwarding and recovery instead.

| ID | Failure injection | Traffic point | Required assertions | Automation |
| --- | --- | --- | --- | --- |
| `processor-all-endpoints-unavailable` | Scale `tsz-ext-proc` to zero replicas. | Sustained normal and streaming traffic after endpoints disappear. | Controlled `500`/`503`; p99 remains within the timeout budget; upstream sequence is unchanged; no ext_proc request retry; Envoy memory remains within its allowance. | Implemented: `make perf-extproc-outage`. |
| `processor-pod-restart` | Delete one processor pod and let the Deployment recreate it. | Continuous traffic during termination, endpoint withdrawal, and readiness recovery. | Requests follow the configured failure mode; no unintended upstream forwarding while no endpoint is usable; service returns to healthy processing after the replacement is Ready. | Planned. |
| `processor-replica-loss` | Scale from two replicas to one, then restore two. | Sustained traffic before, during, and after the scale change. | No traffic blackout while one Ready endpoint remains; request errors and latency stay within the configured SLO; both replicas converge on the active policy after restoration. | Planned. |
| `processor-network-partition` | Apply a temporary NetworkPolicy that denies Envoy-to-`tsz-ext-proc:9002` traffic, then remove it. | Sustained normal and streaming traffic during the partition and recovery. | Same bounded fail-closed result as a full outage; upstream sequence is unchanged during denial; processing recovers after connectivity is restored. | Planned. |
| `processor-latency` | Inject delay above `messageTimeout` on the ext_proc gRPC path. | Sustained traffic while delayed, then after delay removal. | Timeout is bounded; no request replay beyond the configured retry budget; upstream behavior matches the failure mode; latency recovers after delay removal. | Planned. |
| `processor-not-ready` | Start a replacement pod whose readiness probe remains false, then make it Ready. | Traffic while the replacement is unready and while endpoints change. | Kubernetes does not publish the unready endpoint; Envoy never routes ext_proc calls to it; existing Ready replicas continue serving or the configured failure mode applies. | Planned. |
| `processor-active-traffic-interruption` | Terminate all processor pods during an in-flight request/load window. | Long-running streaming and normal traffic already in progress. | In-flight requests complete or fail within the timeout budget; no hung connections or unbounded memory; no unexpected upstream forwarding after processing becomes unavailable. | Planned. |

Run an automated scenario from the repository root:

```bash
make perf-extproc-outage
```

For every new automation, keep the injection reversible and restore the
deployment, NetworkPolicy, and any latency proxy in a shell cleanup handler.
