package config

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultExtProcHTTPPort                        = 8080
	defaultExtProcGRPCPort                        = 9002
	defaultExtProcMaxConcurrentStreams            = 100
	defaultExtProcMaxGRPCMessageBytes             = 4 * 1024 * 1024
	defaultExtProcMaxBodyBytes                    = 1024 * 1024
	defaultExtProcMaxStreamBufferBytes            = 256 * 1024
	defaultExtProcProcessingTimeoutMS             = 2000
	defaultExtProcPolicyReconcileInterval         = 30 * time.Second
	defaultExtProcPolicyMaxStaleness              = 5 * time.Minute
	defaultExtProcPolicyReconcileFailureThreshold = 3
	defaultExtProcGracefulShutdownTimeout         = 10 * time.Second
)

// DefaultExtProcProcessingTimeout is also used by legacy AI validation as its
// bounded fallback budget, keeping the two request-processing paths aligned.
const DefaultExtProcProcessingTimeout = time.Duration(defaultExtProcProcessingTimeoutMS) * time.Millisecond

type ExtProcFailMode string

const (
	ExtProcFailClosed ExtProcFailMode = "closed"
	ExtProcFailOpen   ExtProcFailMode = "open"
)

// ExtProcConfig contains the transport and runtime limits for tsz-ext-proc.
// Durations are parsed once at startup so invalid configuration cannot reach
// request processing.
type ExtProcConfig struct {
	HTTPPort                        int
	GRPCPort                        int
	FailMode                        ExtProcFailMode
	MaxConcurrentStreams            uint32
	MaxGRPCMessageBytes             int
	MaxBodyBytes                    int64
	MaxStreamBufferBytes            int
	ProcessingTimeout               time.Duration
	PolicyReconcileInterval         time.Duration
	PolicyMaxStaleness              time.Duration
	PolicyReconcileFailureThreshold uint32
	GracefulShutdownTimeout         time.Duration
	// GRPCTLSCertFile, GRPCTLSKeyFile and GRPCTLSClientCAFile enable mTLS on
	// the external-processor gRPC listener. They must be configured together.
	GRPCTLSCertFile     string
	GRPCTLSKeyFile      string
	GRPCTLSClientCAFile string
}

