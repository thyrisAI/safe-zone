#!/usr/bin/env bash
set -euo pipefail
example_dir="${1:-}"
if [[ -n "${example_dir}" ]]; then
  name="tsz-example-$(basename "${example_dir}")"
  name="${name//[^a-z0-9-]/-}"
  kubectl -n tsz-byg-demo delete job,configmap "${name}" --ignore-not-found
  if [[ -f "${example_dir}/resources.yaml" ]]; then
    kubectl delete -f "${example_dir}/resources.yaml" --ignore-not-found
  fi
fi
kubectl -n tsz-byg-demo delete -f deployments/envoy-gateway/tsz-ext-proc-envoy-extension-policy.yaml --ignore-not-found
# Streaming fixtures add a ConfigMap volume to the reusable mock deployment.
# Remove that template mutation before deleting the ConfigMap, otherwise the
# next buffered example's bootstrap can wait forever for a missing volume.
if kubectl -n tsz-byg-demo get deployment mock-openai >/dev/null 2>&1; then
  # Clear streaming mode before removing its fixture volume. Otherwise the
  # replacement pod starts in SSE mode without the fixture and never becomes
  # ready, leaving local suites stuck in cleanup.
  kubectl -n tsz-byg-demo set env deployment/mock-openai \
    BYG_MOCK_RESPONSE_MODE- BYG_MOCK_SSE_FIXTURE-
  kubectl -n tsz-byg-demo patch deployment mock-openai --type=strategic -p \
    '{"spec":{"template":{"spec":{"containers":[{"name":"nginx","volumeMounts":[{"mountPath":"/fixtures","$patch":"delete"}]}],"volumes":[{"name":"tsz-mock-response-fixture","$patch":"delete"}]}}}}'
  kubectl -n tsz-byg-demo rollout status deployment/mock-openai --timeout=90s
fi
kubectl -n tsz-byg-demo delete configmap tsz-mock-response-fixture --ignore-not-found
# Failure examples enable a local-only fault on the Deployment. Always remove
# it during cleanup so the next example and the bootstrap health check start
# from a healthy ext-proc process.
if kubectl -n tsz-byg-demo get deployment tsz-ext-proc >/dev/null 2>&1; then
  kubectl -n tsz-byg-demo set env deployment/tsz-ext-proc TSZ_EXAMPLE_AUDIT_FAILURE-
  kubectl -n tsz-byg-demo rollout status deployment/tsz-ext-proc --timeout=90s
fi
