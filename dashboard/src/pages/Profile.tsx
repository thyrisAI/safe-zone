import { useEffect, useState } from 'react'
import { getMyActivity } from '../api/audit'
import ChangePasswordForm from '../components/ChangePasswordForm'
import { useAuth } from '../context/AuthContext'
import type { AuditLogEntry } from '../types/audit'
import { activityLabel } from '../utils/activityLabel'

type LoadState = 'loading' | 'success' | 'empty' | 'error'

export default function Profile() {
  const { user } = useAuth()
  const [entries, setEntries] = useState<AuditLogEntry[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [refreshCount, setRefreshCount] = useState(0)

  useEffect(() => {
    let cancelled = false

    getMyActivity()
      .then((data) => {
        if (cancelled) return
        setEntries(data)
        setLoadState(data.length === 0 ? 'empty' : 'success')
      })
      .catch(() => {
        if (!cancelled) setLoadState('error')
      })

    return () => {
      cancelled = true
    }
  }, [refreshCount])

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Profile</h1>
        <p className="page-subtitle">Your account and recent sign-in activity</p>
      </div>

      <section className="profile-section">
        <h2 className="profile-section-title">Account</h2>
        <dl className="profile-details">
          <dt>Email</dt>
          <dd>{user?.email}</dd>
          <dt>Role</dt>
          <dd>{user?.role === 'admin' ? 'Admin' : 'Viewer'}</dd>
          <dt>Password</dt>
          <dd>
            <ChangePasswordForm onChanged={() => setRefreshCount((n) => n + 1)} />
          </dd>
        </dl>
        {user?.role !== 'admin' && (
          <p className="profile-note">Your email can only be changed by an administrator.</p>
        )}
      </section>

      <section className="profile-section">
        <h2 className="profile-section-title">My recent activity</h2>

        {loadState === 'loading' && <p className="info-message">Loading activity...</p>}

        {loadState === 'error' && (
          <p className="info-message">Unable to load your activity. Please try again later.</p>
        )}

        {loadState === 'empty' && <p className="info-message">No activity recorded yet.</p>}

        {loadState === 'success' && (
          <table className="data-table">
            <thead>
              <tr>
                <th scope="col">Time</th>
                <th scope="col">Activity</th>
                <th scope="col">IP address</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((entry) => (
                <tr key={entry.id}>
                  <td data-label="Time">{new Date(entry.created_at).toLocaleString()}</td>
                  <td data-label="Activity">
                    <span className={entry.status === 'failure' ? 'toggle-label-off' : undefined}>
                      {activityLabel(entry)}
                    </span>
                    {entry.details && <div className="activity-details">{entry.details}</div>}
                  </td>
                  <td data-label="IP address">{entry.ip_address || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  )
}
