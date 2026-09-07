#!/usr/bin/env bash
# Runs every checked-in BYG example against the same local Kind cluster.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export KUBECONFIG="${TSZ_BYG_KUBECONFIG:-${TMPDIR:-/tmp}/tsz-byg-tools/tsz-byg.kubeconfig}"
for example in "${root}"/examples/bring-your-gateway/[0-9][0-9]-*; do
  if [[ -f "${example}/run.sh" ]]; then
    bash "${example}/run.sh"
  else
    "${root}/examples/bring-your-gateway/shared/run.sh" "${example}"
  fi
  if [[ -f "${example}/cleanup.sh" ]]; then
    bash "${example}/cleanup.sh"
  else
    "${root}/examples/bring-your-gateway/shared/cleanup.sh" "${example}"
  fi
done
