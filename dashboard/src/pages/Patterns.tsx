import { useEffect, useState } from 'react'
import { getPatterns, setPatternActive } from '../api/patterns'
import { ApiError } from '../api/client'
import type { Pattern } from '../types/pattern'
import ConfirmDialog from '../components/ConfirmDialog'

type LoadState = 'loading' | 'success' | 'empty' | 'error' | 'unauthorized'

function sortById(items: Pattern[]): Pattern[] {
  return [...items].sort((a, b) => a.ID - b.ID)
}

export default function Patterns() {
  const [patterns, setPatterns] = useState<Pattern[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [pendingToggle, setPendingToggle] = useState<Pattern | null>(null)
  const [isUpdating, setIsUpdating] = useState(false)
  const [toggleError, setToggleError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    async function loadPatterns() {
      setLoadState('loading')
      try {
        const data = await getPatterns()
        if (cancelled) return

        setPatterns(sortById(data))
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

  async function handleConfirmToggle() {
    if (!pendingToggle) return

    setIsUpdating(true)
    setToggleError(null)
    try {
      await setPatternActive(pendingToggle.ID, !pendingToggle.IsActive)
      const data = await getPatterns()
      setPatterns(sortById(data))
      setLoadState(data.length === 0 ? 'empty' : 'success')
      setPendingToggle(null)
    } catch {
      setToggleError('Unable to update this pattern. Please try again.')
    } finally {
      setIsUpdating(false)
    }
  }

  const isDisabling = pendingToggle?.IsActive === true

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
                  <div className="toggle-cell">
                    <button
                      type="button"
                      role="switch"
                      aria-checked={pattern.IsActive}
                      aria-label={`${pattern.IsActive ? 'Disable' : 'Enable'} ${pattern.Name}`}
                      className={`toggle-switch${pattern.IsActive ? ' toggle-switch-on' : ''}`}
                      onClick={() => {
                        setPendingToggle(pattern)
                        setToggleError(null)
                      }}
                    >
                      <span className="toggle-switch-knob" />
                    </button>
                    <span className={pattern.IsActive ? 'toggle-label-on' : 'toggle-label-off'}>
                      {pattern.IsActive ? 'Enabled' : 'Disabled'}
                    </span>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {pendingToggle && (
        <ConfirmDialog
          title={isDisabling ? 'Disable pattern?' : 'Enable pattern?'}
          message={
            isDisabling
              ? 'This pattern will stop detecting matches immediately. Data it protects may pass through unmasked.'
              : 'This pattern will start detecting matches immediately.'
          }
          itemLabel={pendingToggle.Name}
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
