CONTROLLER_GEN = go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5
SETUP_ENVTEST = go run sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.24.1
ENVTEST_K8S_VERSION ?= 1.35.0

.PHONY: manifests generate test-envtest
manifests:
	$(CONTROLLER_GEN) rbac:roleName=tsz-controller crd:allowDangerousTypes=true webhook paths="./api/...;./internal/controller/..." output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac

generate:
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

test-envtest: ## Run API-server-backed controller tests with pinned envtest assets.
	KUBEBUILDER_ASSETS="$$($(SETUP_ENVTEST) use -p path $(ENVTEST_K8S_VERSION))" go test ./internal/controller/policyattach -run '^TestEnvtest' -count=1
