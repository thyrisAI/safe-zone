CONTROLLER_GEN = go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5
SETUP_ENVTEST = go run sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.24.1
ENVTEST_K8S_VERSION ?= 1.35.0

.PHONY: manifests generate test-envtest perf-extproc-regex-only perf-extproc-outage
manifests:
	$(CONTROLLER_GEN) rbac:roleName=tsz-controller crd:allowDangerousTypes=true webhook paths="./api/...;./internal/controller/..." output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac

generate:
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

test-envtest: ## Run API-server-backed controller tests with pinned envtest assets.
	@set -eu; \
	assets="$$($(SETUP_ENVTEST) use -p path $(ENVTEST_K8S_VERSION))"; \
	test -x "$$assets/kube-apiserver"; \
	KUBEBUILDER_ASSETS="$$assets" go test ./internal/controller/policyattach -run '^TestEnvtest' -count=1 -v

perf-extproc-regex-only: ## Run the BYG regex-only request-path load scenario against the Kind reference environment.
	./tests/perf/run-extproc-regex-only.sh

perf-extproc-outage: ## Stress Envoy while tsz-ext-proc is unavailable; assert bounded timeout and memory growth.
	./tests/perf/run-extproc-outage.sh
