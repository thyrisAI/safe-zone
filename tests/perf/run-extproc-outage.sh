#!/usr/bin/env bash

set -euo pipefail

# Exercises Envoy while every tsz-ext-proc endpoint is unavailable. It proves
# that failures stay bounded at the data plane: requests terminate within a
# budget, no request reaches the mock upstream, and Envoy's allocated-memory
# counter does not grow beyond the configured allowance.

readonly DEMO_NAMESPACE="${TSZ_PERF_NAMESPACE:-tsz-byg-demo}"
readonly GATEWAY_NAMESPACE="${TSZ_PERF_GATEWAY_NAMESPACE:-envoy-gateway-system}"
readonly GATEWAY_NAME="${TSZ_PERF_GATEWAY_NAME:-echo-gateway}"
readonly ENVOY_PORT="${TSZ_OUTAGE_ENVOY_PORT:-28080}"
readonly ADMIN_PORT="${TSZ_OUTAGE_ADMIN_PORT:-29000}"
readonly MOCK_PORT="${TSZ_OUTAGE_MOCK_PORT:-28081}"
readonly K6_BIN="${K6_BIN:-k6}"
readonly KUBECONFIG_PATH="${TSZ_BYG_KUBECONFIG:-${TMPDIR:-/tmp}/tsz-byg-tools/tsz-byg.kubeconfig}"
readonly MAX_MEMORY_DELTA_BYTES="${TSZ_OUTAGE_MAX_MEMORY_DELTA_BYTES:-33554432}"
readonly EXPECTED_REQUEST_RETRIES="${TSZ_OUTAGE_EXPECTED_REQUEST_RETRIES:-0}"
readonly RESULTS_DIR="${TSZ_PERF_RESULTS_DIR:-test-reports/perf}"
readonly EXAMPLE="examples/bring-your-gateway/06-fail-closed"

fail() { echo "outage-perf: $*" >&2; exit 1; }

command -v kubectl >/dev/null 2>&1 || fail "kubectl is required"
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"
command -v "$K6_BIN" >/dev/null 2>&1 || fail "k6 is required"

export KUBECONFIG="$KUBECONFIG_PATH"
TSZ_BYG_KUBECONFIG="$KUBECONFIG_PATH" examples/bring-your-gateway/shared/run.sh "$EXAMPLE"

