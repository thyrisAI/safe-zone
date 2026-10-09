import { useEffect, useState } from 'react'
import { getValidators, setValidatorActive } from '../api/validators'
import { ApiError } from '../api/client'
import type { Validator } from '../types/validator'
import ConfirmDialog from '../components/ConfirmDialog'
import StatusPill from '../components/StatusPill'
import { useAuth } from '../context/AuthContext'

type LoadState = 'loading' | 'success' | 'empty' | 'error' | 'unauthorized'

function sortById(items: Validator[]): Validator[] {
  return [...items].sort((a, b) => a.ID - b.ID)
}

export default function Guardrails() {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'

  const [validators, setValidators] = useState<Validator[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [pendingToggle, setPendingToggle] = useState<Validator | null>(null)
  const [isUpdating, setIsUpdating] = useState(false)
  const [toggleError, setToggleError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    async function loadValidators() {
      setLoadState('loading')
      try {
        const data = await getValidators()
        if (cancelled) return

        setValidators(sortById(data))
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

  async function handleConfirmToggle() {
    if (!pendingToggle) return

    setIsUpdating(true)
    setToggleError(null)
    try {
      await setValidatorActive(pendingToggle.ID, !pendingToggle.is_active)
      const data = await getValidators()
      setValidators(sortById(data))
      setLoadState(data.length === 0 ? 'empty' : 'success')
      setPendingToggle(null)
    } catch {
      setToggleError('Unable to update this guardrail. Please try again.')
    } finally {
      setIsUpdating(false)
    }
  }

  const isDisabling = pendingToggle?.is_active === true

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Guardrails</h1>
        <p className="page-subtitle">Validation rules configured for this gateway</p>
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
              <th scope="col">Status</th>
            </tr>
          </thead>
          <tbody>
            {validators.map((validator) => (
              <tr key={validator.ID}>
                <td data-label="Name">{validator.name}</td>
                <td data-label="Type">{validator.type}</td>
                <td data-label="Description">{validator.description}</td>
                <td data-label="Status">
                  {isAdmin ? (
                    <div className="toggle-cell">
                      <button
                        type="button"
                        role="switch"
                        aria-checked={validator.is_active}
                        aria-label={`${validator.is_active ? 'Disable' : 'Enable'} ${validator.name}`}
                        className={`toggle-switch${validator.is_active ? ' toggle-switch-on' : ''}`}
                        onClick={() => {
                          setPendingToggle(validator)
                          setToggleError(null)
                        }}
                      >
                        <span className="toggle-switch-knob" />
                      </button>
                      <span className={validator.is_active ? 'toggle-label-on' : 'toggle-label-off'}>
                        {validator.is_active ? 'Enabled' : 'Disabled'}
                      </span>
                    </div>
                  ) : (
                    <StatusPill active={validator.is_active} />
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {pendingToggle && (
        <ConfirmDialog
          title={isDisabling ? 'Disable guardrail?' : 'Enable guardrail?'}
          message={
            isDisabling
              ? 'Requests that use this guardrail will no longer be checked by it, starting immediately. Content it protects against may pass through.'
              : 'Requests that use this guardrail will be checked by it again, starting immediately.'
          }
          itemLabel={pendingToggle.name}
          isConfirming={isUpdating}
          confirmError={toggleError}
          confirmLabel={isDisabling ? 'Disable' : 'Enable'}
          confirmVariant={isDisabling ? 'danger' : 'success'}
          confirmingLabel={isDisabling ? 'Disabling...' : 'Enabling...'}
          onCancel={() => setPendingToggle(null)}
          onConfirm={handleConfirmToggle}
        />
      )}
    </div>
  )
}