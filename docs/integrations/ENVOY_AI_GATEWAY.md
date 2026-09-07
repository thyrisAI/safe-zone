# Envoy AI Gateway compatibility track

## Status

Envoy AI Gateway support is **deferred and unverified**. The current release
supports Envoy Gateway v1.8.3 as its reference adapter. Do not describe Envoy AI
Gateway as supported, and do not deploy the current Envoy examples unchanged
in front of production AI Gateway routes.

This is an existing-adapter compatibility track rather than a claim that a
second transport adapter is needed: AI Gateway traffic may still traverse an
Envoy `ext_proc` filter, but TSZ must prove that guardrail mutation preserves
the AI-specific control-plane and data-plane contracts.

## Required validation matrix

Promotion requires a pinned clean-cluster test covering:

| Concern | Required evidence |
| --- | --- |
| Filter ordering | TSZ observes the intended normalized request/response stage and cannot be bypassed by another filter. |
| Provider transformations | Masked OpenAI input remains valid after provider-specific translation. |
| Model routing | Route/model selection is unchanged by TSZ header and body mutations. |
| Provider fallback | Primary failure and fallback selection work with request and response guardrails enabled. |
| Authentication | Provider credentials remain owned by AI Gateway and never enter TSZ payloads, metadata, logs, or examples. |
| Token usage and quota | Usage metadata and quota accounting survive response inspection and mutation. |
| Streaming | Event framing, terminal events, windowed masking/halt, cancellation, and already-emitted-byte limitations are verified. |
| Failure policy | Processor timeout/unavailability produces the documented route outcome without accidental unguarded forwarding. |

## Intended filter boundary

TSZ should run after the route has established trusted policy identity and at a
stage where the payload format is one of TSZ's supported content adapters. It
must not own provider credentials, model selection, routing, retries, fallback,
or quotas. The final order must be derived from the tested AI Gateway version;
there is no version-independent ordering recommendation yet.

## Runnable examples

No Envoy AI Gateway example is shipped because no version has completed the
matrix above. This is an explicit deferral, not an omitted supported example.
When the first version qualifies, add single-provider, routing/fallback,
provider-transformation, quota/usage-metadata, and streaming examples; register
the resulting compatibility profile in `adapter-releases.json` so CI enforces
the guide and required example set.

## Promotion gate

The status may change from Deferred only after all matrix rows run in CI on a
clean Kind cluster, the supported version range is recorded here, the threat
model is reviewed for the final filter order, and upgrade/rollback behavior is
verified. Until then, operators may run a private spike but receive no BYG
compatibility guarantee for Envoy AI Gateway.
