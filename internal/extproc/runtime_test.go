package extproc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"thyris-sz/internal/config"
	"thyris-sz/internal/extproc/policy"
)

type stubPolicyCache struct {
	ready bool
}

func (cache stubPolicyCache) Ready() bool { return cache.ready }
func (cache stubPolicyCache) Get(string) (policy.CompiledSnapshot, bool) {
	return policy.CompiledSnapshot{}, false
}

type mutablePolicyCache struct {
	ready atomic.Bool
}

type largeResponseProcessor struct {
	size int
}

type gatedProcessor struct {
	started chan struct{}
	release chan struct{}
}

type cancellationProcessor struct {
	started chan struct{}
	exited  chan struct{}
}

func newCancellationProcessor(capacity int) *cancellationProcessor {
	return &cancellationProcessor{started: make(chan struct{}, capacity), exited: make(chan struct{}, capacity)}
}

func (processor *cancellationProcessor) Process(ctx context.Context, _ ProcessingRequest) (ProcessingResult, error) {
	select {
	case processor.started <- struct{}{}:
	case <-ctx.Done():
		return ProcessingResult{}, ctx.Err()
	}
	<-ctx.Done()
	processor.exited <- struct{}{}
	return ProcessingResult{}, ctx.Err()
}

// testRegistrar exercises generic runtime lifecycle behavior without making
// the runtime depend on Envoy protocol types in production code.
type testRegistrar struct{ processor Processor }

type testExternalProcessorServer struct {
	extprocv3.UnimplementedExternalProcessorServer
	processor Processor
}

func (registrar testRegistrar) Register(server *grpc.Server) {
	extprocv3.RegisterExternalProcessorServer(server, testExternalProcessorServer{processor: registrar.processor})
}

func (server testExternalProcessorServer) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		result, err := server.processor.Process(stream.Context(), ProcessingRequest{Body: message.GetRequestBody().GetBody()})
		if err != nil {
			return err
		}
		response := &extprocv3.ProcessingResponse{}
		if result.Body != nil {
			response.Response = &extprocv3.ProcessingResponse_RequestBody{RequestBody: &extprocv3.BodyResponse{Response: &extprocv3.CommonResponse{BodyMutation: &extprocv3.BodyMutation{Mutation: &extprocv3.BodyMutation_Body{Body: result.Body}}}}}
		}
		if err := stream.Send(response); err != nil {
			return err
		}
	}
}

func requestHeadersMessage(_, _, _ string) *extprocv3.ProcessingRequest {
	return &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestHeaders{RequestHeaders: &extprocv3.HttpHeaders{}}}
}

func awaitSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}

func (processor largeResponseProcessor) Process(context.Context, ProcessingRequest) (ProcessingResult, error) {
	return ProcessingResult{Action: ActionAllow, Body: make([]byte, processor.size)}, nil
}

func (processor gatedProcessor) Process(ctx context.Context, _ ProcessingRequest) (ProcessingResult, error) {
	select {
	case processor.started <- struct{}{}:
	case <-ctx.Done():
		return ProcessingResult{}, ctx.Err()
	}
	select {
	case <-processor.release:
		return ProcessingResult{Action: ActionAllow}, nil
	case <-ctx.Done():
		return ProcessingResult{}, ctx.Err()
	}
}

func (cache *mutablePolicyCache) Ready() bool { return cache.ready.Load() }
func (cache *mutablePolicyCache) Get(string) (policy.CompiledSnapshot, bool) {
	return policy.CompiledSnapshot{}, false
}

