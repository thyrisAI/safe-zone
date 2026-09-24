import { useEffect, useState } from 'react'
import { getValidators } from '../api/validators'
import { ApiError } from '../api/client'
import type { Validator } from '../types/validator'

type LoadState = 'loading' | 'success' | 'empty' | 'error' | 'unauthorized'

export default function Guardrails() {
  const [validators, setValidators] = useState<Validator[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')

  useEffect(() => {
    let cancelled = false

    async function loadValidators() {
      setLoadState('loading')
      try {
        const data = await getValidators()
        if (cancelled) return

        setValidators(data)
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

    loadValidators()
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Guardrails</h1>
        <p className="page-subtitle">AI-based validation rules currently active</p>
      </div>

      {loadState === 'loading' && <p className="info-message">Loading guardrails...</p>}

      {loadState === 'unauthorized' && (
        <p className="info-message">
          You don't have permission to view this data. Contact your administrator if you believe this is an error.
        </p>
      )}

      {loadState === 'error' && (
        <p className="info-message">
          Unable to load guardrails. Please check your connection and try again.
        </p>
      )}

      {loadState === 'empty' && <p className="info-message">No guardrails configured yet.</p>}

      {loadState === 'success' && (
        <table className="data-table">
          <thead>
            <tr>
              <th scope="col">Name</th>
              <th scope="col">Type</th>
              <th scope="col">Description</th>
            </tr>
          </thead>
          <tbody>
            {validators.map((validator) => (
              <tr key={validator.ID}>
                <td data-label="Name">{validator.name}</td>
                <td data-label="Type">{validator.type}</td>
                <td data-label="Description">{validator.description}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}