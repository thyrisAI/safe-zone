import { useState } from 'react'
import type { FormEvent } from 'react'
import type { ListKind } from '../types/list'

interface AddEntryModalProps {
  listKind: ListKind
  onCancel: () => void
  onSubmit: (value: string, description: string) => Promise<void>
}

export default function AddEntryModal({ listKind, onCancel, onSubmit }: AddEntryModalProps) {
  const [value, setValue] = useState('')
  const [description, setDescription] = useState('')
  const [validationError, setValidationError] = useState<string | null>(null)
  const [submitState, setSubmitState] = useState<'idle' | 'submitting' | 'error'>('idle')
  const [submitError, setSubmitError] = useState<string | null>(null)

  const title = listKind === 'allowlist' ? 'Add Allowlist Entry' : 'Add Blacklist Entry'

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()

    const trimmedValue = value.trim()
    if (!trimmedValue) {
      setValidationError('Value is required')
      return
    }
    setValidationError(null)
    setSubmitState('submitting')
    setSubmitError(null)

    try {
      await onSubmit(trimmedValue, description.trim())
      // Success: parent (Lists.tsx) closes the modal and refreshes the list.
    } catch {
      setSubmitState('error')
      setSubmitError('Unable to add this entry. It may already exist in the list.')
    }
  }

  return (
    <div role="dialog" aria-modal="true" aria-label={title} className="modal-overlay">
      <div className="modal">
        <div className="modal-header">
          <h2 className="modal-title">{title}</h2>
          <button type="button" className="modal-close" aria-label="Close" onClick={onCancel}>
            ×
          </button>
        </div>

        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label htmlFor="entry-value">
              Value <span className="required-mark">*</span>
            </label>
            <input
              id="entry-value"
              type="text"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder="e.g. user@example.com or 192.168.1.1"
              autoFocus
            />
            {validationError && <p className="field-error">{validationError}</p>}
          </div>

          <div className="form-field">
            <label htmlFor="entry-description">Description (optional)</label>
            <input
              id="entry-description"
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Brief note about this entry"
            />
          </div>

          {submitState === 'error' && submitError && <p className="field-error">{submitError}</p>}

          <div className="modal-footer">
            <button type="button" className="button-secondary" onClick={onCancel} disabled={submitState === 'submitting'}>
              Cancel
            </button>
            <button type="submit" className="button-primary" disabled={submitState === 'submitting'}>
              {submitState === 'submitting' ? 'Adding...' : 'Add Entry'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}