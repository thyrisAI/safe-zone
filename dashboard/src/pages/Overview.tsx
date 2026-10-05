import { useEffect, useState } from 'react'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@thyris/ui'
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
        <div className="mb-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <Card>
            <CardHeader className="pb-2">
              <CardDescription>Total Requests</CardDescription>
            </CardHeader>
            <CardContent>
              <CardTitle className="font-mono text-3xl">{summary.total_requests}</CardTitle>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <CardDescription>Allowed</CardDescription>
            </CardHeader>
            <CardContent>
              <CardTitle className="font-mono text-3xl">{summary.allowed}</CardTitle>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <CardDescription>Blocked</CardDescription>
            </CardHeader>
            <CardContent>
              <CardTitle className="font-mono text-3xl">{summary.blocked}</CardTitle>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <CardDescription>PII Detections</CardDescription>
            </CardHeader>
            <CardContent>
              <CardTitle className="font-mono text-3xl">{summary.pii_detections}</CardTitle>
            </CardContent>
          </Card>
        </div>
      )}

      {(loadState === 'success' || loadState === 'empty') && (
        <>
          <h2 className="mb-4 text-lg font-semibold">Recent Activity</h2>

          {loadState === 'empty' && (
            <p className="info-message">
              No events recorded since last restart. Events will appear here as requests are processed through the gateway.
            </p>
          )}

          {loadState === 'success' && (
            <Table className="data-table">
              <TableHeader>
                <TableRow>
                  <TableHead scope="col">Timestamp</TableHead>
                  <TableHead scope="col">Request ID</TableHead>
                  <TableHead scope="col">Result</TableHead>
                  <TableHead scope="col">Reason</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {events.map((event) => (
                  <TableRow key={`${event.request_id}-${event.timestamp}`}>
                    <TableCell data-label="Timestamp">{new Date(event.timestamp).toLocaleString()}</TableCell>
                    <TableCell data-label="Request ID">{event.request_id || '—'}</TableCell>
                    <TableCell data-label="Result">
                      <StatusPill
                        active={!event.blocked}
                        activeLabel="Allowed"
                        inactiveLabel="Blocked"
                      />
                    </TableCell>
                    <TableCell data-label="Reason">{event.reason}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </>
      )}
    </div>
  )
}
