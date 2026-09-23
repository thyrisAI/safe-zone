package envoy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"thyris-sz/internal/extproc"
	"thyris-sz/internal/guardrails"
)

type liveAgentgatewayBackendRequest struct {
	path string
	body []byte
}

// TestLiveAgentgatewayExtProc runs the released agentgateway proxy, not a
// hand-built Envoy ExtProc client. CI sets TSZ_TEST_AGENTGATEWAY_BINARY.
func TestLiveAgentgatewayExtProc(t *testing.T) {
	binary := os.Getenv("TSZ_TEST_AGENTGATEWAY_BINARY")
	if binary == "" {
		t.Skip("set TSZ_TEST_AGENTGATEWAY_BINARY to run the live proxy test")
	}
	processor, err := extproc.NewOpenAIRequestProcessor(agentgatewayGenericJSONInspector(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		switch {
		case strings.Contains(input.Text, "blocked-secret"):
			return guardrails.InspectResult{Action: guardrails.RuleActionBlock, DetectionCount: 1, Categories: []string{"SECRET"}}, nil
		case strings.Contains(input.Text, "person@example.test"):
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: strings.ReplaceAll(input.Text, "person@example.test", "[MASKED]"), DetectionCount: 1, Categories: []string{"PII"}}, nil
		default:
			return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerWithSettings(processor, agentgatewayGenericJSONPolicyCache{}, nil, ServerSettings{AdapterName: "agentgateway"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	server.Register(grpcServer)
	go func() { _ = grpcServer.Serve(grpcListener) }()
	t.Cleanup(grpcServer.Stop)

	var backendCalls atomic.Int64
	backendRequests := make(chan liveAgentgatewayBackendRequest, 3)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		backendCalls.Add(1)
		backendRequests <- liveAgentgatewayBackendRequest{path: r.URL.Path, body: body}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/chat/completions" {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Reply to person@example.test"}}]}`)
		} else {
			_, _ = io.WriteString(w, `{"owner":"person@example.test","ok":true}`)
		}
	}))
	defer backend.Close()

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyPort := proxyListener.Addr().(*net.TCPAddr).Port
	_ = proxyListener.Close()
	config := fmt.Sprintf(`gateways:
  default:
    port: %d
routes:
- backends:
  - host: %s
  policies:
    extProc:
      host: %s
      failureMode: failClosed
      processingOptions:
        requestHeaderMode: send
        responseHeaderMode: send
        requestBodyMode: buffered
        responseBodyMode: buffered
        requestTrailerMode: skip
        responseTrailerMode: skip
        allowModeOverride: false
`, proxyPort, strings.TrimPrefix(backend.URL, "http://"), grpcListener.Addr().String())
	configPath := filepath.Join(t.TempDir(), "agentgateway.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "agentgateway.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-f", configPath)
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start agentgateway: %v", err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait(); _ = logFile.Close() })
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(12 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			logs, _ := os.ReadFile(logPath)
			t.Fatalf("agentgateway did not listen: %v\n%s", err, logs)
		}
		time.Sleep(100 * time.Millisecond)
	}

	request := func(path, body string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, proxyURL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		// The standalone wire fixture supplies the policy and content selector.
		// Kubernetes PreRouting header ownership is covered by the manifests.
		req.Header.Set("X-TSZ-Policy", agentgatewayGenericJSONPolicyID)
		if path == "/api/customer-profiles" {
			req.Header.Set("X-TSZ-Content-Adapter", extproc.GenericJSONContentAdapter)
		}
		response, err := client.Do(req)
		if err != nil {
			logs, _ := os.ReadFile(logPath)
			t.Fatalf("request through agentgateway: %v\n%s", err, logs)
		}
		return response
	}
	response := request("/api/customer-profiles", `{"email":"person@example.test","active":true}`)
	responseBody, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || bytes.Contains(responseBody, []byte("person@example.test")) || !bytes.Contains(responseBody, []byte("[MASKED]")) {
		logs, _ := os.ReadFile(logPath)
		t.Fatalf("response status=%d body=%s\nagentgateway logs:\n%s", response.StatusCode, responseBody, logs)
	}
	select {
	case received := <-backendRequests:
		if received.path != "/api/customer-profiles" || bytes.Contains(received.body, []byte("person@example.test")) || !bytes.Contains(received.body, []byte("[MASKED]")) || !bytes.Contains(received.body, []byte(`"active":true`)) {
			t.Fatalf("backend received unguarded request: path=%s body=%s", received.path, received.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backend did not receive the masked request")
	}
	response = request("/v1/chat/completions", `{"model":"test","messages":[{"role":"user","content":"Email person@example.test"}]}`)
	chatBody, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || bytes.Contains(chatBody, []byte("person@example.test")) || !bytes.Contains(chatBody, []byte("[MASKED]")) {
		t.Fatalf("chat response status=%d body=%s", response.StatusCode, chatBody)
	}
	select {
	case received := <-backendRequests:
		if received.path != "/v1/chat/completions" || bytes.Contains(received.body, []byte("person@example.test")) || !bytes.Contains(received.body, []byte("[MASKED]")) {
			t.Fatalf("backend received unguarded chat request: path=%s body=%s", received.path, received.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backend did not receive the masked chat request")
	}
	response = request("/api/customer-profiles", `{"token":"blocked-secret"}`)
	blockedBody, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || !bytes.Contains(blockedBody, []byte("TSZ_GUARDRAIL_BLOCKED")) || bytes.Contains(blockedBody, []byte("blocked-secret")) {
		t.Fatalf("block status=%d body=%s", response.StatusCode, blockedBody)
	}
	if calls := backendCalls.Load(); calls != 2 {
		t.Fatalf("blocked request reached backend: calls=%d", calls)
	}
}
