#!/usr/bin/env bash
set -euo pipefail
dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
kubectl -n tsz-byg-demo delete -f "${dir}/resources.yaml" --ignore-not-found
kubectl -n tsz-byg-demo set env deployment/tsz-ext-proc TSZ_AUDIT_WEBHOOK_URL-
