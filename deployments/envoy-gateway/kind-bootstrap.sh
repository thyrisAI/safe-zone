#!/usr/bin/env bash

set -euo pipefail

readonly KIND_VERSION="v0.32.0"
readonly KUBERNETES_VERSION="v1.35.5"
readonly KIND_NODE_IMAGE="kindest/node:v1.35.5@sha256:ce977ae6d65918d0b58a5f8b5e940429c2ce42fa3a5619ec2bbc60b949c0ac95"
readonly KUBECTL_VERSION="v1.35.5"
readonly HELM_VERSION="v3.20.2"
readonly ENVOY_GATEWAY_VERSION="v1.8.3"
readonly GATEWAY_API_VERSION="v1.5.1"

readonly CLUSTER_NAME="${TSZ_BYG_CLUSTER_NAME:-tsz-byg}"
readonly DEMO_NAMESPACE="tsz-byg-demo"
readonly GATEWAY_NAME="echo-gateway"
readonly LOCAL_PORT="${TSZ_BYG_LOCAL_PORT:-18080}"
readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly TOOLS_DIR="${TSZ_BYG_TOOLS_DIR:-${TMPDIR:-/tmp}/tsz-byg-tools}"
readonly KUBECONFIG_PATH="${TSZ_BYG_KUBECONFIG:-${TOOLS_DIR}/${CLUSTER_NAME}.kubeconfig}"

KIND="${TOOLS_DIR}/kind-${KIND_VERSION}"
KUBECTL="${TOOLS_DIR}/kubectl-${KUBECTL_VERSION}"
HELM="${TOOLS_DIR}/helm-${HELM_VERSION}"

log() {
  printf '[tsz-byg] %s\n' "$*"
}

fail() {
  printf '[tsz-byg] ERROR: %s\n' "$*" >&2
  exit 1
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

verify_checksum() {
  local file="$1"
  local expected="$2"
  local actual
  actual="$(sha256_file "$file")"
  [[ "$actual" == "$expected" ]] || fail "Checksum mismatch for ${file}"
}

platform() {
  case "$(uname -s)" in
    Darwin) printf 'darwin' ;;
    Linux) printf 'linux' ;;
    *) fail "Unsupported operating system: $(uname -s)" ;;
  esac
}

architecture() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64' ;;
    arm64|aarch64) printf 'arm64' ;;
    *) fail "Unsupported architecture: $(uname -m)" ;;
  esac
}

install_tools() {
  local os arch url checksum archive temp_dir
  os="$(platform)"
  arch="$(architecture)"
  mkdir -p "$TOOLS_DIR"

  if [[ ! -x "$KIND" ]]; then
    log "Downloading Kind ${KIND_VERSION}"
    url="https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-${os}-${arch}"
    curl --fail --location --silent --show-error "$url" --output "${KIND}.download"
    checksum="$(curl --fail --location --silent --show-error "${url}.sha256sum" | awk '{print $1}')"
    verify_checksum "${KIND}.download" "$checksum"
    mv "${KIND}.download" "$KIND"
    chmod +x "$KIND"
  fi

  if [[ ! -x "$KUBECTL" ]]; then
    log "Downloading kubectl ${KUBECTL_VERSION}"
    url="https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${os}/${arch}/kubectl"
    curl --fail --location --silent --show-error "$url" --output "${KUBECTL}.download"
    checksum="$(curl --fail --location --silent --show-error "${url}.sha256")"
    verify_checksum "${KUBECTL}.download" "$checksum"
    mv "${KUBECTL}.download" "$KUBECTL"
    chmod +x "$KUBECTL"
  fi

  if [[ ! -x "$HELM" ]]; then
    log "Downloading Helm ${HELM_VERSION}"
    archive="${TOOLS_DIR}/helm-${HELM_VERSION}-${os}-${arch}.tar.gz"
    url="https://get.helm.sh/helm-${HELM_VERSION}-${os}-${arch}.tar.gz"
    curl --fail --location --silent --show-error "$url" --output "$archive"
    checksum="$(curl --fail --location --silent --show-error "${url}.sha256sum" | awk '{print $1}')"
    verify_checksum "$archive" "$checksum"
    temp_dir="$(mktemp -d)"
    tar -xzf "$archive" -C "$temp_dir"
    mv "${temp_dir}/${os}-${arch}/helm" "$HELM"
    chmod +x "$HELM"
    rm -rf "$temp_dir" "$archive"
  fi
}

cluster_exists() {
  "$KIND" get clusters 2>/dev/null | grep -Fxq "$CLUSTER_NAME"
}

