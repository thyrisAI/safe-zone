#!/usr/bin/env bash
# Run one BYG example through a real Envoy Gateway route. The request body is
# never printed: examples may deliberately contain synthetic sensitive-looking
# values.
set -euo pipefail

example_dir="${1:?usage: ./run.sh <example-directory> [--response-mode buffered|streamed]}"
shift
shared_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${shared_dir}/../../.." && pwd)"
namespace="tsz-byg-demo"
kubeconfig="${TSZ_BYG_KUBECONFIG:-${TMPDIR:-/tmp}/tsz-byg-tools/tsz-byg.kubeconfig}"
[[ -d "${example_dir}" ]] || { echo "example directory does not exist: ${example_dir}" >&2; exit 2; }
for required_file in expected-status policy.json request.json; do
  [[ -f "${example_dir}/${required_file}" ]] || {
    echo "example is missing required file: ${example_dir}/${required_file}" >&2
    exit 2
  }
done
expected_status="$(<"${example_dir}/expected-status")"
[[ "${expected_status}" =~ ^[1-5][0-9][0-9]$ ]] || {
  echo "expected-status must contain one HTTP status code: ${example_dir}/expected-status" >&2
  exit 2
}
response_mode="buffered"
if [[ -f "${example_dir}/response-body-mode" ]]; then
  response_mode="$(<"${example_dir}/response-body-mode")"
fi
while [[ $# -gt 0 ]]; do
  case "$1" in
    --response-mode)
      [[ $# -ge 2 ]] || { echo "--response-mode requires buffered or streamed" >&2; exit 2; }
      response_mode="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done
case "${response_mode}" in
  buffered)
    extension_policy="${repo_root}/deployments/envoy-gateway/tsz-ext-proc-envoy-extension-policy.yaml"
    ;;
  streamed)
    extension_policy="${repo_root}/deployments/envoy-gateway/tsz-ext-proc-envoy-extension-policy-streamed.yaml"
    ;;
  *)
    echo "response mode must be buffered or streamed, got ${response_mode}" >&2
    exit 2
    ;;
esac
# A prior interrupted run can leave a kubectl port-forward process alive.
# Per-run ports keep a retry from colliding with that process.
port_base="${TSZ_BYG_EXAMPLE_PORT_BASE:-$((20000 + ($$ % 10000)))}"
envoy_port="${port_base}"
mock_port="$((port_base + 1))"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

wait_for_policy_accepted() {
  local resource="$1"
  local statuses

  # Envoy Gateway reports policy acceptance on each target ancestor rather
  # than in the resource's top-level status.conditions array.
  for _ in $(seq 1 60); do
    statuses="$(kubectl -n "${namespace}" get "${resource}" \
      -o jsonpath='{range .status.ancestors[*].conditions[?(@.type=="Accepted")]}{.status}{"\n"}{end}' \
      2>/dev/null || true)"
    if grep -Fxq 'True' <<<"${statuses}"; then
      return 0
    fi
    sleep 1
  done

  echo "Timed out waiting for ${resource} Accepted=True" >&2
  return 1
}

if [[ "${TSZ_BYG_SKIP_BOOTSTRAP:-0}" != "1" ]]; then
  # Pass the path explicitly so Kind refreshes it if Docker has reassigned the
  # API-server port of an already-existing cluster.
  TSZ_BYG_KUBECONFIG="${kubeconfig}" \
    "${repo_root}/deployments/envoy-gateway/kind-bootstrap.sh" up
  TSZ_BYG_KUBECONFIG="${kubeconfig}" \
    "${repo_root}/deployments/envoy-gateway/kind-bootstrap.sh" verify-replica-lifecycle
fi
export KUBECONFIG="${kubeconfig}"

