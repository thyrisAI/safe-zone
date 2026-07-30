package config

// DashboardConfig, dashboard'un Configuration ekranında gösterilecek
// GÜVENLİ (non-sensitive) ayarları temsil eder.
//
// KRİTİK GÜVENLİK KURALI: Config struct'ının tamamı asla doğrudan
// JSON'a çevrilip döndürülmemeli -- DBDSN, RedisURL, AIAPIKey,
// AuthTokenPermissions gibi alanlar şifre/token içerir.
// Bu DTO, sadece "sistem nasıl davranıyor" bilgisini taşıyan,
// elle seçilmiş bir alt kümedir (bkz. FAZ 4 analizi / issue #16).
type DashboardConfig struct {
	PIIMode          string `json:"pii_mode"`
	GatewayBlockMode string `json:"gateway_block_mode"`
	AppMode          string `json:"app_mode"`

	AIProvider  string `json:"ai_provider"`
	AIModelName string `json:"ai_model_name"`

	SecurityHeadersEnabled bool `json:"security_headers_enabled"`
	CORSEnabled            bool `json:"cors_enabled"`
	AuthEnabled            bool `json:"auth_enabled"`
	RateLimitEnabled       bool `json:"rate_limit_enabled"`

	MaxRequestSizeBytes         int64 `json:"max_request_size_bytes"`
	HandlerTimeoutDetectSeconds int   `json:"handler_timeout_detect_seconds"`
	HandlerTimeoutChatSeconds   int   `json:"handler_timeout_chat_seconds"`
}

// GetDashboardConfig, AppConfig'ten sadece güvenli alanları seçip
// DashboardConfig olarak döner. Yeni bir alan eklerken, önce bu
// alanın hassas veri (secret/token/credential) İÇERMEDİĞİNDEN emin ol.
func GetDashboardConfig() DashboardConfig {
	return DashboardConfig{
		PIIMode:          AppConfig.PIIMode,
		GatewayBlockMode: AppConfig.GatewayBlockMode,
		AppMode:          AppConfig.AppMode,

		AIProvider:  AppConfig.AIProvider,
		AIModelName: AppConfig.AIModelName,

		SecurityHeadersEnabled: AppConfig.SecurityHeadersEnabled,
		CORSEnabled:            AppConfig.CORSEnabled,
		AuthEnabled:            AppConfig.AuthEnabled,
		RateLimitEnabled:       AppConfig.RateLimitEnabled,

		MaxRequestSizeBytes:         AppConfig.MaxRequestSizeBytes,
		HandlerTimeoutDetectSeconds: AppConfig.HandlerTimeoutDetectSeconds,
		HandlerTimeoutChatSeconds:   AppConfig.HandlerTimeoutChatSeconds,
	}
}