ensure_cluster() {
  local server_version
  if cluster_exists; then
    log "Kind cluster ${CLUSTER_NAME} already exists"
    "$KIND" export kubeconfig --name "$CLUSTER_NAME" --kubeconfig "$KUBECONFIG_PATH"
  else
    log "Creating Kind ${KIND_VERSION} cluster with Kubernetes ${KUBERNETES_VERSION}"
    "$KIND" create cluster \
      --name "$CLUSTER_NAME" \
      --image "$KIND_NODE_IMAGE" \
      --kubeconfig "$KUBECONFIG_PATH" \
      --wait 180s
  fi

  server_version="$("$KUBECTL" --kubeconfig "$KUBECONFIG_PATH" version -o yaml | \
    awk '/^  gitVersion:/ { version=$2 } END { print version }')"
  [[ "$server_version" == "$KUBERNETES_VERSION" ]] || \
    fail "Cluster server is ${server_version:-unknown}; expected ${KUBERNETES_VERSION}"
  log "Kubernetes server version verified: ${server_version}"
}

kubectl() {
  "$KUBECTL" --kubeconfig "$KUBECONFIG_PATH" "$@"
}

wait_for_route_accepted() {
  local statuses
  for _ in $(seq 1 180); do
    statuses="$(kubectl -n "$DEMO_NAMESPACE" get httproute mock-openai \
      -o jsonpath='{range .status.parents[*].conditions[?(@.type=="Accepted")]}{.status}{"\n"}{end}' 2>/dev/null || true)"
    if grep -Fxq 'True' <<<"$statuses"; then
      log "HTTPRoute mock-openai condition met: Accepted=True"
      return
    fi
    sleep 1
  done
  fail "Timed out waiting for HTTPRoute mock-openai Accepted=True"
}

install_envoy_gateway() {
  log "Installing Envoy Gateway ${ENVOY_GATEWAY_VERSION}"
  "$HELM" upgrade --install eg \
    oci://docker.io/envoyproxy/gateway-helm \
    --version "$ENVOY_GATEWAY_VERSION" \
    --namespace envoy-gateway-system \
    --create-namespace \
    --kubeconfig "$KUBECONFIG_PATH" \
    --wait \
    --timeout 5m

  local actual_bundle
  actual_bundle="$(kubectl get crd gateways.gateway.networking.k8s.io -o jsonpath='{.metadata.annotations.gateway\.networking\.k8s\.io/bundle-version}')"
  [[ "$actual_bundle" == "$GATEWAY_API_VERSION" ]] || \
    fail "Gateway API bundle is ${actual_bundle:-missing}; expected ${GATEWAY_API_VERSION}"
  log "Gateway API CRD bundle verified: ${actual_bundle}"
}

apply_demo() {
  log "Applying Gateway, HTTPRoute, and mock OpenAI backend"
  kubectl apply -f "${SCRIPT_DIR}/echo-demo.yaml"
  kubectl -n "$DEMO_NAMESPACE" rollout status deployment/mock-openai --timeout=180s
  kubectl -n "$DEMO_NAMESPACE" wait \
    --for=condition=Programmed "gateway/${GATEWAY_NAME}" --timeout=180s
  wait_for_route_accepted
}

apply_policy_dependencies() {
  log "Applying pinned PostgreSQL and Redis dependencies"
  kubectl -n "$DEMO_NAMESPACE" create configmap tsz-byg-postgres-init \
    --from-file=init.sql="${SCRIPT_DIR}/../../init.sql" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl -n "$DEMO_NAMESPACE" create configmap tsz-byg-policy-migrations \
    --from-file=000001_create_policy_snapshots.up.sql="${SCRIPT_DIR}/../../internal/extproc/policy/migrations/000001_create_policy_snapshots.up.sql" \
    --from-file=000002_create_route_policy_bindings.up.sql="${SCRIPT_DIR}/../../internal/extproc/policy/migrations/000002_create_route_policy_bindings.up.sql" \
    --from-file=000003_create_owner_crd_refs.up.sql="${SCRIPT_DIR}/../../internal/extproc/policy/migrations/000003_create_owner_crd_refs.up.sql" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl apply -f "${SCRIPT_DIR}/policy-dependencies.yaml"
  kubectl -n "$DEMO_NAMESPACE" rollout status deployment/postgres --timeout=180s
  kubectl -n "$DEMO_NAMESPACE" rollout status deployment/redis --timeout=180s

  # Jobs do not rerun after completion. Recreating this idempotent migration
  # job gives fresh and reused clusters the same verified schema.
  kubectl -n "$DEMO_NAMESPACE" delete job tsz-policy-migrations --ignore-not-found
  # Kubernetes deletion is asynchronous. Waiting avoids an AlreadyExists race
  # when examples bootstrap the same reusable Kind cluster back-to-back.
  for _ in $(seq 1 30); do
    kubectl -n "$DEMO_NAMESPACE" get job tsz-policy-migrations >/dev/null 2>&1 || break
    sleep 1
  done
  kubectl -n "$DEMO_NAMESPACE" get job tsz-policy-migrations >/dev/null 2>&1 && \
    fail "Timed out deleting tsz-policy-migrations"
  kubectl apply -f "${SCRIPT_DIR}/policy-dependencies.yaml"
  kubectl -n "$DEMO_NAMESPACE" wait --for=condition=complete job/tsz-policy-migrations --timeout=180s
}