# Authentication and rate-limit examples attach route-scoped Gateway policies.
# Remove their known test fixtures before every run so a prior interrupted
# example cannot short-circuit a later, unrelated guardrail scenario.
kubectl -n "${namespace}" delete securitypolicy tsz-jwt-authentication --ignore-not-found
kubectl -n "${namespace}" delete configmap tsz-jwt-authentication --ignore-not-found
kubectl -n "${namespace}" delete backendtrafficpolicy tsz-local-rate-limit --ignore-not-found
# Removed in the v1.8.3 response-only fallback design; delete a stale copy
# from an earlier run so it cannot obscure the real filter ordering.
kubectl -n "${namespace}" delete backendtrafficpolicy tsz-native-local-reply-marker --ignore-not-found

name="tsz-example-$(basename "${example_dir}")"
name="${name//[^a-z0-9-]/-}"
kubectl -n "${namespace}" delete job "${name}" --ignore-not-found
kubectl -n "${namespace}" delete configmap "${name}" --ignore-not-found
kubectl -n "${namespace}" create configmap "${name}" --from-file=policy.json="${example_dir}/policy.json"
cat <<EOF | kubectl apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: ${name}
  namespace: ${namespace}
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: activate
          image: thyris-sz:local
          imagePullPolicy: IfNotPresent
          command: ["/app/tsz-policy", "-name", "default", "-file", "/policy/policy.json"]
          env:
            - name: DB_DSN
              value: postgres://postgres:postgres@postgres.tsz-byg-demo.svc.cluster.local:5432/thyris?sslmode=disable&TimeZone=Europe/Istanbul
            - name: REDIS_URL
              value: redis://:thyrisredis@redis.tsz-byg-demo.svc.cluster.local:6379/0
          volumeMounts:
            - name: policy
              mountPath: /policy
              readOnly: true
      volumes:
        - name: policy
          configMap:
            name: ${name}
EOF
kubectl -n "${namespace}" wait --for=condition=complete "job/${name}" --timeout=90s
# The shared examples exercise the manual/preview attachment, whose trusted
# policy identity is the gateway-owned X-TSZ-Policy header. Restart after
# activation so every processor replica starts with the newly active snapshot
# rather than racing its periodic policy-cache reconciliation.
kubectl -n "${namespace}" set env deployment/tsz-ext-proc TSZ_POLICY_RESOLUTION_MODE=header
kubectl -n "${namespace}" rollout status deployment/tsz-ext-proc --timeout=90s
kubectl apply -f "${extension_policy}"
wait_for_policy_accepted envoyextensionpolicy/tsz-request-guardrail
wait_for_policy_accepted clienttrafficpolicy/tsz-route-policy-identity
if [[ -f "${example_dir}/resources.yaml" ]]; then
  kubectl apply -f "${example_dir}/resources.yaml"
fi
if [[ -f "${example_dir}/jwt-token" ]]; then
  wait_for_policy_accepted securitypolicy/tsz-jwt-authentication
fi
if [[ -f "${example_dir}/rate-limit-requests" ]]; then
  wait_for_policy_accepted backendtrafficpolicy/tsz-local-rate-limit
fi

# Replace the bootstrap's simple nginx response with the safety-preserving
# inspection mock. The mock never retains raw request content.
kubectl -n "${namespace}" set image deployment/mock-openai nginx=thyris-sz:local
kubectl -n "${namespace}" patch deployment/mock-openai --type=strategic -p \
  '{"spec":{"template":{"spec":{"containers":[{"name":"nginx","command":["/app/byg-mock-openai"],"volumeMounts":[{"name":"tsz-mock-response-fixture","mountPath":"/fixtures","readOnly":true}]}],"volumes":[{"name":"tsz-mock-response-fixture","configMap":{"name":"tsz-mock-response-fixture"}}]}}}}'
kubectl -n "${namespace}" delete configmap tsz-mock-response-fixture --ignore-not-found
if [[ -f "${example_dir}/mock-sse-fixture" ]]; then
  kubectl -n "${namespace}" create configmap tsz-mock-response-fixture --from-file=response.sse="${example_dir}/mock-sse-fixture"
  kubectl -n "${namespace}" set env deployment/mock-openai BYG_MOCK_RESPONSE_MODE=sse BYG_MOCK_SSE_FIXTURE=/fixtures/response.sse
