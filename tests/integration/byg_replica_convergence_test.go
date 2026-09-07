package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"thyris-sz/internal/extproc/policy"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestBYGReplicaConvergenceKind is a real Kind E2E: no cache or database is
// mocked. It observes pod convergence only through the safe version endpoint,
// except for the stream-pinning assertion, which uses ext-proc metadata.
func TestBYGReplicaConvergenceKind(t *testing.T) {
	if os.Getenv("TSZ_BYG_KIND_E2E") != "1" {
		t.Skip("set TSZ_BYG_KIND_E2E=1 to run against the pinned Kind cluster")
	}
	runRepo(t, "./deployments/envoy-gateway/kind-bootstrap.sh", "verify-replica-lifecycle")
	kubectl := filepath.Join(os.TempDir(), "tsz-byg-tools", "kubectl-v1.35.5")
	kubeconfig := filepath.Join(os.TempDir(), "tsz-byg-tools", "tsz-byg.kubeconfig")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pods := extProcPods(t, kubectl, kubeconfig)
	if len(pods) != 2 {
		t.Fatalf("ext-proc pods = %v, want exactly two", pods)
	}

	dbPort := forward(t, kubectl, kubeconfig, "service/postgres", 5432)
	redisPort := forward(t, kubectl, kubeconfig, "service/redis", 6379)
	db, err := sql.Open("pgx", fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/thyris?sslmode=disable", dbPort))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := policy.NewPostgresRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("127.0.0.1:%d", redisPort), Password: "thyrisredis"})
	defer redisClient.Close()
	publisher, err := policy.NewRedisActivationPublisher(redisClient)
	if err != nil {
		t.Fatal(err)
	}
	compiler, _ := policy.NewCompiler(repository)
	activator, _ := policy.NewActivator(repository, publisher)
	policyName := fmt.Sprintf("kind-convergence-%d", time.Now().UnixNano())

	v1, policyID := compileActivate(t, ctx, repository, compiler, activator, policyName)
	waitVersions(t, kubectl, kubeconfig, pods, policyName, 1, 10*time.Second) // (1)

	grpcPort := forward(t, kubectl, kubeconfig, "pod/"+pods[0], 9002)
	connection, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", grpcPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	oldStream, err := extprocv3.NewExternalProcessorClient(connection).Process(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sendHeaders(t, oldStream, policyName, false)
	if got := metadataVersion(t, receive(t, oldStream)); got != 1 {
		t.Fatalf("open stream version = %d, want v1", got)
	}

	_, _ = compileActivate(t, ctx, repository, compiler, activator, policyName)
	_ = v1
	waitVersions(t, kubectl, kubeconfig, pods, policyName, 2, 10*time.Second) // (2)
	sendBody(t, oldStream)
	if got := metadataVersion(t, receive(t, oldStream)); got != 1 {
		t.Fatalf("open stream lost v1 pin: got v%d", got)
	} // (3)
	newStream, err := extprocv3.NewExternalProcessorClient(connection).Process(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sendHeaders(t, newStream, policyName, true)
	if got := metadataVersion(t, receive(t, newStream)); got != 2 {
		t.Fatalf("new stream version = %d, want v2", got)
	} // (3)

	if err := activator.Rollback(ctx, policyID); err != nil {
		t.Fatalf("rollback v2: %v", err)
	}
	waitVersions(t, kubectl, kubeconfig, pods, policyName, 1, 10*time.Second) // (4)

	// Kill only the first pod's Redis Pub/Sub client, then activate v2 again.
	killPodSubscriber(t, kubectl, kubeconfig, pods[0])
	// A rolled-back snapshot is immutable historical state, not activatable
	// input. A new compiled v3 is the next valid activation event.
	_, _ = compileActivate(t, ctx, repository, compiler, activator, policyName)
	waitVersions(t, kubectl, kubeconfig, pods, policyName, 3, 40*time.Second) // (5): periodic/reconnect reconciliation
}

func compileActivate(t *testing.T, ctx context.Context, r policy.Repository, c *policy.Compiler, a *policy.Activator, name string) (int64, int64) {
	t.Helper()
	definition := policy.PolicyDefinition{Request: policy.RequestPolicy{PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly}, Response: policy.ResponsePolicy{Enabled: true, PII: policy.ActionMask, Secret: policy.ActionBlock, UnsafeContent: policy.ActionAuditOnly}, FailurePolicy: policy.FailurePolicy{Request: policy.FailureModeClosed, Response: policy.FailureModeOpen}}
	id, err := r.CreateValidated(ctx, name, definition)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Compile(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := a.Activate(ctx, id); err != nil {
		t.Fatal(err)
	}
	snapshot, err := r.SnapshotByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return id, snapshot.PolicyID
}

func extProcPods(t *testing.T, kubectl, kubeconfig string) []string {
	t.Helper()
	output := run(t, kubectl, "--kubeconfig", kubeconfig, "-n", "tsz-byg-demo", "get", "pods", "-l", "app.kubernetes.io/name=tsz-ext-proc", "-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
	return strings.Fields(output)
}
func podVersions(t *testing.T, kubectl, kubeconfig, pod string) map[string]int {
	t.Helper()
	output := run(t, kubectl, "--kubeconfig", kubeconfig, "-n", "tsz-byg-demo", "exec", pod, "--", "wget", "-qO-", "http://127.0.0.1:8080/debug/policy-versions")
	var value struct {
		Ready    bool `json:"ready"`
		Policies []struct {
			PolicyID string `json:"policy_id"`
			Version  int    `json:"version"`
		} `json:"policies"`
	}
	if err := json.Unmarshal([]byte(output), &value); err != nil || !value.Ready {
		t.Fatalf("pod %s diagnostics=%q err=%v", pod, output, err)
	}
	versions := map[string]int{}
	for _, p := range value.Policies {
		versions[p.PolicyID] = p.Version
	}
	return versions
}
func waitVersions(t *testing.T, kubectl, kubeconfig string, pods []string, policyID string, version int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ok := true
		for _, pod := range pods {
			if podVersions(t, kubectl, kubeconfig, pod)[policyID] != version {
				ok = false
			}
		}
		if ok {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("pods %v did not converge on %s v%d", pods, policyID, version)
}
func forward(t *testing.T, kubectl, kubeconfig, target string, remote int) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	command := exec.Command(kubectl, "--kubeconfig", kubeconfig, "-n", "tsz-byg-demo", "port-forward", target, fmt.Sprintf("%d:%d", port, remote))
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _, _ = command.Process.Wait() })
	time.Sleep(time.Second)
	return port
}
func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	output, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func runRepo(t *testing.T, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = "../.."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}
func receive(t *testing.T, stream extprocv3.ExternalProcessor_ProcessClient) *extprocv3.ProcessingResponse {
	t.Helper()
	response, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	return response
}
func sendHeaders(t *testing.T, stream extprocv3.ExternalProcessor_ProcessClient, policyID string, eos bool) {
	t.Helper()
	err := stream.Send(&extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestHeaders{RequestHeaders: &extprocv3.HttpHeaders{Headers: &corev3.HeaderMap{Headers: []*corev3.HeaderValue{{Key: "x-request-id", RawValue: []byte("kind-convergence")}, {Key: "x-tsz-policy", RawValue: []byte(policyID)}}}, EndOfStream: eos}}})
	if err != nil {
		t.Fatal(err)
	}
}
func sendBody(t *testing.T, stream extprocv3.ExternalProcessor_ProcessClient) {
	t.Helper()
	err := stream.Send(&extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestBody{RequestBody: &extprocv3.HttpBody{Body: []byte(`{"messages":[{"role":"user","content":"safe"}]}`), EndOfStream: true}}})
	if err != nil {
		t.Fatal(err)
	}
}
func metadataVersion(t *testing.T, response *extprocv3.ProcessingResponse) int {
	t.Helper()
	fields := response.GetDynamicMetadata().GetFields()["io.thyris.tsz"].GetStructValue().GetFields()
	version := int(fields["policy_version"].GetNumberValue())
	if version == 0 {
		t.Fatalf("missing policy_version metadata: %+v", response.GetDynamicMetadata())
	}
	return version
}
func killPodSubscriber(t *testing.T, kubectl, kubeconfig, pod string) {
	t.Helper()
	ip := run(t, kubectl, "--kubeconfig", kubeconfig, "-n", "tsz-byg-demo", "get", "pod", pod, "-o", "jsonpath={.status.podIP}")
	clients := run(t, kubectl, "--kubeconfig", kubeconfig, "-n", "tsz-byg-demo", "exec", "deploy/redis", "--", "sh", "-ec", "redis-cli --no-auth-warning -a thyrisredis CLIENT LIST TYPE pubsub")
	for _, line := range strings.Split(clients, "\n") {
		if strings.Contains(line, "addr="+ip+":") {
			for _, field := range strings.Fields(line) {
				if strings.HasPrefix(field, "addr=") {
					run(t, kubectl, "--kubeconfig", kubeconfig, "-n", "tsz-byg-demo", "exec", "deploy/redis", "--", "redis-cli", "--no-auth-warning", "-a", "thyrisredis", "CLIENT", "KILL", "ADDR", strings.TrimPrefix(field, "addr="))
					return
				}
			}
		}
	}
	t.Fatalf("no Redis Pub/Sub client found for pod %s (%s)", pod, ip)
}

var _ = strconv.Itoa
