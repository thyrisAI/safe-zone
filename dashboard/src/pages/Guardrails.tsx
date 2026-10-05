import { useEffect, useState } from 'react'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@thyris/ui'
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
        <Table className="data-table">
          <TableHeader>
            <TableRow>
              <TableHead scope="col">Name</TableHead>
              <TableHead scope="col">Type</TableHead>
              <TableHead scope="col">Description</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {validators.map((validator) => (
              <TableRow key={validator.ID}>
                <TableCell data-label="Name">{validator.name}</TableCell>
                <TableCell data-label="Type">{validator.type}</TableCell>
                <TableCell data-label="Description">{validator.description}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}