else
  kubectl -n "${namespace}" create configmap tsz-mock-response-fixture --from-literal=.keep=
  kubectl -n "${namespace}" set env deployment/mock-openai BYG_MOCK_RESPONSE_MODE- BYG_MOCK_SSE_FIXTURE-
fi
if [[ -f "${example_dir}/mock-response-content" ]]; then
  kubectl -n "${namespace}" set env deployment/mock-openai "BYG_MOCK_RESPONSE_CONTENT=$(<"${example_dir}/mock-response-content")"
else
  kubectl -n "${namespace}" set env deployment/mock-openai BYG_MOCK_RESPONSE_CONTENT-
fi
kubectl -n "${namespace}" rollout status deployment/mock-openai --timeout=90s

if [[ "$(basename "${example_dir}")" == "05-fail-open" || "$(basename "${example_dir}")" == "06-fail-closed" ]]; then
	# This affects only the local example Deployment and lets the stream-pinned
	# failure policy decide after an otherwise safe request is processed.
	kubectl -n "${namespace}" set env deployment/tsz-ext-proc TSZ_EXAMPLE_AUDIT_FAILURE=1
else
	# Do not let the fail-mode fixture leak into the next example run.
	kubectl -n "${namespace}" set env deployment/tsz-ext-proc TSZ_EXAMPLE_AUDIT_FAILURE-
fi
kubectl -n "${namespace}" rollout status deployment/tsz-ext-proc --timeout=90s

envoy_service="$(kubectl -n envoy-gateway-system get service -l "gateway.envoyproxy.io/owning-gateway-namespace=${namespace},gateway.envoyproxy.io/owning-gateway-name=echo-gateway" -o jsonpath='{.items[0].metadata.name}')"
[[ -n "${envoy_service}" ]] || { echo "Envoy data-plane Service not found" >&2; exit 1; }
work_dir="$(mktemp -d)"
cleanup() {
  [[ -n "${forward_pid:-}" ]] && kill "${forward_pid}" >/dev/null 2>&1 || true
  [[ -n "${mock_forward_pid:-}" ]] && kill "${mock_forward_pid}" >/dev/null 2>&1 || true
  rm -rf "${work_dir}"
}
trap cleanup EXIT
kubectl -n envoy-gateway-system port-forward "service/${envoy_service}" "${envoy_port}:80" >"${work_dir}/port-forward.log" 2>&1 &
forward_pid=$!
kubectl -n "${namespace}" port-forward service/mock-openai "${mock_port}:8080" >"${work_dir}/mock-port-forward.log" 2>&1 &
mock_forward_pid=$!
sleep 2
before_sequence="$(curl --silent "http://127.0.0.1:${mock_port}/inspect" | jq -r '.sequence')"
curl_auth_args=()
if [[ -f "${example_dir}/jwt-token" ]]; then
  unauthenticated_status="$(curl --silent --output /dev/null --write-out '%{http_code}' \
    --header 'content-type: application/json' --data-binary "@${example_dir}/request.json" \
    "http://127.0.0.1:${envoy_port}/v1/chat/completions")"
  # Envoy can call ext_proc only on this JWT local-reply path. There is no
  # policy-pinned request state, so the default global closed fallback returns
  # TSZ's safe response-stage block rather than silently allowing it.
  [[ "${unauthenticated_status}" == "403" ]] || { echo "missing JWT returned HTTP ${unauthenticated_status}, want 403" >&2; exit 1; }
  invalid_token_status="$(curl --silent --output /dev/null --write-out '%{http_code}' \
    --header 'content-type: application/json' --header 'authorization: Bearer invalid.jwt.token' \
    --data-binary "@${example_dir}/request.json" "http://127.0.0.1:${envoy_port}/v1/chat/completions")"
  [[ "${invalid_token_status}" == "403" ]] || { echo "invalid JWT returned HTTP ${invalid_token_status}, want 403" >&2; exit 1; }
  curl_auth_args=(--header "authorization: Bearer $(<"${example_dir}/jwt-token")")
