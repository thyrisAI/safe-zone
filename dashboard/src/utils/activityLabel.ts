import type { AuditLogEntry } from '../types/audit'

/** Actor name the backend uses for changes made with an API token. */
export const API_TOKEN_ACTOR = 'api-token'

const MANAGEMENT_LABELS: Record<string, [string, string]> = {
  pattern_created: ['Created pattern', 'Failed to create pattern'],
  pattern_deleted: ['Deleted pattern', 'Failed to delete pattern'],
  pattern_enabled: ['Enabled pattern', 'Failed to enable pattern'],
  pattern_disabled: ['Disabled pattern', 'Failed to disable pattern'],
  allowlist_added: ['Added allowlist entry', 'Failed to add allowlist entry'],
  allowlist_removed: ['Removed allowlist entry', 'Failed to remove allowlist entry'],
  blacklist_added: ['Added blocklist entry', 'Failed to add blocklist entry'],
  blacklist_removed: ['Removed blocklist entry', 'Failed to remove blocklist entry'],
  guardrail_created: ['Created guardrail', 'Failed to create guardrail'],
  guardrail_deleted: ['Deleted guardrail', 'Failed to delete guardrail'],
}

/** Plain-language text for one audit record. */
export function activityLabel(entry: AuditLogEntry): string {
  if (entry.action === 'login') {
    return entry.status === 'success' ? 'Signed in' : 'Failed sign-in'
  }
  if (entry.action === 'logout') {
    return entry.status === 'success' ? 'Signed out' : 'Failed sign-out'
  }
  if (entry.action === 'password_change') {
    return entry.status === 'success' ? 'Password changed' : 'Failed password change'
  }
  if (entry.action === 'user_created') {
    return entry.status === 'success' ? 'Created user' : 'Failed to create user'
  }
  if (entry.action === 'access_denied') {
    return 'Blocked: not allowed for this role'
  }
  const management = MANAGEMENT_LABELS[entry.action]
  if (management) {
    return entry.status === 'success' ? management[0] : management[1]
  }
  return `${String(entry.action)} (${entry.status})`
}