func TestRuntimeStartsHealthServersAndShutsDown(t *testing.T) {
	httpPort := availablePort(t)
	grpcPort := availablePort(t)
	for grpcPort == httpPort {
		grpcPort = availablePort(t)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer redisClient.Close()

	runtime, err := NewRuntime(&config.ExtProcConfig{
		HTTPPort:             httpPort,
		GRPCPort:             grpcPort,
		MaxConcurrentStreams: 2,
		MaxGRPCMessageBytes:  4 * 1024 * 1024,
	}, Dependencies{
		DB:          &gorm.DB{},
		Redis:       redisClient,
		PolicyCache: stubPolicyCache{ready: true},
		Registrar:   testRegistrar{processor: NewAllowProcessor()},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if err := runtime.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	response, err := http.Get("http://127.0.0.1:" + portString(httpPort) + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz error = %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatalf("read /healthz response: %v", readErr)
	}
	if response.StatusCode != http.StatusOK || string(body) != "UP" {
		t.Fatalf("GET /healthz = %d %q, want 200 UP", response.StatusCode, body)
	}
	readyResponse, err := http.Get("http://127.0.0.1:" + portString(httpPort) + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz error = %v", err)
	}
	readyBody, readErr := io.ReadAll(readyResponse.Body)
	_ = readyResponse.Body.Close()
	if readErr != nil {
		t.Fatalf("read /readyz response: %v", readErr)
	}
	if readyResponse.StatusCode != http.StatusOK || string(readyBody) != "READY" {
		t.Fatalf("GET /readyz = %d %q, want 200 READY", readyResponse.StatusCode, readyBody)
	}

	connection, err := grpc.NewClient(
		"127.0.0.1:"+portString(grpcPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("connect to gRPC health server: %v", err)
	}
	defer connection.Close()
	healthContext, cancelHealth := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelHealth()
	healthResponse, err := grpc_health_v1.NewHealthClient(connection).Check(
		healthContext,
		&grpc_health_v1.HealthCheckRequest{},
	)
	if err != nil {
		t.Fatalf("gRPC health check error = %v", err)
	}
	if healthResponse.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("gRPC health status = %s, want SERVING", healthResponse.Status)
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelShutdown()
	if err := runtime.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestRuntimeRequiresVerifiedGRPCClientCertificateWhenMTLSEnabled(t *testing.T) {
	serverCert, serverKey, clientCA, clientCert, clientKey := writeMTLSTestCertificates(t)
	httpPort, grpcPort := availablePort(t), availablePort(t)
	for grpcPort == httpPort {
		grpcPort = availablePort(t)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer redisClient.Close()
	runtime, err := NewRuntime(&config.ExtProcConfig{
		HTTPPort: httpPort, GRPCPort: grpcPort, MaxConcurrentStreams: 2, MaxGRPCMessageBytes: 4 * 1024 * 1024,
		GRPCTLSCertFile: serverCert, GRPCTLSKeyFile: serverKey, GRPCTLSClientCAFile: clientCA,
	}, Dependencies{DB: &gorm.DB{}, Redis: redisClient, PolicyCache: stubPolicyCache{ready: true}, Registrar: testRegistrar{processor: NewAllowProcessor()}})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if err := runtime.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = runtime.Shutdown(ctx)
	})

	rootPEM, err := os.ReadFile(clientCA)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(rootPEM) {
		t.Fatal("append test CA")
	}
	address := "127.0.0.1:" + portString(grpcPort)
	noClient, err := grpc.NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{RootCAs: pool, ServerName: "localhost"})))
	if err != nil {
		t.Fatalf("connect without client certificate: %v", err)
	}
	defer noClient.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = grpc_health_v1.NewHealthClient(noClient).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err == nil {
		t.Fatal("health check without client certificate succeeded")
	}

	clientCertificate, err := tls.LoadX509KeyPair(clientCert, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	withClient, err := grpc.NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{RootCAs: pool, ServerName: "localhost", Certificates: []tls.Certificate{clientCertificate}})))
	if err != nil {
		t.Fatalf("connect with client certificate: %v", err)
	}
	defer withClient.Close()
	health, err := grpc_health_v1.NewHealthClient(withClient).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health check with client certificate: %v", err)
	}
	if health.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("health status = %s, want SERVING", health.Status)
	}
}

