/**
 * GET /dashboard/summary response şekli.
 * Bkz. internal/metrics/store.go - Summary struct'ı.
 */
export interface DashboardSummary {
    total_requests: number
    allowed: number
    blocked: number
    pii_detections: number
  }
  
  /**
   * GET /dashboard/events response şeklindeki tek bir event.
   * Bkz. internal/metrics/store.go - Event struct'ı.
   *
   * Not: Raw PII değeri (value) burada YOK -- backend zaten
   * bu bilgiyi event kaydına dahil etmiyor (FAZ 18 güvenlik kararı).
   */
  export interface DashboardEvent {
    timestamp: string // ISO 8601 string (Go'nun time.Time JSON serialize formatı)
    request_id: string
    blocked: boolean
    reason: 'PII' | 'GUARDRAIL' | 'RULE'
  }
  
  /**
   * GET /dashboard/config response şekli.
   * Bkz. internal/config/dashboard_dto.go - DashboardConfig struct'ı.
   * Sadece güvenli (non-sensitive) alanlar -- backend tarafında filtrelenmiş.
   */
  export interface DashboardConfig {
    pii_mode: string
    gateway_block_mode: string
    app_mode: string
    ai_provider: string
    ai_model_name: string
    security_headers_enabled: boolean
    cors_enabled: boolean
    auth_enabled: boolean
    rate_limit_enabled: boolean
    max_request_size_bytes: number
    handler_timeout_detect_seconds: number
    handler_timeout_chat_seconds: number
  }
  