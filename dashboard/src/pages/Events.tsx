import { useEffect, useState } from 'react'
import { Button, Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@thyris/ui'
import { getDashboardEvents } from '../api/dashboard'
import { ApiError } from '../api/client'
import type { DashboardEvent } from '../types/dashboard'
import StatusPill from '../components/StatusPill'

type LoadState = 'loading' | 'success' | 'empty' | 'error' | 'unauthorized'

const DEFAULT_LIMIT = 20
const EXPANDED_LIMIT = 50

export default function Events() {
  const [events, setEvents] = useState<DashboardEvent[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [limit, setLimit] = useState(DEFAULT_LIMIT)

  useEffect(() => {
    let cancelled = false

    async function loadEvents() {
      setLoadState('loading')
      try {
        const data = await getDashboardEvents(limit)
        if (cancelled) return

        setEvents(data)
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

    loadEvents()
    return () => {
      cancelled = true
    }
  }, [limit])

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Events</h1>
        <p className="page-subtitle">Recent request activity (since last restart)</p>
      </div>

      {loadState === 'loading' && <p className="info-message">Loading events...</p>}

      {loadState === 'unauthorized' && (
        <p className="info-message">
          You don't have permission to view this data. Contact your administrator if you believe this is an error.
        </p>
      )}

      {loadState === 'error' && (
        <p className="info-message">
          Unable to load events. Please check your connection and try again.
        </p>
      )}

      {loadState === 'empty' && (
        <p className="info-message">
          No events recorded since last restart. Events will appear here as requests are processed through the gateway.
        </p>
      )}

      {loadState === 'success' && (
        <>
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

          {/* The backend does not support real pagination (only "most
              recent N events"), so we use a simple "load more" button
              instead of page-numbered pagination. */}
          {events.length === limit && limit < EXPANDED_LIMIT && (
            <Button
              type="button"
              variant="outline"
              className="mt-4"
              onClick={() => setLimit(EXPANDED_LIMIT)}
            >
              Show more events
            </Button>
          )}
        </>
      )}
    </div>
  )
}