envoy_forward_pid=""
admin_forward_pid=""
mock_forward_pid=""
cleanup() {
  kubectl -n "$DEMO_NAMESPACE" scale deployment/tsz-ext-proc --replicas=2 >/dev/null 2>&1 || true
  for pid in "$envoy_forward_pid" "$admin_forward_pid" "$mock_forward_pid"; do
    [[ -n "$pid" ]] && kill "$pid" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT INT TERM

kubectl -n "$DEMO_NAMESPACE" scale deployment/tsz-ext-proc --replicas=0 >/dev/null
kubectl -n "$DEMO_NAMESPACE" wait --for=delete pod -l app.kubernetes.io/name=tsz-ext-proc --timeout=90s >/dev/null

envoy_service="$(kubectl -n "$GATEWAY_NAMESPACE" get service -l "gateway.envoyproxy.io/owning-gateway-namespace=$DEMO_NAMESPACE,gateway.envoyproxy.io/owning-gateway-name=$GATEWAY_NAME" -o jsonpath='{.items[0].metadata.name}')"
envoy_pod="$(kubectl -n "$GATEWAY_NAMESPACE" get pod -l "gateway.envoyproxy.io/owning-gateway-namespace=$DEMO_NAMESPACE,gateway.envoyproxy.io/owning-gateway-name=$GATEWAY_NAME" -o jsonpath='{.items[0].metadata.name}')"
[[ -n "$envoy_service" && -n "$envoy_pod" ]] || fail "Envoy data-plane service or pod was not found"

kubectl -n "$GATEWAY_NAMESPACE" port-forward "service/$envoy_service" "$ENVOY_PORT:80" >/tmp/tsz-outage-envoy-port-forward.log 2>&1 &
envoy_forward_pid=$!
kubectl -n "$GATEWAY_NAMESPACE" port-forward "pod/$envoy_pod" "$ADMIN_PORT:19000" >/tmp/tsz-outage-admin-port-forward.log 2>&1 &
admin_forward_pid=$!
kubectl -n "$DEMO_NAMESPACE" port-forward service/mock-openai "$MOCK_PORT:8080" >/tmp/tsz-outage-mock-port-forward.log 2>&1 &
mock_forward_pid=$!
sleep 2

memory_allocated() {
  curl --silent --fail "http://127.0.0.1:$ADMIN_PORT/stats?filter=^server.memory_allocated" | awk -F': ' '/^server.memory_allocated:/ { print $2; exit }'
}

memory_before="$(memory_allocated)"
[[ "$memory_before" =~ ^[0-9]+$ ]] || fail "could not read Envoy server.memory_allocated"

# The BYG mock exposes a content-free monotonic request counter. Capture it
# before load so this verifies the actual upstream boundary, rather than
# inferring it from Envoy's local 5xx response alone.
upstream_sequence_before="$(curl --silent --fail "http://127.0.0.1:$MOCK_PORT/inspect" | jq -r '.sequence')"
[[ "$upstream_sequence_before" =~ ^[0-9]+$ ]] || fail "could not read mock upstream request counter"

# The selected outage policy is deliberately no request replay: the ext_proc
# gRPC service must not carry a retry_policy. Envoy may reconnect its transport
# when endpoints return, but it must not retry an in-flight client request.
retry_policy_count="$(curl --silent --fail "http://127.0.0.1:$ADMIN_PORT/config_dump" | jq '[.. | objects | select(."@type"? == "type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor") | select(.grpc_service.envoy_grpc.authority == "tsz-ext-proc.tsz-byg-demo:9002") | select(.grpc_service.retry_policy? != null)] | length')"
[[ "$EXPECTED_REQUEST_RETRIES" == "0" ]] || fail "only zero request retries is supported by this EnvoyExtensionPolicy profile"
[[ "$retry_policy_count" == "$EXPECTED_REQUEST_RETRIES" ]] || fail "ext_proc retry_policy is configured; outage policy requires $EXPECTED_REQUEST_RETRIES request retries"

mkdir -p "$RESULTS_DIR"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
summary_file="$RESULTS_DIR/extproc-outage-$timestamp.json"

"$K6_BIN" run --summary-export "$summary_file" \
  --env "TSZ_OUTAGE_BASE_URL=http://127.0.0.1:$ENVOY_PORT" \
  --env "TSZ_OUTAGE_RATE=${TSZ_OUTAGE_RATE:-50}" \
  --env "TSZ_OUTAGE_DURATION=${TSZ_OUTAGE_DURATION:-30s}" \
  --env "TSZ_OUTAGE_PRE_ALLOCATED_VUS=${TSZ_OUTAGE_PRE_ALLOCATED_VUS:-20}" \
  --env "TSZ_OUTAGE_MAX_VUS=${TSZ_OUTAGE_MAX_VUS:-100}" \
  --env "TSZ_OUTAGE_TIMEOUT_BUDGET_MS=${TSZ_OUTAGE_TIMEOUT_BUDGET_MS:-3000}" \
  tests/perf/extproc-outage.js

memory_after="$(memory_allocated)"
[[ "$memory_after" =~ ^[0-9]+$ ]] || fail "could not read Envoy server.memory_allocated after load"
memory_delta=$((memory_after - memory_before))
if (( memory_delta > MAX_MEMORY_DELTA_BYTES )); then
  fail "Envoy allocated-memory growth was $memory_delta bytes; limit is $MAX_MEMORY_DELTA_BYTES"
fi
upstream_sequence_after="$(curl --silent --fail "http://127.0.0.1:$MOCK_PORT/inspect" | jq -r '.sequence')"
[[ "$upstream_sequence_after" =~ ^[0-9]+$ ]] || fail "could not read mock upstream request counter after load"
[[ "$upstream_sequence_before" == "$upstream_sequence_after" ]] || \
  fail "processor outage forwarded request(s) to mock upstream: sequence $upstream_sequence_before -> $upstream_sequence_after"

echo "PASS outage test: retries=0, upstream requests=0, Envoy memory delta=${memory_delta}B, limit=${MAX_MEMORY_DELTA_BYTES}B"
echo "summary: $summary_file"