run_policy_integration_tests() {
  local temp_dir postgres_pid redis_pid
  temp_dir="$(mktemp -d)"
  kubectl -n "$DEMO_NAMESPACE" port-forward service/postgres 15432:5432 >"${temp_dir}/postgres.log" 2>&1 &
  postgres_pid=$!
  kubectl -n "$DEMO_NAMESPACE" port-forward service/redis 16379:6379 >"${temp_dir}/redis.log" 2>&1 &
  redis_pid=$!
  cleanup_policy_port_forwards() {
    kill "$postgres_pid" "$redis_pid" >/dev/null 2>&1 || true
    wait "$postgres_pid" "$redis_pid" >/dev/null 2>&1 || true
    rm -rf "$temp_dir"
  }
  trap cleanup_policy_port_forwards RETURN

  sleep 2
  kill -0 "$postgres_pid" 2>/dev/null || fail "PostgreSQL port-forward did not start"
  kill -0 "$redis_pid" 2>/dev/null || fail "Redis port-forward did not start"
  TSZ_POLICY_TEST_DSN="postgres://postgres:postgres@127.0.0.1:15432/thyris?sslmode=disable" \
    TSZ_POLICY_TEST_REDIS_URL="redis://:thyrisredis@127.0.0.1:16379/0" \
    go test ./internal/extproc/policy/...
  cleanup_policy_port_forwards
  trap - RETURN
}

verify() {
  local envoy_service temp_dir port_forward_pid response_file status_code

  kubectl cluster-info >/dev/null
  kubectl -n "$DEMO_NAMESPACE" wait \
    --for=condition=Programmed "gateway/${GATEWAY_NAME}" --timeout=60s
  wait_for_route_accepted

  envoy_service="$(kubectl -n envoy-gateway-system get service \
    -l "gateway.envoyproxy.io/owning-gateway-namespace=${DEMO_NAMESPACE},gateway.envoyproxy.io/owning-gateway-name=${GATEWAY_NAME}" \
    -o jsonpath='{.items[0].metadata.name}')"
  [[ -n "$envoy_service" ]] || fail "Envoy data-plane service was not found"

  temp_dir="$(mktemp -d)"
  response_file="${temp_dir}/response.json"
  kubectl -n envoy-gateway-system port-forward \
    "service/${envoy_service}" "${LOCAL_PORT}:80" >"${temp_dir}/port-forward.log" 2>&1 &
  port_forward_pid=$!

  cleanup_port_forward() {
    kill "$port_forward_pid" >/dev/null 2>&1 || true
    wait "$port_forward_pid" >/dev/null 2>&1 || true
    rm -rf "$temp_dir"
  }
  trap cleanup_port_forward EXIT INT TERM

  for _ in $(seq 1 30); do
    if curl --silent --output "$response_file" \
      --write-out '%{http_code}' \
      "http://127.0.0.1:${LOCAL_PORT}/v1/chat/completions" | grep -Fxq '200'; then
      break
    fi
    sleep 1
  done

  status_code="$(curl --silent --output "$response_file" \
    --write-out '%{http_code}' \
    "http://127.0.0.1:${LOCAL_PORT}/v1/chat/completions")"
  [[ "$status_code" == "200" ]] || {
    cat "${temp_dir}/port-forward.log" >&2
    fail "Envoy request returned HTTP ${status_code}"
  }
  grep -Fq 'chatcmpl-kind-mock' "$response_file" || fail "Unexpected mock response"

  log "Verified real Envoy route: HTTP ${status_code} from ${envoy_service}"
  cat "$response_file"
  printf '\n'
  cleanup_port_forward
  trap - EXIT INT TERM
}

up() {
  command -v docker >/dev/null 2>&1 || fail "Docker is required"
  docker info >/dev/null 2>&1 || fail "Docker daemon is not running"
  install_tools
  ensure_cluster
  install_envoy_gateway
  apply_demo
  apply_policy_dependencies
  verify
  log "Cluster is ready. Kubeconfig: ${KUBECONFIG_PATH}"
}

