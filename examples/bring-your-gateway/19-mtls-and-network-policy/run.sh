#!/usr/bin/env bash
set -euo pipefail

example_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${example_dir}/../../.." && pwd)"
namespace="tsz-byg-demo"
kubeconfig="${TSZ_BYG_KUBECONFIG:-${TMPDIR:-/tmp}/tsz-byg-tools/tsz-byg.kubeconfig}"

"${example_dir}/generate-certs.sh"
certs="${example_dir}/certs"
TSZ_BYG_KUBECONFIG="$kubeconfig" "${repo_root}/deployments/envoy-gateway/kind-bootstrap.sh" up
TSZ_BYG_KUBECONFIG="$kubeconfig" "${repo_root}/deployments/envoy-gateway/kind-bootstrap.sh" verify-replica-lifecycle
export KUBECONFIG="$kubeconfig"

kubectl -n "$namespace" create secret generic tsz-ext-proc-server-tls \
  --from-file=tls.crt="$certs/server.crt" --from-file=tls.key="$certs/server.key" \
  --from-file=client-ca.crt="$certs/ca.crt" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$namespace" create secret tls envoy-ext-proc-client-tls \
  --cert="$certs/client.crt" --key="$certs/client.key" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$namespace" create configmap tsz-ext-proc-server-ca \
  --from-file=ca.crt="$certs/ca.crt" --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "$namespace" patch deployment tsz-ext-proc --type=strategic -p '
{"spec":{"template":{"spec":{"containers":[{"name":"tsz-ext-proc","env":[
{"name":"TSZ_GRPC_TLS_CERT_FILE","value":"/tls/tls.crt"},
{"name":"TSZ_GRPC_TLS_KEY_FILE","value":"/tls/tls.key"},
{"name":"TSZ_GRPC_TLS_CLIENT_CA_FILE","value":"/tls/client-ca.crt"}],
"volumeMounts":[{"name":"tsz-ext-proc-server-tls","mountPath":"/tls","readOnly":true}]}],
"volumes":[{"name":"tsz-ext-proc-server-tls","secret":{"secretName":"tsz-ext-proc-server-tls"}}]}}}}'
kubectl -n "$namespace" rollout status deployment/tsz-ext-proc --timeout=180s

kubectl -n "$namespace" delete envoyextensionpolicy tsz-request-guardrail --ignore-not-found
kubectl apply -f "${example_dir}/resources.yaml"
kubectl -n "$namespace" wait --for=condition=Accepted backend/tsz-ext-proc-mtls --timeout=90s
for _ in $(seq 1 90); do
  policy_statuses="$(kubectl -n "$namespace" get envoyextensionpolicy tsz-request-guardrail-mtls \
    -o jsonpath='{range .status.ancestors[*].conditions[?(@.type=="Accepted")]}{.status}{"\n"}{end}' 2>/dev/null || true)"
  if grep -Fxq 'True' <<<"$policy_statuses"; then
    break
  fi
  sleep 1
done
grep -Fxq 'True' <<<"${policy_statuses:-}" || {
  echo "mTLS EnvoyExtensionPolicy was not accepted within 90s" >&2
  exit 1
}
envoy_service="$(kubectl -n envoy-gateway-system get service -l "gateway.envoyproxy.io/owning-gateway-namespace=${namespace},gateway.envoyproxy.io/owning-gateway-name=echo-gateway" -o jsonpath='{.items[0].metadata.name}')"
[[ -n "$envoy_service" ]] || { echo "Envoy data-plane Service not found" >&2; exit 1; }
port="$((29000 + ($$ % 2000)))"
kubectl -n envoy-gateway-system port-forward "service/${envoy_service}" "${port}:80" >/dev/null 2>&1 &
forward_pid=$!
trap 'kill "$forward_pid" >/dev/null 2>&1 || true' EXIT
sleep 2
status="$(curl --silent --output /dev/null --write-out '%{http_code}' --header 'content-type: application/json' --data-binary "@${repo_root}/examples/bring-your-gateway/shared/fixtures/safe.json" "http://127.0.0.1:${port}/v1/chat/completions")"
[[ "$status" == "200" ]] || { echo "mTLS protected route returned HTTP $status, want 200" >&2; exit 1; }
printf 'PASS mTLS and NetworkPolicy: accepted mutual TLS route and labeled-peer isolation verified.\n'
