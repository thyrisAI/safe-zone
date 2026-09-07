package policyattach

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	alpha "thyris-sz/api/v1alpha1"
	beta "thyris-sz/api/v1beta1"
	"thyris-sz/internal/controller"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"
)

// TestEnvtestPolicyVersionUpgrade starts with the actual pre-graduation CRD,
// creates stored alpha objects, then upgrades the CRD in place. Fake clients
// cannot exercise API-server conversion, defaulting or storage-version state.
func TestEnvtestPolicyVersionUpgrade(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS is not set; run make test-envtest")
	}
	ctx := context.Background()
	root := filepath.Join("..", "..", "..")
	readCRD := func(path string) *apiextensionsv1.CustomResourceDefinition {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := yaml.Unmarshal(data, crd); err != nil {
			t.Fatal(err)
		}
		return crd
	}
	environment := &envtest.Environment{CRDs: []*apiextensionsv1.CustomResourceDefinition{readCRD("api/testdata/v1alpha1-crd.yaml")}}
	config, err := environment.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Errorf("stop envtest: %v", err)
		}
	})
	scheme, err := controller.NewScheme()
	if err != nil {
		t.Fatal(err)
	}
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	newClient := func() client.Client {
		t.Helper()
		c, err := client.New(config, client.Options{Scheme: scheme})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := newClient()
	fixture, err := os.ReadFile(filepath.Join(root, "api/testdata/policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	old := &alpha.TSZGuardrailPolicy{}
	if err := json.Unmarshal(fixture, old); err != nil {
		t.Fatal(err)
	}
	fixtureStatus := old.Status.DeepCopy()
	old.Status = alpha.TSZGuardrailPolicyStatus{}
	if err := c.Create(ctx, old); err != nil {
		t.Fatal(err)
	}
	old.Status = *fixtureStatus
	if err := c.Status().Update(ctx, old); err != nil {
		t.Fatal(err)
	}
	key := client.ObjectKeyFromObject(old)
	if err := c.Get(ctx, key, old); err != nil {
		t.Fatal(err)
	}
	original := old.DeepCopy()
	crdKey := client.ObjectKey{Name: "tszguardrailpolicies.security.thyris.ai"}
	storedCRD := &apiextensionsv1.CustomResourceDefinition{}
	if err := c.Get(ctx, crdKey, storedCRD); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(storedCRD.Status.StoredVersions, []string{"v1alpha1"}) {
		t.Fatalf("initial storage = %v", storedCRD.Status.StoredVersions)
	}
	crdUID := storedCRD.UID
	upgraded := readCRD("config/crd/bases/security.thyris.ai_tszguardrailpolicies.yaml")
	if _, err := envtest.InstallCRDs(config, envtest.CRDInstallOptions{CRDs: []*apiextensionsv1.CustomResourceDefinition{upgraded}}); err != nil {
		t.Fatalf("upgrade CRD: %v", err)
	}
	c = newClient() // Refresh discovery after adding a served API version.
	if err := c.Get(ctx, crdKey, storedCRD); err != nil {
		t.Fatal(err)
	}
	if storedCRD.UID != crdUID || !slices.Contains(storedCRD.Status.StoredVersions, "v1alpha1") || !slices.Contains(storedCRD.Status.StoredVersions, "v1beta1") {
		t.Fatalf("upgrade replaced CRD or lost storage history: %+v", storedCRD.Status)
	}
	graduated := &beta.TSZGuardrailPolicy{}
	if err := c.Get(ctx, key, graduated); err != nil {
		t.Fatal(err)
	}
	// The only additive default introduced after graduation is adapter.
	original.Spec.Adapter = "envoy-gateway"
	assertPolicyJSONEqual(t, original.Spec, graduated.Spec)
	assertPolicyJSONEqual(t, original.Status, graduated.Status)
	if graduated.UID != original.UID || !reflect.DeepEqual(graduated.Finalizers, original.Finalizers) || !reflect.DeepEqual(graduated.Annotations, original.Annotations) || !reflect.DeepEqual(graduated.Labels, original.Labels) {
		t.Fatal("upgrade lost object identity or metadata")
	}

	t.Run("beta updates remain readable and writable by alpha clients", func(t *testing.T) {
		graduated.Spec.ProcessingTimeout = &metav1.Duration{Duration: 4 * time.Second}
		if err := c.Update(ctx, graduated); err != nil {
			t.Fatal(err)
		}
		graduated.Status.PolicyVersion = new(int(13))
		if err := c.Status().Update(ctx, graduated); err != nil {
			t.Fatal(err)
		}
		if err := c.Get(ctx, key, old); err != nil {
			t.Fatal(err)
		}
		assertPolicyJSONEqual(t, graduated.Spec, old.Spec)
		assertPolicyJSONEqual(t, graduated.Status, old.Status)
		old.Spec.ProcessingTimeout = &metav1.Duration{Duration: 5 * time.Second}
		if err := c.Update(ctx, old); err != nil {
			t.Fatal(err)
		}
		if err := c.Get(ctx, key, graduated); err != nil {
			t.Fatal(err)
		}
		assertPolicyJSONEqual(t, old.Spec, graduated.Spec)
		assertPolicyJSONEqual(t, old.Status, graduated.Status)
	})

	t.Run("both served versions preserve defaults and validation", func(t *testing.T) {
		for _, version := range []string{"v1alpha1", "v1beta1"} {
			t.Run(version, func(t *testing.T) {
				object := &unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "security.thyris.ai/" + version, "kind": "TSZGuardrailPolicy",
					"metadata": map[string]interface{}{"name": "defaults-" + version, "namespace": "default"},
					"spec": map[string]interface{}{
						"policySource": "PostgresRef", "policyRef": map[string]interface{}{"name": "missing", "version": int64(1)},
						"targetRefs": []interface{}{map[string]interface{}{"group": "gateway.networking.k8s.io", "kind": "HTTPRoute", "name": "orders"}},
					},
				}}
				if err := c.Create(ctx, object); err != nil {
					t.Fatal(err)
				}
				for _, served := range []string{"v1alpha1", "v1beta1"} {
					read := &unstructured.Unstructured{}
					read.SetAPIVersion("security.thyris.ai/" + served)
					read.SetKind("TSZGuardrailPolicy")
					if err := c.Get(ctx, client.ObjectKeyFromObject(object), read); err != nil {
						t.Fatal(err)
					}
					timeout, _, _ := unstructured.NestedString(read.Object, "spec", "processingTimeout")
					failure, _, _ := unstructured.NestedString(read.Object, "spec", "failurePolicy", "request")
					responseFailure, _, _ := unstructured.NestedString(read.Object, "spec", "failurePolicy", "response")
					adapter, _, _ := unstructured.NestedString(read.Object, "spec", "adapter")
					if adapter != "envoy-gateway" || timeout != "2s" || failure != "FailClosed" || responseFailure != "FailClosed" {
						t.Fatalf("defaults = %+v", read.Object["spec"])
					}
				}
				bad := object.DeepCopy()
				bad.SetResourceVersion("")
				bad.SetUID("")
				bad.SetName("invalid-" + version)
				if err := unstructured.SetNestedField(bad.Object, "InvalidMode", "spec", "failurePolicy", "request"); err != nil {
					t.Fatal(err)
				}
				if err := c.Create(ctx, bad); !apierrors.IsInvalid(err) {
					t.Fatalf("invalid policy accepted: %v", err)
				}

				// Selection is stable for the resource lifetime, including across
				// API versions, so adapter-owned children cannot be orphaned.
				changed := object.DeepCopy()
				if err := unstructured.SetNestedField(changed.Object, "test-gateway", "spec", "adapter"); err != nil {
					t.Fatal(err)
				}
				if err := c.Update(ctx, changed); !apierrors.IsInvalid(err) {
					t.Fatalf("adapter change accepted: %v", err)
				}
				selectable := changed.DeepCopy()
				selectable.SetName("selectable-" + version)
				selectable.SetResourceVersion("")
				selectable.SetUID("")
				if err := c.Create(ctx, selectable); err != nil {
					t.Fatalf("additional adapter rejected by schema: %v", err)
				}

				// The beta controller reads a resource created using either endpoint
				// and publishes status visible through the original client version.
				reconciler := NewPolicyAttachmentReconciler(c, staticTargets{}, selector{}, missingReferenceResolver(), nil, testRegistry(t, &recordingEnvoy{}))
				if _, err := reconciler.Reconcile(ctx, request(object)); err == nil {
					t.Fatal("missing reference unexpectedly resolved")
				}
				if err := c.Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
					t.Fatal(err)
				}
				conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
				found := false
				for _, item := range conditions {
					condition := item.(map[string]interface{})
					if condition["type"] == "ResolvedRefs" && condition["status"] == "False" && condition["reason"] == "PolicyNotFound" {
						found = true
					}
				}
				if !found {
					t.Fatalf("controller did not update %s object: %v", version, conditions)
				}
			})
		}
	})

	t.Run("rewrite stored objects and retain alpha rollback access", func(t *testing.T) {
		// This is the same optimistic-concurrency rewrite documented for kubectl.
		list := &beta.TSZGuardrailPolicyList{}
		if err := c.List(ctx, list); err != nil {
			t.Fatal(err)
		}
		for i := range list.Items {
			if err := c.Update(ctx, &list.Items[i]); err != nil {
				t.Fatal(err)
			}
		}
		if err := c.Get(ctx, crdKey, storedCRD); err != nil {
			t.Fatal(err)
		}
		if err := c.Status().Patch(ctx, storedCRD, client.RawPatch(types.MergePatchType, []byte(`{"status":{"storedVersions":["v1beta1"]}}`))); err != nil {
			t.Fatal(err)
		}
		if err := c.Get(ctx, key, old); err != nil {
			t.Fatal(err)
		}
		assertPolicyJSONEqual(t, graduated.Spec, old.Spec)
		assertPolicyJSONEqual(t, graduated.Status, old.Status)
		if old.UID != original.UID {
			t.Fatal("migration recreated policy")
		}
		// Removing alpha from storedVersions must not remove its served endpoint.
		old.Annotations["test.thyris.ai/rollback-client"] = "alpha"
		if err := c.Update(ctx, old); err != nil {
			t.Fatal(err)
		}
		if err := c.Get(ctx, key, graduated); err != nil {
			t.Fatal(err)
		}
		if graduated.Annotations["test.thyris.ai/rollback-client"] != "alpha" {
			t.Fatal("alpha rollback write lost")
		}
		if err := c.Get(ctx, crdKey, storedCRD); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(storedCRD.Status.StoredVersions, []string{"v1beta1"}) {
			t.Fatalf("storedVersions = %v", storedCRD.Status.StoredVersions)
		}
	})
}

func assertPolicyJSONEqual(t *testing.T, want, got interface{}) {
	t.Helper()
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(wantJSON) != string(gotJSON) {
		t.Fatalf("policy fields changed\nwant: %s\n got: %s", wantJSON, gotJSON)
	}
}
