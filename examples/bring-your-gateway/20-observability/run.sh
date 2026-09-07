#!/usr/bin/env bash
set -euo pipefail
dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${dir}/../../.." && pwd)"
ns=tsz-byg-demo
kc="${TSZ_BYG_KUBECONFIG:-${TMPDIR:-/tmp}/tsz-byg-tools/tsz-byg.kubeconfig}"
TSZ_BYG_KUBECONFIG="$kc" "${root}/deployments/envoy-gateway/kind-bootstrap.sh" up
export KUBECONFIG="$kc"
kubectl apply -f "${dir}/resources.yaml"
kubectl -n "$ns" rollout status deployment/byg-mock-siem --timeout=90s
kubectl -n "$ns" set env deployment/tsz-ext-proc TSZ_AUDIT_WEBHOOK_URL=http://byg-mock-siem.tsz-byg-demo.svc.cluster.local:8080/events
kubectl -n "$ns" rollout status deployment/tsz-ext-proc --timeout=90s
TSZ_BYG_SKIP_BOOTSTRAP=1 "${root}/examples/bring-your-gateway/shared/run.sh" "$dir"
base=$((25000 + ($$ % 4000)))
kubectl -n "$ns" port-forward service/byg-mock-siem "$base:8080" >/dev/null 2>&1 & siem=$!
kubectl -n "$ns" port-forward service/tsz-ext-proc "$((base+1)):8080" >/dev/null 2>&1 & metrics=$!
trap 'kill "$siem" "$metrics" 2>/dev/null || true' EXIT
sleep 2
event="$(curl -fsS "http://127.0.0.1:$base/inspect")"
jq -e '.action == "MASK" and .policy_id == "default" and (.rid | startswith("RID-")) and (.request_id | length > 0) and (.categories | index("PII"))' <<<"$event" >/dev/null
! grep -Fq 'synthetic@example.com' <<<"$event"
curl -fsS "http://127.0.0.1:$((base+1))/metrics" | grep -q '^tsz_extproc_actions_total'
echo 'PASS 20-observability: metrics and PII-safe SIEM audit event verified'
