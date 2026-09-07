# BYG Observability Guide

This guide describes how to operate BYG through metrics, traces, audit events,
and SIEM delivery. Metric names, trace setup, and safe attribute definitions
remain authoritative in the
[Envoy Gateway integration guide](../integrations/ENVOY_GATEWAY.md#prometheus-metrics)
and [API reference](../API_REFERENCE.md#33-bring-your-gateway-envoy-external-processing).

## Telemetry safety boundary

Telemetry must allow an operator to understand enforcement without recreating
the guarded content. Do not export request or response bodies, raw PII,
credentials, validator prompts/responses, full error text, user IDs, or tenant
identifiers as metric labels or span attributes. Use the documented bounded
dimensions and TSZ's PII-safe `io.thyris.tsz` metadata namespace instead.

Treat any dashboard, collector, SIEM destination, or temporary debug logging
as part of the data boundary. Access must be restricted to authorized
operators, retained according to policy, and reviewed before a new field is
added.

## Establish a correlated incident view

For each protected transaction, correlate these identifiers where available:

| Identifier | Origin | Use |
| --- | --- | --- |
| Envoy request ID | Envoy Gateway | Locate the gateway access log and route decision. |
| TSZ RID | External processor | Locate the guardrail audit event and action. |
| W3C trace ID | Trusted `traceparent` propagated by Envoy | Follow latency across Envoy, TSZ, validators, PostgreSQL, Redis, and an OTLP backend. |
| Policy ID/version | TSZ policy snapshot | Confirm the exact immutable policy used for request and response processing. |

Client-supplied correlation headers are not trusted authority. Configure Envoy
to supply the trusted ext_proc attributes, then use the resulting IDs to join
access logs, traces, and audit events.

## Metrics operations

Scrape the processor's `/metrics` endpoint only from the Prometheus workload or
another authenticated operations network. Never publish it through a public
Gateway route. Confirm scraping by querying the `tsz_extproc_` metric family
and by checking the scraper's target health.

Build dashboards around four questions:

1. **Is enforcement available?** Track request volume, failure and timeout
   rates, active streams, and readiness-target health.
2. **Is enforcement changing traffic?** Track allow, mask, block, and
   audit-only decisions by bounded action/stage/policy dimensions.
3. **Is capacity adequate?** Track processor latency, body-size distribution,
   concurrent streams, CPU, memory, HPA replica count, and pod restarts.
4. **Is policy distribution healthy?** Track reconciliation, policy activation,
   snapshot-distribution, version-skew, and last-known-good signals as those
   metrics are enabled.

Alert on a sustained readiness failure, error/timeout increase, processor
unavailability, unexpected fail-open decision, stream halt spike, or a policy
version mismatch across replicas. Establish a baseline first; action volume
alone is not inherently an incident because a policy rollout can legitimately
change it.

The controller exposes `tsz_controller_managed_adapter_resources{adapter="envoy-gateway"}`
for resource counts per installed native adapter. Label values come only from
registered controller implementations. The existing
`tsz_controller_managed_extension_policies` gauge remains available with the
same Envoy-only meaning for existing dashboards.

## Tracing operations

Enable OTLP only with a controlled collector endpoint and TLS in production.
Use trace sampling appropriate to the deployment’s load and sensitivity. A
sampled trace should answer which route and policy version processed the
request, which enforcement action was selected, and where latency was spent;
it must not contain the inspected value.

When investigating latency, start with the request/response ext_proc spans,
then inspect validator, semantic-model, PostgreSQL, and Redis child spans.
Compare the trace with Envoy's upstream timing before attributing latency to
TSZ. See [OpenTelemetry tracing](../integrations/ENVOY_GATEWAY.md#opentelemetry-tracing)
for exporter configuration and allowed attributes.

## Audit and SIEM operations

Send audit events to the approved sink and periodically test delivery with a
safe fixture. Audit records should contain the RID, trace ID, route, policy and
immutable version, stage, action, detection type/count, processing duration,
and timestamp. They must contain neither raw findings nor protected payloads.

For a blocked request, an operator should be able to prove all of the
following: Envoy received the route request, TSZ selected the expected policy
version, TSZ chose `BLOCK`, and the mock or real upstream did not receive the
request. Use this verification after policy and deployment changes.

## Troubleshooting and cleanup

When an alert fires, preserve the correlated IDs, policy version, Deployment
revision, and rendered manifest revision before changing configuration. Then
follow [BYG troubleshooting](BYG_TROUBLESHOOTING.md). Remove temporary
port-forwards, elevated log levels, and diagnostic collectors after the
investigation; do not leave broad telemetry access enabled.

