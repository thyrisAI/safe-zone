package policyattach

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	securityv1alpha1 "thyris-sz/api/v1alpha1"
	"thyris-sz/internal/controller"
	"thyris-sz/internal/controller/effectivepolicy"
	"thyris-sz/internal/controller/envoyresource"
	"thyris-sz/internal/extproc/policy"

	_ "github.com/jackc/pgx/v5/stdlib"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestReconcileWritesPolicyNotFoundInsteadOfProgrammingRoute(t *testing.T) {
	scheme, _ := controller.NewScheme()
	version := int32(1)
	object := &securityv1alpha1.TSZGuardrailPolicy{ObjectMeta: metav1.ObjectMeta{Name: "missing", Namespace: "apps"}, Spec: securityv1alpha1.TSZGuardrailPolicySpec{PolicySource: securityv1alpha1.PolicySourcePostgresRef, PolicyRef: &securityv1alpha1.PolicyReference{Name: "does-not-exist", Version: &version}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(object).WithObjects(object).Build()
	r := NewPolicyAttachmentReconciler(c, staticTargets{}, selector{}, &effectivepolicy.ReferenceResolver{Repo: missingRepository{}}, nil, &recordingEnvoy{})
	if _, err := r.Reconcile(context.Background(), request(object)); !errors.Is(err, policy.ErrNotFound) {
		t.Fatalf("Reconcile() error = %v", err)
	}
	got := &securityv1alpha1.TSZGuardrailPolicy{}
	_ = c.Get(context.Background(), client.ObjectKeyFromObject(object), got)
	condition := findCondition(got.Status.Conditions, securityv1alpha1.ConditionResolvedRefs)
	if condition.Status != metav1.ConditionFalse || condition.Reason != securityv1alpha1.ReasonPolicyNotFound {
		t.Fatalf("ResolvedRefs = %+v", condition)
	}
}

func TestReconcileUsesRouteOverGatewayCandidate(t *testing.T) {
	scheme, _ := controller.NewScheme()
	routeName := gatewayv1.ObjectName("orders")
	gatewayName := gatewayv1.ObjectName("edge")
	gatewayPolicy := inlinePolicy("gateway", target("Gateway", gatewayName, nil))
	routePolicy := inlinePolicy("route", target("HTTPRoute", routeName, nil))
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(gatewayPolicy, routePolicy).WithIndex(&securityv1alpha1.TSZGuardrailPolicy{}, targetRefIndex, targetRefIndexValues).WithObjects(gatewayPolicy, routePolicy).Build()
	targetObject := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "apps"}, Spec: gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: gatewayName}}}}}
	envoy := &recordingEnvoy{}
	r := NewPolicyAttachmentReconciler(c, staticTargets{targets: []ResolvedTarget{{Kind: "HTTPRoute", Ref: routePolicy.Spec.TargetRefs[0], Object: targetObject, SectionOK: true}}}, selector{}, nil, nil, envoy)
	if _, err := r.Reconcile(context.Background(), request(routePolicy)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if envoy.calls != 1 {
		t.Fatalf("Envoy calls = %d, want 1 for route winner", envoy.calls)
	}
}

