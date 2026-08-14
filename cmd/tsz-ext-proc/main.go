package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"thyris-sz/internal/cache"
	"thyris-sz/internal/config"
	"thyris-sz/internal/database"
	"thyris-sz/internal/extproc"
	"thyris-sz/internal/extproc/envoy"
	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

// exampleFaultAuditor exists exclusively so the checked-in BYG examples can
// exercise failure_policy without taking a real dependency down. It is opt-in
// and is never enabled by the deployment manifest.
type exampleFaultAuditor struct{}

func (exampleFaultAuditor) Audit(context.Context, guardrails.AuditEvent) error {
	return errors.New("BYG example audit fault injection")
}

func main() {
	config.LoadConfig()
	extProcConfig, err := config.LoadExtProcConfig()
	if err != nil {
		log.Fatalf("invalid ext-proc configuration: %v", err)
	}

	database.InitDB()
	cache.InitRedis()
	detector := guardrails.NewDetector()
	guardrailService, err := guardrails.NewGuardrailService(detector)
	if err != nil {
		log.Fatalf("initialize guardrail service: %v", err)
	}

	sqlDB, err := database.DB.DB()
	if err != nil {
		log.Fatalf("get PostgreSQL connection pool: %v", err)
	}
	policyRepository, err := policy.NewPostgresRepository(sqlDB)
	if err != nil {
		log.Fatalf("initialize policy repository: %v", err)
	}
	policyCache, err := policy.NewCache(policyRepository, cache.RDB, extProcConfig.PolicyReconcileInterval)
	if err != nil {
		log.Fatalf("initialize policy cache: %v", err)
	}
	applicationContext, cancelApplication := context.WithCancel(context.Background())
	defer cancelApplication()
	if err := policyCache.Start(applicationContext); err != nil {
		log.Fatalf("start policy cache: %v", err)
	}
	processor, err := extproc.NewOpenAIRequestProcessor(guardrailService)
	if err != nil {
		log.Fatalf("initialize BYG processor: %v", err)
	}
	var auditor guardrails.Auditor
	if os.Getenv("TSZ_EXAMPLE_AUDIT_FAILURE") == "1" {
		log.Println("BYG example audit fault injection is enabled")
		auditor = exampleFaultAuditor{}
	}
	resolver := extproc.PolicyResolver(extproc.HeaderPolicyResolver{})
	if os.Getenv("TSZ_POLICY_RESOLVER") == "attribute" {
		bindings, err := policy.NewPostgresRoutePolicyBindingStore(sqlDB)
		if err != nil {
			log.Fatalf("initialize native route binding store: %v", err)
		}
		resolver = extproc.AttributePolicyResolver{Mapping: bindings}
	}
	transport, err := envoy.NewServerWithResolverAndSettings(processor, policyCache, resolver, auditor, envoy.ServerSettings{
		FailMode: policy.FailureMode(extProcConfig.FailMode), MaxBodyBytes: extProcConfig.MaxBodyBytes,
		ProcessingTimeout: extProcConfig.ProcessingTimeout,
	}, extProcConfig.MaxConcurrentStreams)
	if err != nil {
		log.Fatalf("initialize Envoy adapter: %v", err)
	}

	dependencies := extproc.Dependencies{
		DB:          database.DB,
		Redis:       cache.RDB,
		PolicyCache: policyCache,
		Registrar:   transport,
	}
	runtime, err := extproc.NewRuntime(extProcConfig, dependencies)
	if err != nil {
		log.Fatalf("initialize ext-proc runtime: %v", err)
	}
	if err := runtime.Start(); err != nil {
		log.Fatalf("start ext-proc runtime: %v", err)
	}
	log.Printf("tsz-ext-proc started: HTTP=:%d gRPC=:%d", extProcConfig.HTTPPort, extProcConfig.GRPCPort)

	signalContext, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	var serveErr error
	select {
	case <-signalContext.Done():
		log.Println("tsz-ext-proc shutdown signal received")
	case serveErr = <-runtime.Errors():
		log.Printf("tsz-ext-proc server stopped unexpectedly: %v", serveErr)
	}
	cancelApplication()

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), extProcConfig.GracefulShutdownTimeout)
	defer cancelShutdown()
	if err := runtime.Shutdown(shutdownContext); err != nil {
		log.Fatalf("shutdown tsz-ext-proc: %v", err)
	}
	if serveErr != nil {
		log.Fatalf("tsz-ext-proc exited after server failure: %v", serveErr)
	}
	log.Println("tsz-ext-proc stopped")
}
