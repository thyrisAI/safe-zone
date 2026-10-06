import { request } from './client'
import type { AuditFilter, AuditLogEntry } from '../types/audit'

/** Admin-only: the most recent audit records, newest first. */
export async function listAuditLogs(filter: AuditFilter = {}): Promise<AuditLogEntry[]> {
  const params = new URLSearchParams()
  if (filter.userId !== undefined) params.set('user_id', String(filter.userId))
  if (filter.status) params.set('status', filter.status)

  const query = params.toString()
  return request<AuditLogEntry[]>(`/audit/logs${query ? `?${query}` : ''}`)
}
