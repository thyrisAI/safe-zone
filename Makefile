CONTROLLER_GEN = go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5

.PHONY: manifests generate
manifests:
	$(CONTROLLER_GEN) rbac:roleName=tsz-controller crd:allowDangerousTypes=true webhook paths="./api/...;./internal/controller/..." output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac

generate:
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."
