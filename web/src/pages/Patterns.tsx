import { useEffect, useState } from 'react'
import { getPatterns } from '../api/patterns'
import { ApiError } from '../api/client'
import type { Pattern } from '../types/pattern'
import StatusPill from '../components/StatusPill'

type LoadState = 'loading' | 'success' | 'empty' | 'error' | 'unauthorized'

export default function Patterns() {
  const [patterns, setPatterns] = useState<Pattern[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')

  useEffect(() => {
    let cancelled = false

    async function loadPatterns() {
      setLoadState('loading')
      try {
        const data = await getPatterns()
        if (cancelled) return

        setPatterns(data)
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

    loadPatterns()
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Patterns</h1>
        <p className="page-subtitle">PII detection patterns currently configured</p>
      </div>

      {loadState === 'loading' && <p className="info-message">Loading patterns...</p>}

      {loadState === 'unauthorized' && (
        <p className="info-message">
          You don't have permission to view this data. Contact your administrator if you believe this is an error.
        </p>
      )}

      {loadState === 'error' && (
        <p className="info-message">
          Unable to load patterns. Please check your connection and try again.
        </p>
      )}

      {loadState === 'empty' && <p className="info-message">No patterns configured yet.</p>}

      {loadState === 'success' && (
        <table className="data-table">
          <thead>
            <tr>
              <th scope="col">Name</th>
              <th scope="col">Category</th>
              <th scope="col">Description</th>
              <th scope="col">Status</th>
            </tr>
          </thead>
          <tbody>
            {patterns.map((pattern) => (
          <tr key={pattern.ID}>
            <td data-label="Name">{pattern.Name}</td>
            <td data-label="Category">{pattern.Category}</td>
            <td data-label="Description">{pattern.Description}</td>
            <td data-label="Status">
              <StatusPill active={pattern.IsActive} />
            </td>
          </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}