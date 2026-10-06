import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { listAuditLogs } from '../api/audit'
import { listUsers } from '../api/users'
import { ApiError } from '../api/client'
import type { AuditFilter, AuditLogEntry, AuditStatus } from '../types/audit'
import type { DashboardUser } from '../types/users'
import { useAuth } from '../context/AuthContext'
import { activityLabel } from '../utils/activityLabel'

type LoadState = 'loading' | 'success' | 'empty' | 'error' | 'unauthorized'

// Dropdown values: 'all' = no filter, 'none' = unregistered emails,
// otherwise a user id as a string.
type UserFilterValue = 'all' | 'none' | string
type StatusFilterValue = 'all' | AuditStatus

function toFilter(userFilter: UserFilterValue, statusFilter: StatusFilterValue): AuditFilter {
  const filter: AuditFilter = {}
  if (userFilter === 'none') filter.userId = 'none'
  else if (userFilter !== 'all') filter.userId = Number(userFilter)
  if (statusFilter !== 'all') filter.status = statusFilter
  return filter
}

// Reads the optional ?user= link parameter (used by the Users page).
// Only a plain number or 'none' is accepted; anything else is ignored.
function initialUserFilter(param: string | null): UserFilterValue {
  if (param === 'none') return 'none'
  if (param !== null && /^\d+$/.test(param)) return param
  return 'all'
}

export default function Activity() {
  const { user: currentUser } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()
  const [entries, setEntries] = useState<AuditLogEntry[]>([])
  const [users, setUsers] = useState<DashboardUser[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [userFilter, setUserFilter] = useState<UserFilterValue>(() => initialUserFilter(searchParams.get('user')))
  const [statusFilter, setStatusFilter] = useState<StatusFilterValue>('all')
  const [refreshCount, setRefreshCount] = useState(0)

  // The user dropdown is filled from the account list. If this fails the
  // page still works: only the per-user choices are missing.
  useEffect(() => {
    let cancelled = false
    listUsers()
      .then((data) => {
        if (!cancelled) setUsers(data)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    let cancelled = false

    async function loadEntries() {
      setLoadState('loading')
      try {
        const data = await listAuditLogs(toFilter(userFilter, statusFilter))
        if (cancelled) return

        setEntries(data)
        setLoadState(data.length === 0 ? 'empty' : 'success')
      } catch (err) {
        if (cancelled) return

        if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
          setLoadState('unauthorized')
        } else {
          setLoadState('error')
        }
      }
    }

    loadEntries()
    return () => {
      cancelled = true
    }
  }, [userFilter, statusFilter, refreshCount])

  const filtersActive = userFilter !== 'all' || statusFilter !== 'all'

  function userLabel(): string {
    if (userFilter === 'none') return 'unregistered emails'
    return users.find((u) => String(u.ID) === userFilter)?.email ?? `user #${userFilter}`
  }

  function summaryText(): string {
    const count = entries.length
    let text = `Showing ${count} ${count === 1 ? 'event' : 'events'}`
    if (userFilter !== 'all') text += ` for ${userLabel()}`
    if (statusFilter !== 'all') text += ` · ${statusFilter === 'success' ? 'successful' : 'failed'} only`
    return text
  }

  // Only a record tied to a registered user can be "mine"; an unregistered
  // email that happens to equal mine must not be marked.
  function isOwnEntry(entry: AuditLogEntry): boolean {
    return entry.user_id !== undefined && entry.actor_email === currentUser?.email
  }

  function clearFilters() {
    setUserFilter('all')
    setStatusFilter('all')
    if (searchParams.has('user')) setSearchParams({}, { replace: true })
  }

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Activity</h1>
        <p className="page-subtitle">Sign-in and sign-out activity (most recent 100 records)</p>
      </div>

      <div className="filter-bar">
        <div className="form-field">
          <label htmlFor="activity-user">User</label>
          <select
            id="activity-user"
            value={userFilter}
            onChange={(e) => setUserFilter(e.target.value)}
          >
            <option value="all">All users</option>
            {users.map((u) => (
              <option key={u.ID} value={String(u.ID)}>
                {u.email} · {u.role === 'admin' ? 'Admin' : 'Viewer'}
              </option>
            ))}
            <option value="none">Unregistered emails</option>
          </select>
        </div>

        <div className="form-field">
          <label htmlFor="activity-status">Result</label>
          <select
            id="activity-status"
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value as StatusFilterValue)}
          >
            <option value="all">All results</option>
            <option value="success">Successful</option>
            <option value="failure">Failed</option>
          </select>
        </div>

        <button
          type="button"
          className="button-primary filter-bar-end"
          onClick={() => setRefreshCount((n) => n + 1)}
        >
          Refresh
        </button>
      </div>

      {filtersActive && (loadState === 'success' || loadState === 'empty') && (
        <div className="filter-summary">
          <span>{summaryText()}</span>
          <button type="button" className="link-button" onClick={clearFilters}>
            Clear filters
          </button>
        </div>
      )}

      {loadState === 'loading' && <p className="info-message">Loading activity...</p>}

      {loadState === 'unauthorized' && (
        <p className="info-message">
          You don't have permission to view this data. Contact your administrator if you believe this is an error.
        </p>
      )}

      {loadState === 'error' && (
        <p className="info-message">Unable to load activity. Please check your connection and try again.</p>
      )}

      {loadState === 'empty' && (
        <p className="info-message">
          {filtersActive ? 'No activity matches these filters.' : 'No activity recorded yet.'}
        </p>
      )}

      {loadState === 'success' && (
        <table className="data-table">
          <thead>
            <tr>
              <th scope="col">Time</th>
              <th scope="col">User</th>
              <th scope="col">Activity</th>
              <th scope="col">IP address</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((entry) => (
              <tr key={entry.id} className={isOwnEntry(entry) ? 'row-self' : undefined}>
                <td data-label="Time">{new Date(entry.created_at).toLocaleString()}</td>
                <td data-label="User">
                  {entry.user_id !== undefined ? (
                    <button
                      type="button"
                      className="link-button"
                      title="Show only this user's activity"
                      onClick={() => setUserFilter(String(entry.user_id))}
                    >
                      {entry.actor_email}
                    </button>
                  ) : (
                    <>
                      {entry.actor_email} <span className="tag-muted">Not a registered user</span>
                    </>
                  )}
                  {isOwnEntry(entry) && <span className="tag-you">You</span>}
                </td>
                <td data-label="Activity">
                  <span className={entry.status === 'failure' ? 'toggle-label-off' : undefined}>
                    {activityLabel(entry)}
                  </span>
                </td>
                <td data-label="IP address">{entry.ip_address || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
