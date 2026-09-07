package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"thyris-sz/internal/ai"
	"thyris-sz/internal/auth"
	"thyris-sz/internal/cache"
	"thyris-sz/internal/config"
	"thyris-sz/internal/database"
	"thyris-sz/internal/guardrails"
	"thyris-sz/internal/handlers"
	"thyris-sz/internal/middleware"
	"thyris-sz/internal/models"
	"time"
)

func main() {
	// Load Config
	config.LoadConfig()

	// Initialize Database
	database.InitDB()

	// Initialize Redis
	cache.InitRedis()

	// Initialize AI Provider
	if err := ai.InitProvider(); err != nil {
		log.Printf("Warning: Failed to initialize AI provider: %v (gateway will use direct HTTP)", err)
	}

	// Log Configuration
	log.Printf("PII Mode: [%s] | Gateway Block Mode: [%s] | AI Provider: %s",
		config.AppConfig.PIIMode,
		config.AppConfig.GatewayBlockMode,
		config.AppConfig.AIProvider)

	detector := guardrails.NewDetector()
	guardrailService, err := guardrails.NewGuardrailService(detector)
	if err != nil {
		log.Fatalf("Failed to initialize guardrail service: %v", err)
	}

	mux := http.NewServeMux()

	// ===== MILESTONE 1: HTTP SECURITY HARDENING =====
	// Initialize middleware (before registering handlers)

	// Initialize CORS middleware
	middleware.InitCORS(
		config.AppConfig.CORSEnabled,
		config.AppConfig.CORSAllowedOrigins,
		config.AppConfig.CORSAllowedMethods,
		config.AppConfig.CORSAllowedHeaders,
		config.AppConfig.CORSMaxAge,
	)

	// Initialize request limits
	middleware.InitRequestLimits(middleware.RequestLimitsConfig{
		MaxBodyBytes:            config.AppConfig.MaxRequestSizeBytes,
		DetectTimeoutSeconds:    config.AppConfig.HandlerTimeoutDetectSeconds,
		ChatTimeoutSeconds:      config.AppConfig.HandlerTimeoutChatSeconds,
		HTTPReadTimeoutSeconds:  config.AppConfig.HTTPReadTimeoutSeconds,
		HTTPWriteTimeoutSeconds: config.AppConfig.HTTPWriteTimeoutSeconds,
		HTTPIdleTimeoutSeconds:  config.AppConfig.HTTPIdleTimeoutSeconds,
	})

	// Set content type validation
	middleware.SetValidateContentType(config.AppConfig.ValidateContentType)

	// Initialize authentication
	auth.Init(auth.AuthConfig{
		Enabled:            config.AppConfig.AuthEnabled,
		TokenPermissions:   config.AppConfig.AuthTokenPermissions,
		AdminAPIKey:        os.Getenv("ADMIN_API_KEY"),
		PublicPaths:        config.AppConfig.AuthPublicPaths,
		RequireBearerToken: config.AppConfig.AuthRequireBearerToken,
	})

	// Initialize rate limiting
	middleware.InitRateLimit(middleware.RateLimitConfig{
		Enabled:           config.AppConfig.RateLimitEnabled,
		RequestsPerSecond: config.AppConfig.RateLimitRequestsPerSecond,
		Burst:             config.AppConfig.RateLimitBurst,
		DetectPerMinute:   config.AppConfig.RateLimitDetectPerMinute,
		ChatPerMinute:     config.AppConfig.RateLimitChatPerMinute,
		PatternsPerMinute: config.AppConfig.RateLimitPatternsPerMinute,
		AdminPerMinute:    config.AppConfig.RateLimitAdminPerMinute,
	})

	// Log security configuration
	log.Printf("[Security] Headers: %v | CORS: %v | MaxRequestSize: %d bytes | Timeouts: detect=%ds, chat=%ds",
		config.AppConfig.SecurityHeadersEnabled,
		config.AppConfig.CORSEnabled,
		config.AppConfig.MaxRequestSizeBytes,
		config.AppConfig.HandlerTimeoutDetectSeconds,
		config.AppConfig.HandlerTimeoutChatSeconds,
	)

	// ===== END MIDDLEWARE INITIALIZATION =====
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("UP"))
	})

	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if sqlDB, err := database.DB.DB(); err != nil || sqlDB.Ping() != nil {
			http.Error(w, "Database not ready", http.StatusServiceUnavailable)
			return
		}
		if err := cache.RDB.Ping(context.Background()).Err(); err != nil {
			http.Error(w, "Redis not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("READY"))
	})

	mux.Handle("POST /detect", auth.RequirePermission("detect:read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req models.DetectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Validation
		if req.Text == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Text field is required"})
			return
		}

		if req.Mode != "" {
			validModes := map[string]bool{
				"MASK":   true,
				"BLOCK":  true,
				"DETECT": true,
			}
			if !validModes[req.Mode] {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "Invalid mode"})
				return
			}
		}

		startTime := time.Now()
		result, err := guardrails.DetectLegacy(r.Context(), guardrailService, req)
		if err != nil {
			http.Error(w, "Guardrail inspection failed", http.StatusInternalServerError)
			return
		}

		var breakdownParts []string
		totalDetections := 0
		for typeName, count := range result.Breakdown {
			breakdownParts = append(breakdownParts, fmt.Sprintf("%s: %d", typeName, count))
			totalDetections += count
		}
		breakdownStr := strings.Join(breakdownParts, ", ")
		if breakdownStr == "" {
			breakdownStr = "None"
		}

		rid := req.RID
		if rid == "" {
			rid = "NO-RID"
		}

		log.Printf("[AUDIT] Request ID: %s | Time: %s | Duration: %v | Total Found: %d | Breakdown: {%s}",
			rid,
			startTime.Format(time.RFC3339),
			time.Since(startTime),
			totalDetections,
			breakdownStr,
		)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})))

	// OpenAI-compatible LLM gateway (chat completions)
	mux.Handle(
		"POST /v1/chat/completions",
		auth.RequirePermission("gateway:use")(http.HandlerFunc(handlers.NewOpenAIChatGateway(guardrailService))),
	)

	mux.Handle("POST /patterns", auth.RequirePermission("patterns:admin")(http.HandlerFunc(handlers.CreatePattern)))
	mux.Handle("GET /patterns", auth.RequirePermission("patterns:admin")(http.HandlerFunc(handlers.ListPatterns)))
	mux.Handle("DELETE /patterns/{id}", auth.RequirePermission("patterns:admin")(http.HandlerFunc(handlers.DeletePattern)))

	mux.Handle("POST /allowlist", auth.RequirePermission("allowlist:admin")(http.HandlerFunc(handlers.CreateAllowlistItem)))
	mux.Handle("GET /allowlist", auth.RequirePermission("allowlist:admin")(http.HandlerFunc(handlers.ListAllowlistItems)))
	mux.Handle("DELETE /allowlist/{id}", auth.RequirePermission("allowlist:admin")(http.HandlerFunc(handlers.DeleteAllowlistItem)))

	mux.Handle("POST /blacklist", auth.RequirePermission("blacklist:admin")(http.HandlerFunc(handlers.CreateBlacklistItem)))
	mux.Handle("GET /blacklist", auth.RequirePermission("blacklist:admin")(http.HandlerFunc(handlers.ListBlacklistItems)))
	mux.Handle("DELETE /blacklist/{id}", auth.RequirePermission("blacklist:admin")(http.HandlerFunc(handlers.DeleteBlacklistItem)))

	mux.Handle("POST /validators", auth.RequirePermission("validators:admin")(http.HandlerFunc(handlers.CreateValidator)))
	mux.Handle("GET /validators", auth.RequirePermission("validators:admin")(http.HandlerFunc(handlers.ListValidators)))
	mux.Handle("DELETE /validators/{id}", auth.RequirePermission("validators:admin")(http.HandlerFunc(handlers.DeleteValidator)))

	// Template Endpoints
	mux.Handle("POST /templates/import", auth.RequirePermission("templates:admin")(http.HandlerFunc(handlers.ImportTemplateHandler)))

	// Admin Endpoints
	mux.Handle("POST /admin/reload", auth.RequirePermission("cache:admin")(http.HandlerFunc(handlers.ReloadCache)))

	// Dashboard Endpoints (bkz. issue #16 -- read-only, in-memory metrics)

	mux.Handle("GET /dashboard/summary", auth.RequirePermission("dashboard:read")(http.HandlerFunc(handlers.GetDashboardSummary)))
	mux.Handle("GET /dashboard/events", auth.RequirePermission("dashboard:read")(http.HandlerFunc(handlers.GetDashboardEvents)))
	mux.Handle("GET /dashboard/config", auth.RequirePermission("dashboard:read")(http.HandlerFunc(handlers.GetDashboardConfig)))

	// ===== MILESTONE 1: MIDDLEWARE WRAPPING =====
	// Wrap mux with middleware (applied in reverse order: last middleware is outermost)
	var handler http.Handler = mux

	if config.AppConfig.SecurityHeadersEnabled {
		handler = middleware.SecurityHeaders(handler)
		log.Println("[Security] Security headers middleware enabled")
	}

	handler = middleware.CORS(handler)
	handler = middleware.InputValidation(handler)
	handler = middleware.RequestLimits(config.AppConfig.MaxRequestSizeBytes)(handler)
	handler = middleware.RateLimit(handler)
	handler = auth.Middleware(handler)

	// ===== END MIDDLEWARE WRAPPING =====

	server := &http.Server{
		Addr:    ":" + config.AppConfig.ServerPort,
		Handler: handler,
		// ===== MILESTONE 1: HTTP SECURITY TIMEOUTS =====
		ReadTimeout:    time.Duration(config.AppConfig.HTTPReadTimeoutSeconds) * time.Second,
		WriteTimeout:   time.Duration(config.AppConfig.HTTPWriteTimeoutSeconds) * time.Second,
		IdleTimeout:    time.Duration(config.AppConfig.HTTPIdleTimeoutSeconds) * time.Second,
		MaxHeaderBytes: config.AppConfig.HTTPMaxHeaderBytes,
		// ===== END HTTP TIMEOUTS =====
	}

	// Graceful Shutdown
	go func() {
		log.Printf("Server starting on :%s...", config.AppConfig.ServerPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Could not listen on %s: %v\n", config.AppConfig.ServerPort, err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("Server is shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited properly")
}
