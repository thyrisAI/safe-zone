export type AuditAction = 'login' | 'logout' | 'password_change' | 'user_created'
export type AuditStatus = 'success' | 'failure'

export interface AuditLogEntry {
  id: number
  user_id?: number // absent when the email matched no registered user
  actor_email: string
  action: AuditAction
  status: AuditStatus
  ip_address: string
  details?: string // what the action was done to, e.g. a created account
  created_at: string // ISO 8601
}

/** A user id, or 'none' for emails that matched no registered user. */
export type AuditUserFilter = number | 'none'

export interface AuditFilter {
  userId?: AuditUserFilter
  status?: AuditStatus
}
