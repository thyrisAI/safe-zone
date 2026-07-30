package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"thyris-sz/internal/config"
	"thyris-sz/internal/metrics"
)

// GetDashboardSummary, Overview ekranındaki 4 sayaç kartı için
// mevcut in-memory metrikleri JSON olarak döner.
//
// NOT: Bu veriler kalıcı değildir, servis restart olduğunda sıfırlanır.
// Bkz. internal/metrics/store.go üstündeki paket yorumu.
func GetDashboardSummary(w http.ResponseWriter, r *http.Request) {
	summary := metrics.GetSummary()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(summary); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// GetDashboardEvents, Events ekranı için son N event'i JSON olarak döner.
// ?limit query parametresi ile sınırlanabilir (varsayılan: 20, maksimum: 50).
func GetDashboardEvents(w http.ResponseWriter, r *http.Request) {
	limit := 20

	if limitParam := r.URL.Query().Get("limit"); limitParam != "" {
		if parsed, err := strconv.Atoi(limitParam); err == nil && parsed > 0 {
			limit = parsed
		}
		// Geçersiz/negatif bir değer gelirse sessizce varsayılana (20) düşüyoruz;
		// istemciye hata döndürmek bu düşük riskli senaryo için gereksiz.
	}

	// Sınırsız/aşırı büyük bir query'yi engelle (senin promptundaki
	// "recent events için sınırsız query yapma" kuralı).
	if limit > 50 {
		limit = 50
	}

	events := metrics.GetRecentEvents(limit)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(events); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// GetDashboardConfig, Configuration ekranı için güvenli (allowlist'lenmiş)
// sistem ayarlarını JSON olarak döner. Hiçbir zaman secret/token/credential
// içermez -- bkz. internal/config/dashboard_dto.go.
func GetDashboardConfig(w http.ResponseWriter, r *http.Request) {
	cfg := config.GetDashboardConfig()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cfg); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}