func writeMTLSTestCertificates(t *testing.T) (serverCertFile, serverKeyFile, caFile, clientCertFile, clientKeyFile string) {
	t.Helper()
	dir := t.TempDir()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial := big.NewInt(1)
	caTemplate := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "test-ca"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	writePEM := func(name, kind string, bytes []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: bytes}), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	caFile = writePEM("ca.crt", "CERTIFICATE", caDER)
	issue := func(name string, usages []x509.ExtKeyUsage, dnsNames []string) (string, string) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		serial.Add(serial, big.NewInt(1))
		template := &x509.Certificate{SerialNumber: new(big.Int).Set(serial), Subject: pkix.Name{CommonName: name}, DNSNames: dnsNames, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: usages}
		der, err := x509.CreateCertificate(rand.Reader, template, caTemplate, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		return writePEM(name+".crt", "CERTIFICATE", der), writePEM(name+".key", "PRIVATE KEY", keyDER)
	}
	serverCertFile, serverKeyFile = issue("server", []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, []string{"localhost"})
	clientCertFile, clientKeyFile = issue("client", []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, nil)
	return
}

func TestRuntimeReadinessRequiresInitialPolicyCacheLoad(t *testing.T) {
	runtime := &Runtime{dependencies: Dependencies{PolicyCache: stubPolicyCache{ready: false}}}
	runtime.started.Store(true)

	readyRecorder := httptest.NewRecorder()
	runtime.healthHandler().ServeHTTP(readyRecorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if readyRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /readyz status = %d, want 503 before cache load", readyRecorder.Code)
	}

	healthRecorder := httptest.NewRecorder()
	runtime.healthHandler().ServeHTTP(healthRecorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200 while cache is not ready", healthRecorder.Code)
	}
}

func TestLiveRuntimeHealthAndReadinessTransition(t *testing.T) {
	httpPort := availablePort(t)
	grpcPort := availablePort(t)
	for grpcPort == httpPort {
		grpcPort = availablePort(t)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer redisClient.Close()
	policyCache := &mutablePolicyCache{}
	runtime, err := NewRuntime(&config.ExtProcConfig{
		HTTPPort: httpPort, GRPCPort: grpcPort,
		MaxConcurrentStreams: 2, MaxGRPCMessageBytes: 4 * 1024 * 1024,
	}, Dependencies{
		DB: &gorm.DB{}, Redis: redisClient, PolicyCache: policyCache,
		Registrar: testRegistrar{processor: NewAllowProcessor()},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if err := runtime.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := runtime.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
	}()

	baseURL := "http://127.0.0.1:" + portString(httpPort)
	assertHTTPStatus(t, baseURL+"/healthz", http.StatusOK)
	assertHTTPStatus(t, baseURL+"/readyz", http.StatusServiceUnavailable)
	policyCache.ready.Store(true)
	assertHTTPStatus(t, baseURL+"/readyz", http.StatusOK)
}

func TestRuntimeEnforcesGRPCReceiveAndSendMessageLimits(t *testing.T) {
	tests := []struct {
		name       string
		processor  Processor
		message    *extprocv3.ProcessingRequest
		limitBytes int
	}{
		{
			name:       "receive",
			processor:  NewAllowProcessor(),
			message:    &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestBody{RequestBody: &extprocv3.HttpBody{Body: make([]byte, 2048)}}},
			limitBytes: 512,
		},
		{
			name:       "send",
			processor:  largeResponseProcessor{size: 2048},
			message:    requestHeadersMessage("rid-large", "envoy-large", "default"),
			limitBytes: 512,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, client, closeClient := startRuntimeClient(t, test.limitBytes, test.processor)
			defer closeClient()
			defer shutdownRuntime(t, runtime)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			stream, err := client.Process(ctx)
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			if err := stream.Send(test.message); err != nil {
				if status.Code(err) != codes.ResourceExhausted {
					t.Fatalf("Send() error = %v (%s), want RESOURCE_EXHAUSTED", err, status.Code(err))
				}
				return
			}
			if _, err := stream.Recv(); status.Code(err) != codes.ResourceExhausted {
				t.Fatalf("Recv() error = %v (%s), want RESOURCE_EXHAUSTED", err, status.Code(err))
			}
		})
	}
}

