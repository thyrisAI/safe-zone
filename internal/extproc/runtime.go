package extproc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"gorm.io/gorm"
	"thyris-sz/internal/config"
	"thyris-sz/internal/extproc/policy"
)

// GRPCRegistrar binds a transport-specific service to the runtime's generic
// gRPC server. Protocol generated types stay in the transport adapter.
type GRPCRegistrar interface {
	Register(*grpc.Server)
}

type Dependencies struct {
	DB             *gorm.DB
	Redis          *redis.Client
	PolicyCache    PolicyCache
	Registrar      GRPCRegistrar
	MetricsHandler http.Handler
}

// Runtime owns only the ext-proc process lifecycle and transports.
type Runtime struct {
	config       *config.ExtProcConfig
	dependencies Dependencies
	httpServer   *http.Server
	grpcServer   *grpc.Server
	grpcHealth   *health.Server
	errors       chan error
	started      atomic.Bool
}

func NewRuntime(cfg *config.ExtProcConfig, dependencies Dependencies) (*Runtime, error) {
	if cfg == nil {
		return nil, errors.New("ext-proc config is required")
	}
	if dependencies.DB == nil {
		return nil, errors.New("PostgreSQL dependency is required")
	}
	if dependencies.Redis == nil {
		return nil, errors.New("Redis dependency is required")
	}
	if dependencies.PolicyCache == nil {
		return nil, errors.New("policy cache dependency is required")
	}
	if dependencies.Registrar == nil {
		return nil, errors.New("gRPC transport registrar is required")
	}
	if cfg.MaxConcurrentStreams == 0 {
		return nil, errors.New("max concurrent streams must be greater than zero")
	}
	if cfg.MaxGRPCMessageBytes <= 0 {
		return nil, errors.New("max gRPC message bytes must be greater than zero")
	}
	grpcOptions := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(cfg.MaxGRPCMessageBytes),
		grpc.MaxSendMsgSize(cfg.MaxGRPCMessageBytes),
	}
	if cfg.GRPCTLSCertFile != "" {
		tlsConfig, err := loadGRPCMTLSConfig(cfg)
		if err != nil {
			return nil, err
		}
		grpcOptions = append(grpcOptions, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}
	grpcServer := grpc.NewServer(grpcOptions...)
	grpcHealth := health.NewServer()
	healthv1.RegisterHealthServer(grpcServer, grpcHealth)
	dependencies.Registrar.Register(grpcServer)

	runtime := &Runtime{
		config:       cfg,
		dependencies: dependencies,
		grpcServer:   grpcServer,
		grpcHealth:   grpcHealth,
		errors:       make(chan error, 2),
	}
	runtime.httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           runtime.healthHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return runtime, nil
}

func loadGRPCMTLSConfig(cfg *config.ExtProcConfig) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(cfg.GRPCTLSCertFile, cfg.GRPCTLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load gRPC server certificate: %w", err)
	}
	clientCABytes, err := os.ReadFile(cfg.GRPCTLSClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("read gRPC client CA certificate: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(clientCABytes) {
		return nil, fmt.Errorf("parse gRPC client CA certificate %q: no certificates found", cfg.GRPCTLSClientCAFile)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
	}, nil
}

func (r *Runtime) Start() error {
	if r.started.Load() {
		return errors.New("ext-proc runtime has already been started")
	}
	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", r.config.GRPCPort))
	if err != nil {
		return fmt.Errorf("listen on gRPC port %d: %w", r.config.GRPCPort, err)
	}
	httpListener, err := net.Listen("tcp", r.httpServer.Addr)
	if err != nil {
		_ = grpcListener.Close()
		return fmt.Errorf("listen on HTTP port %d: %w", r.config.HTTPPort, err)
	}

	r.started.Store(true)
	r.grpcHealth.SetServingStatus("", healthv1.HealthCheckResponse_SERVING)
	go func() {
		if err := r.grpcServer.Serve(grpcListener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			r.errors <- fmt.Errorf("gRPC server: %w", err)
		}
	}()
	go func() {
		if err := r.httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.errors <- fmt.Errorf("HTTP health server: %w", err)
		}
	}()
	return nil
}

func (r *Runtime) Errors() <-chan error {
	return r.errors
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if !r.started.Swap(false) {
		return nil
	}
	r.grpcHealth.SetServingStatus("", healthv1.HealthCheckResponse_NOT_SERVING)

	grpcStopped := make(chan struct{})
	go func() {
		r.grpcServer.GracefulStop()
		close(grpcStopped)
	}()

	httpErr := r.httpServer.Shutdown(ctx)
	select {
	case <-grpcStopped:
		return httpErr
	case <-ctx.Done():
		r.grpcServer.Stop()
		<-grpcStopped
		return errors.Join(httpErr, fmt.Errorf("graceful shutdown: %w", ctx.Err()))
	}
}

func (r *Runtime) healthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if !r.started.Load() {
			http.Error(w, "Server not running", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("UP"))
	})
	if r.dependencies.MetricsHandler != nil {
		mux.Handle("GET /metrics", r.dependencies.MetricsHandler)
	}
	readiness := func(w http.ResponseWriter, _ *http.Request) {
		if !r.dependencies.PolicyCache.Ready() {
			http.Error(w, "Policy cache not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("READY"))
	}
	mux.HandleFunc("GET /readyz", readiness)
	// Keep the pre-existing path as a compatibility alias.
	mux.HandleFunc("GET /ready", readiness)
	mux.HandleFunc("GET /debug/policy-versions", func(w http.ResponseWriter, _ *http.Request) {
		cache, ok := r.dependencies.PolicyCache.(interface {
			Snapshots() []policy.CompiledSnapshot
		})
		if !ok {
			http.Error(w, "Policy version diagnostics unavailable", http.StatusNotImplemented)
			return
		}
		type policyVersion struct {
			PolicyID string `json:"policy_id"`
			Version  int    `json:"version"`
		}
		response := struct {
			Ready    bool            `json:"ready"`
			Policies []policyVersion `json:"policies"`
		}{Ready: r.dependencies.PolicyCache.Ready(), Policies: make([]policyVersion, 0)}
		for _, snapshot := range cache.Snapshots() {
			response.Policies = append(response.Policies, policyVersion{PolicyID: snapshot.PolicyID, Version: snapshot.Version})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "encode policy version diagnostics", http.StatusInternalServerError)
		}
	})
	return mux
}
