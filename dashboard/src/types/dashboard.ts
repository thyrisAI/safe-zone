/**
 * Shape of the GET /dashboard/summary response.
 * See internal/metrics/store.go - Summary struct.
 */
export interface DashboardSummary {
  total_requests: number
  allowed: number
  blocked: number
  pii_detections: number
}

/**
 * Shape of a single event in the GET /dashboard/events response.
 * See internal/metrics/store.go - Event struct.
 *
 * Note: the raw PII value is intentionally absent -- the backend
 * never includes it in the event record (security decision, see
 * internal/metrics/store.go).
 */
export interface DashboardEvent {
  timestamp: string // ISO 8601 string (Go's time.Time JSON format)
  request_id: string
  blocked: boolean
  reason: 'PII' | 'GUARDRAIL' | 'RULE'
}

/**
 * Shape of the GET /dashboard/config response.
 * See internal/config/dashboard_dto.go - DashboardConfig struct.
 * Contains only safe (non-sensitive) fields, filtered on the backend.
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