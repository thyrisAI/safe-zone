#!/usr/bin/env bash

set -euo pipefail

# Prepares the minimal-inspection BYG example, then measures its deterministic
# request path through Envoy, ext_proc, and the mock OpenAI upstream. That
# policy contains no AI semantic validator.

readonly DEMO_NAMESPACE="${TSZ_PERF_NAMESPACE:-tsz-byg-demo}"
readonly GATEWAY_NAMESPACE="${TSZ_PERF_GATEWAY_NAMESPACE:-envoy-gateway-system}"
readonly GATEWAY_NAME="${TSZ_PERF_GATEWAY_NAME:-echo-gateway}"
readonly LOCAL_PORT="${TSZ_PERF_LOCAL_PORT:-18080}"
readonly RESULTS_DIR="${TSZ_PERF_RESULTS_DIR:-test-reports/perf}"
readonly K6_BIN="${K6_BIN:-k6}"
readonly KUBECONFIG_PATH="${TSZ_BYG_KUBECONFIG:-${TMPDIR:-/tmp}/tsz-byg-tools/tsz-byg.kubeconfig}"
readonly PERF_EXAMPLE="examples/bring-your-gateway/01-minimal-inspection"
readonly EXTENSION_POLICY="deployments/envoy-gateway/tsz-ext-proc-envoy-extension-policy.yaml"
readonly MAX_ADDED_P95_MS="${TSZ_PERF_MAX_ADDED_P95_MS:-20}"

fail() {
  echo "perf: $*" >&2
  exit 1
}

command -v kubectl >/dev/null 2>&1 || fail "kubectl is required"
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v "${K6_BIN}" >/dev/null 2>&1 || fail "k6 is required (set K6_BIN to its path if needed)"

# The example runner creates or refreshes the pinned Kind environment, deploys
# the ext_proc processor, activates the deterministic policy, and attaches the
# EnvoyExtensionPolicy. It also performs one functional request before load is
# generated, so a benchmark never silently bypasses the processor.
export KUBECONFIG="${KUBECONFIG_PATH}"
TSZ_BYG_KUBECONFIG="${KUBECONFIG_PATH}" \
  examples/bring-your-gateway/shared/run.sh "${PERF_EXAMPLE}"
kubectl cluster-info >/dev/null || fail "the prepared Kubernetes cluster is not reachable"

envoy_service="$(kubectl -n "${GATEWAY_NAMESPACE}" get service \
  -l "gateway.envoyproxy.io/owning-gateway-namespace=${DEMO_NAMESPACE},gateway.envoyproxy.io/owning-gateway-name=${GATEWAY_NAME}" \
  -o jsonpath='{.items[0].metadata.name}')"
[[ -n "${envoy_service}" ]] || fail "Envoy data-plane service for ${DEMO_NAMESPACE}/${GATEWAY_NAME} was not found"

temp_dir="$(mktemp -d)"
port_forward_pid=""
cleanup() {
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
    wait "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
  # Restore the protected route if the comparison exits after temporarily
  # removing the attachment.
  kubectl apply -f "${EXTENSION_POLICY}" >/dev/null 2>&1 || true
  rm -rf "${temp_dir}"
}
trap cleanup EXIT INT TERM

kubectl -n "${GATEWAY_NAMESPACE}" port-forward "service/${envoy_service}" "${LOCAL_PORT}:80" \
  >"${temp_dir}/port-forward.log" 2>&1 &
port_forward_pid=$!

for _ in $(seq 1 30); do
  if curl --silent --fail --output /dev/null \
    --header 'Content-Type: application/json' \
    --data '{"model":"mock-openai","messages":[{"role":"user","content":"health check"}]}' \
    "http://127.0.0.1:${LOCAL_PORT}/v1/chat/completions"; then
    break
  fi
  sleep 1
done

kill -0 "${port_forward_pid}" 2>/dev/null || {
  cat "${temp_dir}/port-forward.log" >&2
  fail "Envoy port-forward did not start"
}

mkdir -p "${RESULTS_DIR}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
protected_summary="${RESULTS_DIR}/extproc-regex-only-protected-${timestamp}.json"
baseline_summary="${RESULTS_DIR}/extproc-regex-only-baseline-${timestamp}.json"

echo "perf: targeting http://127.0.0.1:${LOCAL_PORT} through ${GATEWAY_NAMESPACE}/${envoy_service}"
echo "perf: writing protected k6 summary to ${protected_summary}"
"${K6_BIN}" run \
  --summary-export "${protected_summary}" \
  --env "TSZ_PERF_BASE_URL=http://127.0.0.1:${LOCAL_PORT}" \
  --env "TSZ_PERF_RATE=${TSZ_PERF_RATE:-25}" \
  --env "TSZ_PERF_DURATION=${TSZ_PERF_DURATION:-2m}" \
  --env "TSZ_PERF_PRE_ALLOCATED_VUS=${TSZ_PERF_PRE_ALLOCATED_VUS:-10}" \
  --env "TSZ_PERF_MAX_VUS=${TSZ_PERF_MAX_VUS:-50}" \
  tests/perf/extproc-regex-only.js

# Measure the same warm route without ext_proc. The mock, Envoy service,
# request shape, rate, and duration stay identical; only the policy attachment
# is removed. The cleanup trap restores protection on every exit path.
kubectl -n "${DEMO_NAMESPACE}" delete envoyextensionpolicy tsz-request-guardrail --wait=true
sleep "${TSZ_PERF_XDS_SETTLE_SECONDS:-5}"
echo "perf: writing unprotected baseline k6 summary to ${baseline_summary}"
"${K6_BIN}" run \
  --summary-export "${baseline_summary}" \
  --env "TSZ_PERF_BASE_URL=http://127.0.0.1:${LOCAL_PORT}" \
  --env "TSZ_PERF_RATE=${TSZ_PERF_RATE:-25}" \
  --env "TSZ_PERF_DURATION=${TSZ_PERF_DURATION:-2m}" \
  --env "TSZ_PERF_PRE_ALLOCATED_VUS=${TSZ_PERF_PRE_ALLOCATED_VUS:-10}" \
  --env "TSZ_PERF_MAX_VUS=${TSZ_PERF_MAX_VUS:-50}" \
  tests/perf/extproc-regex-only.js

protected_p95="$(jq -er '.metrics.tsz_perf_request_duration.values["p(95)"]' "${protected_summary}")"
baseline_p95="$(jq -er '.metrics.tsz_perf_request_duration.values["p(95)"]' "${baseline_summary}")"
added_p95="$(awk -v protected="${protected_p95}" -v baseline="${baseline_p95}" 'BEGIN { printf "%.3f", protected - baseline }')"
echo "perf: protected p95=${protected_p95}ms baseline p95=${baseline_p95}ms added p95=${added_p95}ms"
awk -v added="${added_p95}" -v maximum="${MAX_ADDED_P95_MS}" 'BEGIN { exit !(added <= maximum) }' ||
  fail "regex-only added p95 ${added_p95}ms exceeds ${MAX_ADDED_P95_MS}ms"