func TestReconcileInlinePolicyIsIdempotentAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("TSZ_POLICY_TEST_DSN")
	if dsn == "" {
		t.Skip("TSZ_POLICY_TEST_DSN is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	schema := fmt.Sprintf("tsz_orchestration_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"migrations/000001_create_policy_snapshots.up.sql", "migrations/000002_create_route_policy_bindings.up.sql"} {
		sqlText, err := policy.MigrationFiles.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(sqlText)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	repo, err := policy.NewPostgresRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := policy.NewCompiler(repo)
	if err != nil {
		t.Fatal(err)
	}
	activator, err := policy.NewActivator(repo, activationPublisher{})
	if err != nil {
		t.Fatal(err)
	}
	scheme, _ := controller.NewScheme()
	routeName := gatewayv1.ObjectName("orders")
	object := compilingInlinePolicy("inline", target("HTTPRoute", routeName, nil))
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(object).WithIndex(&securityv1alpha1.TSZGuardrailPolicy{}, targetRefIndex, targetRefIndexValues).WithObjects(object).Build()
	r := NewPolicyAttachmentReconciler(c, staticTargets{targets: []ResolvedTarget{{Kind: "HTTPRoute", Ref: object.Spec.TargetRefs[0], Object: &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "apps"}}, SectionOK: true}}}, selector{}, nil, &effectivepolicy.Compiler{Repo: repo, Compiler: compiler, Activator: activator}, &recordingEnvoy{})
	if _, err := r.Reconcile(ctx, request(object)); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	var first int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM policy_snapshots").Scan(&first); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, request(object)); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	var second int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM policy_snapshots").Scan(&second); err != nil {
		t.Fatal(err)
	}
	if first != 1 || second != first {
		t.Fatalf("snapshot counts = %d then %d, want 1 then 1", first, second)
	}
}

type staticTargets struct{ targets []ResolvedTarget }

func (s staticTargets) ResolveTargets(context.Context, *securityv1alpha1.TSZGuardrailPolicy) []ResolvedTarget {
	return s.targets
}

type selector struct{}

func (selector) Select(c []effectivepolicy.Candidate) (effectivepolicy.Candidate, *effectivepolicy.ConflictError) {
	return effectivepolicy.SelectWinner(c)
}

type missingRepository struct{ policy.Repository }

func (missingRepository) PolicyByName(context.Context, string, *string) (policy.Policy, error) {
	return policy.Policy{}, policy.ErrNotFound
}

type recordingEnvoy struct{ calls int }

func (r *recordingEnvoy) ReconcileExtensionPolicy(context.Context, *securityv1alpha1.TSZGuardrailPolicy, gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName, envoyresource.EffectivePolicy) (controllerutil.OperationResult, error) {
	r.calls++
	return controllerutil.OperationResultCreated, nil
}
func request(object client.Object) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: object.GetNamespace(), Name: object.GetName()}}
}
func findCondition(conditions []metav1.Condition, kind string) metav1.Condition {
	for _, condition := range conditions {
		if condition.Type == kind {
			return condition
		}
	}
	return metav1.Condition{}
}
func target(kind string, name gatewayv1.ObjectName, section *gatewayv1.SectionName) gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName(gatewayv1.LocalPolicyTargetReferenceWithSectionName{LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{Group: gatewayv1.Group(gatewayAPIGroup), Kind: gatewayv1.Kind(kind), Name: name}, SectionName: section})
}
func inlinePolicy(name string, ref gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName) *securityv1alpha1.TSZGuardrailPolicy {
	return &securityv1alpha1.TSZGuardrailPolicy{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "apps"}, Spec: securityv1alpha1.TSZGuardrailPolicySpec{PolicySource: securityv1alpha1.PolicySourceInline, TargetRefs: []gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName{ref}}}
}
func compilingInlinePolicy(name string, ref gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName) *securityv1alpha1.TSZGuardrailPolicy {
	object := inlinePolicy(name, ref)
	object.Spec.Request = &securityv1alpha1.RequestPolicySpec{PII: securityv1alpha1.PolicyActionMask, Secret: securityv1alpha1.PolicyActionMask, PromptInjection: securityv1alpha1.PolicyActionBlock}
	object.Spec.Response = &securityv1alpha1.ResponsePolicySpec{}
	object.Spec.FailurePolicy = securityv1alpha1.FailurePolicySpec{Request: securityv1alpha1.FailureModeClosed, Response: securityv1alpha1.FailureModeClosed}
	return object
}

type activationPublisher struct{}

func (activationPublisher) PublishActivation(context.Context, policy.ActivationEvent) error {
	return nil
}
