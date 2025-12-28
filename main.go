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
	"thyris-sz/internal/cache"
	"thyris-sz/internal/config"
	"thyris-sz/internal/database"
	"thyris-sz/internal/guardrails"
	"thyris-sz/internal/handlers"
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

	mux := http.NewServeMux()

	// Health Check Endpoints
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

	mux.HandleFunc("POST /detect", func(w http.ResponseWriter, r *http.Request) {
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
		result := detector.Detect(req)

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
	})

	// OpenAI-compatible LLM gateway (chat completions)
	mux.HandleFunc("POST /v1/chat/completions", handlers.NewOpenAIChatGateway(detector))

	mux.HandleFunc("POST /patterns", handlers.CreatePattern)
	mux.HandleFunc("GET /patterns", handlers.ListPatterns)
	mux.HandleFunc("DELETE /patterns/{id}", handlers.DeletePattern)

	mux.HandleFunc("POST /allowlist", handlers.CreateAllowlistItem)
	mux.HandleFunc("GET /allowlist", handlers.ListAllowlistItems)
	mux.HandleFunc("DELETE /allowlist/{id}", handlers.DeleteAllowlistItem)

	mux.HandleFunc("POST /blacklist", handlers.CreateBlacklistItem)
	mux.HandleFunc("GET /blacklist", handlers.ListBlacklistItems)
	mux.HandleFunc("DELETE /blacklist/{id}", handlers.DeleteBlacklistItem)

	mux.HandleFunc("POST /validators", handlers.CreateValidator)
	mux.HandleFunc("GET /validators", handlers.ListValidators)
	mux.HandleFunc("DELETE /validators/{id}", handlers.DeleteValidator)

	// Template Endpoints
	mux.HandleFunc("POST /templates/import", handlers.ImportTemplateHandler)

	// Admin Endpoints
	mux.HandleFunc("POST /admin/reload", handlers.ReloadCache)

	server := &http.Server{
		Addr:    ":" + config.AppConfig.ServerPort,
		Handler: mux,
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