func TestRuntimeShutdownForcesStopAtDeadlineAndCancelsStream(t *testing.T) {
	processor := newCancellationProcessor(1)
	runtime, client, closeClient := startRuntimeClient(t, 4*1024*1024, processor)
	defer closeClient()

	stream, err := client.Process(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := stream.Send(requestHeadersMessage("rid-shutdown", "envoy-shutdown", "default")); err != nil {
		t.Fatalf("send stream headers: %v", err)
	}
	awaitSignal(t, processor.started, "processor start before shutdown")

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelShutdown()
	err = runtime.Shutdown(shutdownContext)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown() error = %v, want deadline exceeded after forced Stop", err)
	}
	awaitSignal(t, processor.exited, "processor cancellation after forced Stop")
	if _, err := stream.Recv(); status.Code(err) != codes.Unavailable && status.Code(err) != codes.Canceled {
		t.Fatalf("stream error after forced Stop = %v (%s), want UNAVAILABLE or CANCELED", err, status.Code(err))
	}
}

func TestRuntimeGracefulShutdownWaitsForExistingStream(t *testing.T) {
	processor := gatedProcessor{started: make(chan struct{}, 1), release: make(chan struct{})}
	runtime, client, closeClient := startRuntimeClient(t, 4*1024*1024, processor)
	defer closeClient()

	stream, err := client.Process(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := stream.Send(requestHeadersMessage("rid-graceful", "envoy-graceful", "default")); err != nil {
		t.Fatalf("send stream headers: %v", err)
	}
	awaitSignal(t, processor.started, "processor start before graceful shutdown")

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelShutdown()
	shutdownResult := make(chan error, 1)
	go func() { shutdownResult <- runtime.Shutdown(shutdownContext) }()
	select {
	case err := <-shutdownResult:
		t.Fatalf("Shutdown() returned before active stream completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	newStreamContext, cancelNewStream := context.WithTimeout(context.Background(), time.Second)
	defer cancelNewStream()
	newStream, err := client.Process(newStreamContext)
	if err == nil {
		err = newStream.Send(requestHeadersMessage("rid-rejected", "envoy-rejected", "default"))
	}
	if err == nil {
		_, err = newStream.Recv()
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("new stream during graceful shutdown error = %v (%s), want UNAVAILABLE", err, status.Code(err))
	}

	close(processor.release)
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("receive active stream response during graceful shutdown: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("close active stream: %v", err)
	}
	select {
	case err := <-shutdownResult:
		if err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("graceful shutdown did not finish after active stream completed")
	}
}

func startRuntimeClient(t *testing.T, maxMessageBytes int, processor Processor) (*Runtime, extprocv3.ExternalProcessorClient, func()) {
	t.Helper()
	httpPort := availablePort(t)
	grpcPort := availablePort(t)
	for grpcPort == httpPort {
		grpcPort = availablePort(t)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	runtime, err := NewRuntime(&config.ExtProcConfig{
		HTTPPort: httpPort, GRPCPort: grpcPort,
		MaxConcurrentStreams: 1, MaxGRPCMessageBytes: maxMessageBytes,
		FailMode: config.ExtProcFailOpen,
	}, Dependencies{
		DB: &gorm.DB{}, Redis: redisClient,
		PolicyCache: stubPolicyCache{ready: true},
		Registrar:   testRegistrar{processor: processor},
	})
	if err != nil {
		_ = redisClient.Close()
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if err := runtime.Start(); err != nil {
		_ = redisClient.Close()
		t.Fatalf("Start() error = %v", err)
	}
	connection, err := grpc.NewClient(
		"127.0.0.1:"+portString(grpcPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		shutdownRuntime(t, runtime)
		_ = redisClient.Close()
		t.Fatalf("connect to ext-proc server: %v", err)
	}
	return runtime, extprocv3.NewExternalProcessorClient(connection), func() {
		_ = connection.Close()
		_ = redisClient.Close()
	}
}

func shutdownRuntime(t *testing.T, runtime *Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

func assertHTTPStatus(t *testing.T, url string, wanted int) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	_ = response.Body.Close()
	if response.StatusCode != wanted {
		t.Fatalf("GET %s status = %d, want %d", url, response.StatusCode, wanted)
	}
}

func availablePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func portString(port int) string {
	return fmt.Sprintf("%d", port)
}