verify_policy_store_readiness() {
  install_tools
  kubectl -n "$DEMO_NAMESPACE" rollout status deployment/postgres --timeout=180s
  kubectl -n "$DEMO_NAMESPACE" rollout status deployment/redis --timeout=180s
  kubectl -n "$DEMO_NAMESPACE" wait --for=condition=complete job/tsz-policy-migrations --timeout=180s
  run_policy_integration_tests
}

verify_replica_lifecycle() {
  local pods pod_count policy_versions stable_checks
  command -v docker >/dev/null 2>&1 || fail "Docker is required"
  docker info >/dev/null 2>&1 || fail "Docker daemon is not running"
  install_tools
  kubectl -n "$DEMO_NAMESPACE" rollout status deployment/postgres --timeout=180s
  kubectl -n "$DEMO_NAMESPACE" rollout status deployment/redis --timeout=180s
  kubectl -n "$DEMO_NAMESPACE" wait --for=condition=complete job/tsz-policy-migrations --timeout=180s

  log "Building and loading the local tsz-ext-proc image into Kind"
  docker build -t thyris-sz:local "${SCRIPT_DIR}/../.."
  "$KIND" load docker-image thyris-sz:local --name "$CLUSTER_NAME"
  kubectl apply -f "${SCRIPT_DIR}/tsz-ext-proc.yaml"
  # The local tag is intentionally stable; restart so a repeated verification
  # always exercises the image just loaded into the Kind node.
  kubectl -n "$DEMO_NAMESPACE" rollout restart deployment/tsz-ext-proc
  kubectl -n "$DEMO_NAMESPACE" rollout status deployment/tsz-ext-proc --timeout=180s

  # A rolling update can briefly retain a terminating, label-matched old pod.
  # Count only non-terminating Running pods whose every container is Ready,
  # and require the expected count twice so a transient state cannot pass.
  stable_checks=0
  for _ in $(seq 1 30); do
    pods=""
    while IFS= read -r pod; do
      [[ -n "$pod" ]] || continue
      local deleting ready_states
      deleting="$(kubectl -n "$DEMO_NAMESPACE" get pod "$pod" -o jsonpath='{.metadata.deletionTimestamp}')"
      ready_states="$(kubectl -n "$DEMO_NAMESPACE" get pod "$pod" -o jsonpath='{range .status.containerStatuses[*]}{.ready}{" "}{end}')"
      if [[ -z "$deleting" && -n "$ready_states" && "$ready_states" != *false* ]]; then
        pods+="${pod}"$'\n'
      fi
    done < <(kubectl -n "$DEMO_NAMESPACE" get pods \
      -l app.kubernetes.io/name=tsz-ext-proc \
      --field-selector=status.phase=Running \
      -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
    pod_count="$(printf '%s\n' "$pods" | sed '/^$/d' | wc -l | tr -d ' ')"
    if [[ "$pod_count" == "2" ]]; then
      stable_checks=$((stable_checks + 1))
      [[ "$stable_checks" == "2" ]] && break
    else
      stable_checks=0
    fi
    sleep 1
  done
  [[ "$stable_checks" == "2" ]] || fail "Expected exactly two stable Running/Ready tsz-ext-proc pods, found ${pod_count}"
  while IFS= read -r pod; do
    [[ -n "$pod" ]] || continue
    kubectl -n "$DEMO_NAMESPACE" exec "$pod" -- wget -qO- http://127.0.0.1:8080/readyz | grep -Fxq READY || \
      fail "tsz-ext-proc pod ${pod} is not ready"
    policy_versions="$(kubectl -n "$DEMO_NAMESPACE" exec "$pod" -- wget -qO- http://127.0.0.1:8080/debug/policy-versions)" || \
      fail "could not read policy versions from ${pod}"
    log "tsz-ext-proc pod ${pod} policy versions: ${policy_versions}"
  done <<<"$pods"
}

down() {
  install_tools
  if cluster_exists; then
    log "Deleting Kind cluster ${CLUSTER_NAME}"
    "$KIND" delete cluster --name "$CLUSTER_NAME" --kubeconfig "$KUBECONFIG_PATH"
  else
    log "Kind cluster ${CLUSTER_NAME} is already absent"
  fi
  rm -f "$KUBECONFIG_PATH"
}

usage() {
  printf 'Usage: %s {up|verify|verify-policy-store-readiness|verify-replica-lifecycle|down}\n' "$0"
}

case "${1:-up}" in
  up) up ;;
  verify)
    install_tools
    verify
    ;;
  verify-policy-store-readiness) verify_policy_store_readiness ;;
  verify-replica-lifecycle) verify_replica_lifecycle ;;
  down) down ;;
  *) usage; exit 2 ;;
esac
