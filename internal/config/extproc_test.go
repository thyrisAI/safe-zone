package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

var extProcEnvKeys = []string{
	"TSZ_HTTP_PORT",
	"TSZ_GRPC_PORT",
	"TSZ_FAIL_MODE",
	"TSZ_MAX_CONCURRENT_STREAMS",
	"TSZ_MAX_GRPC_MESSAGE_BYTES",
	"TSZ_MAX_BODY_BYTES",
	"TSZ_MAX_STREAM_BUFFER_BYTES",
	"TSZ_PROCESSING_TIMEOUT_MS",
	"TSZ_POLICY_RECONCILE_INTERVAL",
	"TSZ_POLICY_MAX_STALENESS",
	"TSZ_POLICY_RECONCILE_FAILURE_THRESHOLD",
	"TSZ_GRACEFUL_SHUTDOWN_TIMEOUT",
	"TSZ_GRPC_TLS_CERT_FILE",
	"TSZ_GRPC_TLS_KEY_FILE",
	"TSZ_GRPC_TLS_CLIENT_CA_FILE",
}

func TestLoadExtProcConfigDefaults(t *testing.T) {
	clearExtProcEnv(t)

	cfg, err := LoadExtProcConfig()
	if err != nil {
		t.Fatalf("LoadExtProcConfig() error = %v", err)
	}

	if cfg.HTTPPort != 8080 || cfg.GRPCPort != 9002 {
		t.Fatalf("unexpected ports: HTTP=%d gRPC=%d", cfg.HTTPPort, cfg.GRPCPort)
	}
	if cfg.FailMode != ExtProcFailClosed {
		t.Fatalf("FailMode = %q, want %q", cfg.FailMode, ExtProcFailClosed)
	}
	if cfg.MaxConcurrentStreams != 100 {
		t.Fatalf("MaxConcurrentStreams = %d, want 100", cfg.MaxConcurrentStreams)
	}
	if cfg.MaxGRPCMessageBytes != 4*1024*1024 {
		t.Fatalf("MaxGRPCMessageBytes = %d, want 4194304", cfg.MaxGRPCMessageBytes)
	}
	if cfg.MaxBodyBytes != 1024*1024 {
		t.Fatalf("MaxBodyBytes = %d, want 1048576", cfg.MaxBodyBytes)
	}
	if cfg.MaxStreamBufferBytes != 256*1024 {
		t.Fatalf("MaxStreamBufferBytes = %d, want 262144", cfg.MaxStreamBufferBytes)
	}
	if cfg.ProcessingTimeout != 2*time.Second {
		t.Fatalf("ProcessingTimeout = %s, want 2s", cfg.ProcessingTimeout)
	}
	if cfg.PolicyReconcileInterval != 30*time.Second {
		t.Fatalf("PolicyReconcileInterval = %s, want 30s", cfg.PolicyReconcileInterval)
	}
	if cfg.PolicyMaxStaleness != 5*time.Minute || cfg.PolicyReconcileFailureThreshold != 3 {
		t.Fatalf("unexpected policy readiness defaults: %+v", cfg)
	}
	if cfg.GracefulShutdownTimeout != 10*time.Second {
		t.Fatalf("GracefulShutdownTimeout = %s, want 10s", cfg.GracefulShutdownTimeout)
	}
	if cfg.GRPCTLSCertFile != "" || cfg.GRPCTLSKeyFile != "" || cfg.GRPCTLSClientCAFile != "" {
		t.Fatalf("TLS files should be disabled by default: %+v", cfg)
	}
}

