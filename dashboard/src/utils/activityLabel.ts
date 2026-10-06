import type { AuditLogEntry } from '../types/audit'

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
  return `${String(entry.action)} (${entry.status})`
}
