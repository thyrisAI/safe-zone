#!/usr/bin/env bash
set -euo pipefail

example_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
namespace="tsz-byg-demo"
kubectl -n "$namespace" delete -f "${example_dir}/resources.yaml" --ignore-not-found
kubectl -n "$namespace" delete secret tsz-ext-proc-server-tls envoy-ext-proc-client-tls --ignore-not-found
kubectl -n "$namespace" delete configmap tsz-ext-proc-server-ca --ignore-not-found
kubectl -n "$namespace" set env deployment/tsz-ext-proc \
  TSZ_GRPC_TLS_CERT_FILE- TSZ_GRPC_TLS_KEY_FILE- TSZ_GRPC_TLS_CLIENT_CA_FILE-
kubectl -n "$namespace" patch deployment tsz-ext-proc --type=strategic -p \
  '{"spec":{"template":{"spec":{"containers":[{"name":"tsz-ext-proc","volumeMounts":[{"mountPath":"/tls","$patch":"delete"}]}],"volumes":[{"name":"tsz-ext-proc-server-tls","$patch":"delete"}]}}}}'
rm -rf "${example_dir}/certs"