// LoadExtProcConfig reads and strictly validates ext-proc environment values.
// Unlike legacy config helpers, malformed values are returned as startup
// errors rather than silently replaced with defaults.
func LoadExtProcConfig() (*ExtProcConfig, error) {
	httpPort, err := positiveInt("TSZ_HTTP_PORT", defaultExtProcHTTPPort)
	if err != nil {
		return nil, err
	}
	if httpPort > 65535 {
		return nil, fmt.Errorf("TSZ_HTTP_PORT must be between 1 and 65535, got %d", httpPort)
	}

	grpcPort, err := positiveInt("TSZ_GRPC_PORT", defaultExtProcGRPCPort)
	if err != nil {
		return nil, err
	}
	if grpcPort > 65535 {
		return nil, fmt.Errorf("TSZ_GRPC_PORT must be between 1 and 65535, got %d", grpcPort)
	}
	if grpcPort == httpPort {
		return nil, fmt.Errorf("TSZ_GRPC_PORT and TSZ_HTTP_PORT must use different ports, both are %d", grpcPort)
	}

	failMode := ExtProcFailMode(strings.ToLower(envOrDefault("TSZ_FAIL_MODE", string(ExtProcFailClosed))))
	if failMode != ExtProcFailClosed && failMode != ExtProcFailOpen {
		return nil, fmt.Errorf("TSZ_FAIL_MODE must be %q or %q, got %q", ExtProcFailClosed, ExtProcFailOpen, failMode)
	}

	maxStreams, err := positiveInt64("TSZ_MAX_CONCURRENT_STREAMS", defaultExtProcMaxConcurrentStreams)
	if err != nil {
		return nil, err
	}
	if maxStreams > math.MaxUint32 {
		return nil, fmt.Errorf("TSZ_MAX_CONCURRENT_STREAMS must be at most %d, got %d", uint64(math.MaxUint32), maxStreams)
	}

	maxGRPCMessageBytes, err := positiveInt("TSZ_MAX_GRPC_MESSAGE_BYTES", defaultExtProcMaxGRPCMessageBytes)
	if err != nil {
		return nil, err
	}
	maxBodyBytes, err := positiveInt64("TSZ_MAX_BODY_BYTES", defaultExtProcMaxBodyBytes)
	if err != nil {
		return nil, err
	}
	maxStreamBufferBytes, err := positiveInt("TSZ_MAX_STREAM_BUFFER_BYTES", defaultExtProcMaxStreamBufferBytes)
	if err != nil {
		return nil, err
	}
	processingTimeoutMS, err := positiveInt64("TSZ_PROCESSING_TIMEOUT_MS", defaultExtProcProcessingTimeoutMS)
	if err != nil {
		return nil, err
	}
	if processingTimeoutMS > math.MaxInt64/int64(time.Millisecond) {
		return nil, fmt.Errorf("TSZ_PROCESSING_TIMEOUT_MS is too large: %d", processingTimeoutMS)
	}

	reconcileInterval, err := positiveDuration("TSZ_POLICY_RECONCILE_INTERVAL", defaultExtProcPolicyReconcileInterval)
	if err != nil {
		return nil, err
	}
	maxStaleness, err := positiveDuration("TSZ_POLICY_MAX_STALENESS", defaultExtProcPolicyMaxStaleness)
	if err != nil {
		return nil, err
	}
	failureThreshold, err := positiveInt64("TSZ_POLICY_RECONCILE_FAILURE_THRESHOLD", defaultExtProcPolicyReconcileFailureThreshold)
	if err != nil {
		return nil, err
	}
	if failureThreshold > math.MaxUint32 {
		return nil, fmt.Errorf("TSZ_POLICY_RECONCILE_FAILURE_THRESHOLD must be at most %d, got %d", uint64(math.MaxUint32), failureThreshold)
	}
	shutdownTimeout, err := positiveDuration("TSZ_GRACEFUL_SHUTDOWN_TIMEOUT", defaultExtProcGracefulShutdownTimeout)
	if err != nil {
		return nil, err
	}
	grpcTLSCertFile := envOrDefault("TSZ_GRPC_TLS_CERT_FILE", "")
	grpcTLSKeyFile := envOrDefault("TSZ_GRPC_TLS_KEY_FILE", "")
	grpcTLSClientCAFile := envOrDefault("TSZ_GRPC_TLS_CLIENT_CA_FILE", "")
	grpcTLSFiles := []string{grpcTLSCertFile, grpcTLSKeyFile, grpcTLSClientCAFile}
	configuredTLSFiles := 0
	for _, file := range grpcTLSFiles {
		if file != "" {
			configuredTLSFiles++
		}
	}
	if configuredTLSFiles != 0 && configuredTLSFiles != len(grpcTLSFiles) {
		return nil, fmt.Errorf("TSZ_GRPC_TLS_CERT_FILE, TSZ_GRPC_TLS_KEY_FILE and TSZ_GRPC_TLS_CLIENT_CA_FILE must be configured together")
	}

	return &ExtProcConfig{
		HTTPPort:                        httpPort,
		GRPCPort:                        grpcPort,
		FailMode:                        failMode,
		MaxConcurrentStreams:            uint32(maxStreams),
		MaxGRPCMessageBytes:             maxGRPCMessageBytes,
		MaxBodyBytes:                    maxBodyBytes,
		MaxStreamBufferBytes:            maxStreamBufferBytes,
		ProcessingTimeout:               time.Duration(processingTimeoutMS) * time.Millisecond,
		PolicyReconcileInterval:         reconcileInterval,
		PolicyMaxStaleness:              maxStaleness,
		PolicyReconcileFailureThreshold: uint32(failureThreshold),
		GracefulShutdownTimeout:         shutdownTimeout,
		GRPCTLSCertFile:                 grpcTLSCertFile,
		GRPCTLSKeyFile:                  grpcTLSKeyFile,
		GRPCTLSClientCAFile:             grpcTLSClientCAFile,
	}, nil
}

func envOrDefault(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	return strings.TrimSpace(value)
}

func positiveInt(key string, fallback int) (int, error) {
	value, err := positiveInt64(key, int64(fallback))
	if err != nil {
		return 0, err
	}
	if value > int64(^uint(0)>>1) {
		return 0, fmt.Errorf("%s is too large: %d", key, value)
	}
	return int(value), nil
}

func positiveInt64(key string, fallback int64) (int64, error) {
	raw := envOrDefault(key, strconv.FormatInt(fallback, 10))
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a positive integer, got %q: %w", key, raw, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero, got %d", key, value)
	}
	return value, nil
}

func positiveDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := envOrDefault(key, fallback.String())
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration such as 30s or 1m, got %q: %w", key, raw, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero, got %q", key, raw)
	}
	return value, nil
}