func TestLoadExtProcConfigOverrides(t *testing.T) {
	clearExtProcEnv(t)
	t.Setenv("TSZ_HTTP_PORT", "18080")
	t.Setenv("TSZ_GRPC_PORT", "19002")
	t.Setenv("TSZ_FAIL_MODE", "OPEN")
	t.Setenv("TSZ_MAX_CONCURRENT_STREAMS", "25")
	t.Setenv("TSZ_MAX_GRPC_MESSAGE_BYTES", "8388608")
	t.Setenv("TSZ_MAX_BODY_BYTES", "2097152")
	t.Setenv("TSZ_MAX_STREAM_BUFFER_BYTES", "131072")
	t.Setenv("TSZ_PROCESSING_TIMEOUT_MS", "750")
	t.Setenv("TSZ_POLICY_RECONCILE_INTERVAL", "45s")
	t.Setenv("TSZ_POLICY_MAX_STALENESS", "8m")
	t.Setenv("TSZ_POLICY_RECONCILE_FAILURE_THRESHOLD", "7")
	t.Setenv("TSZ_GRACEFUL_SHUTDOWN_TIMEOUT", "15s")
	t.Setenv("TSZ_GRPC_TLS_CERT_FILE", "/tls/tls.crt")
	t.Setenv("TSZ_GRPC_TLS_KEY_FILE", "/tls/tls.key")
	t.Setenv("TSZ_GRPC_TLS_CLIENT_CA_FILE", "/tls/client-ca.crt")

	cfg, err := LoadExtProcConfig()
	if err != nil {
		t.Fatalf("LoadExtProcConfig() error = %v", err)
	}
	if cfg.HTTPPort != 18080 || cfg.GRPCPort != 19002 || cfg.FailMode != ExtProcFailOpen {
		t.Fatalf("unexpected basic config: %+v", cfg)
	}
	if cfg.MaxConcurrentStreams != 25 || cfg.MaxGRPCMessageBytes != 8388608 || cfg.MaxBodyBytes != 2097152 || cfg.MaxStreamBufferBytes != 131072 {
		t.Fatalf("unexpected limit config: %+v", cfg)
	}
	if cfg.ProcessingTimeout != 750*time.Millisecond || cfg.PolicyReconcileInterval != 45*time.Second || cfg.PolicyMaxStaleness != 8*time.Minute || cfg.PolicyReconcileFailureThreshold != 7 || cfg.GracefulShutdownTimeout != 15*time.Second {
		t.Fatalf("unexpected duration config: %+v", cfg)
	}
	if cfg.GRPCTLSCertFile != "/tls/tls.crt" || cfg.GRPCTLSKeyFile != "/tls/tls.key" || cfg.GRPCTLSClientCAFile != "/tls/client-ca.crt" {
		t.Fatalf("unexpected TLS config: %+v", cfg)
	}
}

func TestLoadExtProcConfigRequiresCompleteGRPCMTLSConfiguration(t *testing.T) {
	clearExtProcEnv(t)
	t.Setenv("TSZ_GRPC_TLS_CERT_FILE", "/tls/tls.crt")
	_, err := LoadExtProcConfig()
	if err == nil || !strings.Contains(err.Error(), "TSZ_GRPC_TLS_CERT_FILE") {
		t.Fatalf("LoadExtProcConfig() error = %v, want TLS configuration error", err)
	}
}

func TestLoadExtProcConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "zero HTTP port", key: "TSZ_HTTP_PORT", value: "0"},
		{name: "negative gRPC port", key: "TSZ_GRPC_PORT", value: "-1"},
		{name: "port above range", key: "TSZ_GRPC_PORT", value: "65536"},
		{name: "same ports", key: "TSZ_GRPC_PORT", value: "8080"},
		{name: "invalid fail mode", key: "TSZ_FAIL_MODE", value: "sometimes"},
		{name: "zero streams", key: "TSZ_MAX_CONCURRENT_STREAMS", value: "0"},
		{name: "negative gRPC message bytes", key: "TSZ_MAX_GRPC_MESSAGE_BYTES", value: "-4"},
		{name: "zero body bytes", key: "TSZ_MAX_BODY_BYTES", value: "0"},
		{name: "zero stream buffer bytes", key: "TSZ_MAX_STREAM_BUFFER_BYTES", value: "0"},
		{name: "invalid processing timeout", key: "TSZ_PROCESSING_TIMEOUT_MS", value: "soon"},
		{name: "negative processing timeout", key: "TSZ_PROCESSING_TIMEOUT_MS", value: "-1"},
		{name: "zero reconcile interval", key: "TSZ_POLICY_RECONCILE_INTERVAL", value: "0s"},
		{name: "invalid reconcile interval", key: "TSZ_POLICY_RECONCILE_INTERVAL", value: "daily"},
		{name: "zero policy max staleness", key: "TSZ_POLICY_MAX_STALENESS", value: "0s"},
		{name: "invalid policy max staleness", key: "TSZ_POLICY_MAX_STALENESS", value: "daily"},
		{name: "zero reconcile failure threshold", key: "TSZ_POLICY_RECONCILE_FAILURE_THRESHOLD", value: "0"},
		{name: "negative shutdown timeout", key: "TSZ_GRACEFUL_SHUTDOWN_TIMEOUT", value: "-1s"},
		{name: "empty value", key: "TSZ_MAX_BODY_BYTES", value: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearExtProcEnv(t)
			t.Setenv(test.key, test.value)

			_, err := LoadExtProcConfig()
			if err == nil {
				t.Fatal("LoadExtProcConfig() error = nil, want startup error")
			}
			if !strings.Contains(err.Error(), test.key) {
				t.Fatalf("error %q does not identify %s", err, test.key)
			}
		})
	}
}

func clearExtProcEnv(t *testing.T) {
	t.Helper()
	for _, key := range extProcEnvKeys {
		value, exists := os.LookupEnv(key)
		if exists {
			t.Cleanup(func() {
				_ = os.Setenv(key, value)
			})
		} else {
			t.Cleanup(func() {
				_ = os.Unsetenv(key)
			})
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
}