fi
response_file="${work_dir}/response.body"
status="$(curl --silent --output "${response_file}" --write-out '%{http_code}' \
  --header 'content-type: application/json' \
  --header 'X-TSZ-Policy: client-must-not-win' \
  "${curl_auth_args[@]+"${curl_auth_args[@]}"}" \
  --data-binary "@${example_dir}/request.json" \
  "http://127.0.0.1:${envoy_port}/v1/chat/completions")"
[[ "${status}" == "${expected_status}" ]] || {
  cat "${work_dir}/port-forward.log" >&2
  echo "expected HTTP ${expected_status}, got ${status}" >&2
  exit 1
}
if [[ "${status}" == "400" || "${status}" == "403" ]]; then
  grep -Fq '"policy_id":"default"' "${response_file}" || {
    echo "block response did not identify the route-owned default policy" >&2; exit 1;
  }
else
  if [[ -f "${example_dir}/mock-sse-fixture" ]]; then
    grep -Fq 'data: [DONE]' "${response_file}" || { echo "stream did not terminate with [DONE]" >&2; exit 1; }
  else
    grep -Fq 'chatcmpl-kind-mock' "${response_file}" || {
      echo "unexpected mock upstream response" >&2; exit 1;
    }
  fi
fi
if [[ -f "${example_dir}/expect-response-mask" || -f "${example_dir}/expect-response-absent" ]]; then
	raw_response="$(<"${example_dir}/mock-response-content")"
	! grep -Fq "$raw_response" "${response_file}" || { echo "raw upstream response reached client" >&2; exit 1; }
fi
if [[ -f "${example_dir}/expect-response-block" ]]; then
	grep -Fq '"code":"TSZ_RESPONSE_GUARDRAIL_BLOCKED"' "${response_file}" || {
		echo "response block did not return the safe TSZ error code" >&2; exit 1;
	}
fi
if [[ -f "${example_dir}/expect-sse-absent" ]]; then
  while IFS= read -r absent || [[ -n "${absent}" ]]; do
    [[ -z "${absent}" ]] && continue
    ! grep -Fq "${absent}" "${response_file}" || { echo "unsafe SSE value reached client" >&2; exit 1; }
  done <"${example_dir}/expect-sse-absent"
fi
if [[ -f "${example_dir}/expect-sse-present" ]]; then
  while IFS= read -r present || [[ -n "${present}" ]]; do
    [[ -z "${present}" ]] && continue
    grep -Fq "${present}" "${response_file}" || { echo "expected SSE value missing from client response" >&2; exit 1; }
  done <"${example_dir}/expect-sse-present"
fi
if [[ -f "${example_dir}/expect-async-audit" ]]; then
  async_audit_observed=0
  for _ in $(seq 1 30); do
    while IFS= read -r processor_pod; do
      [[ -z "${processor_pod}" ]] && continue
      if kubectl -n "${namespace}" exec "${processor_pod}" -- wget -qO- http://127.0.0.1:8080/metrics 2>/dev/null |
        grep -Eq '^tsz_extproc_actions_total\{action="AUDIT_ONLY",policy="default",stage="response"\} [1-9][0-9]*(\.[0-9]+)?$'; then
        async_audit_observed=1
        break
      fi
    done < <(kubectl -n "${namespace}" get pods -l app.kubernetes.io/name=tsz-ext-proc -o name)
    [[ "${async_audit_observed}" == "1" ]] && break
    sleep 1
  done
  [[ "${async_audit_observed}" == "1" ]] || {
    echo "completed stream did not produce an AsyncAudit response decision" >&2; exit 1;
  }
