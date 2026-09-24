import { useEffect, useState } from 'react'
import { getDashboardSummary } from '../api/dashboard'
import { getDashboardEvents } from '../api/dashboard'
import { ApiError } from '../api/client'
import type { DashboardSummary, DashboardEvent } from '../types/dashboard'
import StatusPill from '../components/StatusPill'

type LoadState = 'loading' | 'success' | 'empty' | 'error' | 'unauthorized'

export default function Overview() {
  const [summary, setSummary] = useState<DashboardSummary | null>(null)
  const [events, setEvents] = useState<DashboardEvent[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')

  useEffect(() => {
    let cancelled = false

    async function loadOverview() {
      setLoadState('loading')
      try {
        const [summaryData, eventsData] = await Promise.all([
          getDashboardSummary(),
          getDashboardEvents(8),
        ])
        if (cancelled) return

        setSummary(summaryData)
        setEvents(eventsData)
        setLoadState(eventsData.length === 0 ? 'empty' : 'success')
      } catch (err) {
        if (cancelled) return

        if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
          setLoadState('unauthorized')
        } else {
          setLoadState('error')
        }
      }
    }

    loadOverview()
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Overview</h1>
        <p className="page-subtitle">System activity and summary statistics</p>
      </div>

      {loadState === 'loading' && <p className="info-message">Loading overview...</p>}

      {loadState === 'unauthorized' && (
        <p className="info-message">
          You don't have permission to view this data. Contact your administrator if you believe this is an error.
        </p>
      )}

      {loadState === 'error' && (
        <p className="info-message">
          Unable to load overview data. Please check your connection and try again.
        </p>
      )}

      {(loadState === 'success' || loadState === 'empty') && summary && (
        <div className="stat-grid">
          <div className="stat-card">
            <span className="stat-label">Total Requests</span>
            <span className="stat-value">{summary.total_requests}</span>
          </div>
          <div className="stat-card">
            <span className="stat-label">Allowed</span>
            <span className="stat-value">{summary.allowed}</span>
          </div>
          <div className="stat-card">
            <span className="stat-label">Blocked</span>
            <span className="stat-value">{summary.blocked}</span>
          </div>
          <div className="stat-card">
            <span className="stat-label">PII Detections</span>
            <span className="stat-value">{summary.pii_detections}</span>
          </div>
        </div>
      )}

      {(loadState === 'success' || loadState === 'empty') && (
        <>
          <h2 className="section-title">Recent Activity</h2>

          {loadState === 'empty' && (
            <p className="info-message">
              No events recorded since last restart. Events will appear here as requests are processed through the gateway.
            </p>
          )}

          {loadState === 'success' && (
            <table className="data-table">
              <thead>
                <tr>
                  <th scope="col">Timestamp</th>
                  <th scope="col">Request ID</th>
                  <th scope="col">Result</th>
                  <th scope="col">Reason</th>
                </tr>
              </thead>
              <tbody>
                {events.map((event) => (
                  <tr key={`${event.request_id}-${event.timestamp}`}>
                    <td data-label="Timestamp">{new Date(event.timestamp).toLocaleString()}</td>
                    <td data-label="Request ID">{event.request_id || '—'}</td>
                    <td data-label="Result">
                      <StatusPill
                        active={!event.blocked}
                        activeLabel="Allowed"
                        inactiveLabel="Blocked"
                      />
                    </td>
                    <td data-label="Reason">{event.reason}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}
    </div>
  )
}