fi
if [[ -f "${example_dir}/rate-limit-requests" ]]; then
  rate_limit_requests="$(<"${example_dir}/rate-limit-requests")"
  [[ "${rate_limit_requests}" =~ ^[1-9][0-9]*$ ]] || {
    echo "rate-limit-requests must be a positive integer" >&2; exit 2;
  }
  for request_number in $(seq 2 "$((rate_limit_requests + 1))"); do
    limited_status="$(curl --silent --output /dev/null --write-out '%{http_code}' \
      --header 'content-type: application/json' --header 'X-TSZ-Policy: client-must-not-win' \
      "${curl_auth_args[@]+"${curl_auth_args[@]}"}" --data-binary "@${example_dir}/request.json" \
      "http://127.0.0.1:${envoy_port}/v1/chat/completions")"
    expected_limited_status=200
    if [[ "$request_number" -gt "$rate_limit_requests" ]]; then
      # The Envoy local 429 body is not an OpenAI response. With the response
      # policy's default closed mode, TSZ replaces it with its safe 403 rather
      # than forwarding an uninspectable body.
      expected_limited_status=403
    fi
    [[ "$limited_status" == "$expected_limited_status" ]] || {
      echo "request ${request_number} returned HTTP ${limited_status}, want ${expected_limited_status}" >&2; exit 1;
    }
  done
fi
after="$(curl --silent "http://127.0.0.1:${mock_port}/inspect")"
after_sequence="$(jq -r '.sequence' <<<"${after}")"
if [[ -f "${example_dir}/expect-response-block" ]]; then
	# A response block happens after the upstream has returned a response. It
	# must therefore reach the mock, while its raw response body must not reach
	# the client (verified above).
	[[ "${before_sequence}" != "${after_sequence}" ]] || {
		echo "response block did not reach the mock upstream" >&2; exit 1;
	}
elif [[ "${status}" == "400" || "${status}" == "403" ]]; then
	[[ "${before_sequence}" == "${after_sequence}" ]] || {
		echo "blocked request reached the mock upstream" >&2; exit 1;
	}
fi
if [[ -f "${example_dir}/mock-sse-fixture" ]]; then
  [[ "${before_sequence}" != "${after_sequence}" ]] || {
    echo "streaming example did not reach the mock upstream" >&2; exit 1;
  }
fi
if [[ "$(basename "${example_dir}")" == "02-request-masking" || -f "${example_dir}/expect-mask" ]]; then
  [[ "$(jq -r '.masked' <<<"${after}")" == "true" ]] || {
    echo "mock upstream did not observe an EMAIL mask placeholder" >&2; exit 1;
  }
  [[ "$(jq -r '.contains_synthetic_email' <<<"${after}")" == "false" ]] || {
    echo "synthetic PII reached the mock upstream without masking" >&2; exit 1;
  }
fi
if [[ "$(basename "${example_dir}")" == "05-fail-open" ]]; then
  expected_hash="$(sha256_file "${example_dir}/request.json")"
  [[ "$(jq -r '.sha256' <<<"${after}")" == "${expected_hash}" ]] || {
    echo "fail-open changed the upstream request body" >&2; exit 1;
  }
fi
if [[ -f "${example_dir}/expect-unchanged" ]]; then
  expected_hash="$(sha256_file "${example_dir}/request.json")"
  [[ "$(jq -r '.sha256' <<<"${after}")" == "${expected_hash}" ]] || {
    echo "audit-only/safe path changed the upstream request body" >&2; exit 1;
  }
fi
if [[ -f "${example_dir}/expect-shared-processor" ]]; then
  ready_replicas="$(kubectl -n "${namespace}" get deployment/tsz-ext-proc -o jsonpath='{.status.readyReplicas}')"
  min_replicas="$(kubectl -n "${namespace}" get hpa/tsz-ext-proc -o jsonpath='{.spec.minReplicas}')"
  unavailable="$(kubectl -n "${namespace}" get pdb/tsz-ext-proc -o jsonpath='{.spec.maxUnavailable}')"
  [[ "${ready_replicas}" -ge 2 && "${min_replicas}" -ge 2 && "${unavailable}" == "1" ]] || {
    echo "shared processor availability profile is not ready_replicas>=2, minReplicas>=2, maxUnavailable=1" >&2; exit 1;
  }
fi
printf 'PASS %s: HTTP %s\n' "$(basename "${example_dir}")" "${status}"
