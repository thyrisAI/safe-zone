import { useState, type FormEvent } from 'react'
import { changePassword } from '../api/auth'
import { ApiError } from '../api/client'
import {
  MAX_PASSWORD_LENGTH as MAX_LENGTH,
  MIN_PASSWORD_LENGTH as MIN_LENGTH,
  passwordLength,
} from '../utils/passwordPolicy'

interface ChangePasswordFormProps {
  onChanged: () => void
}

export default function ChangePasswordForm({ onChanged }: ChangePasswordFormProps) {
  const [isOpen, setIsOpen] = useState(false)
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [showNew, setShowNew] = useState(false)
  const [currentError, setCurrentError] = useState<string | null>(null)
  const [newError, setNewError] = useState<string | null>(null)
  const [formError, setFormError] = useState<string | null>(null)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [wasUpdated, setWasUpdated] = useState(false)

  const newLength = passwordLength(newPassword)
  const longEnough = newLength >= MIN_LENGTH

  function reset() {
    setCurrentPassword('')
    setNewPassword('')
    setShowNew(false)
    setCurrentError(null)
    setNewError(null)
    setFormError(null)
  }

  function open() {
    setWasUpdated(false)
    setIsOpen(true)
  }

  function cancel() {
    reset()
    setIsOpen(false)
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setCurrentError(null)
    setNewError(null)
    setFormError(null)

    if (newLength < MIN_LENGTH || newLength > MAX_LENGTH) {
      setNewError(`New password must be ${MIN_LENGTH}-${MAX_LENGTH} characters.`)
      return
    }
    if (newPassword === currentPassword) {
      setNewError('New password must be different from the current password.')
      return
    }

    setIsSubmitting(true)
    try {
      await changePassword(currentPassword, newPassword)
      reset()
      setIsOpen(false)
      setWasUpdated(true)
      onChanged()
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        setCurrentError('Current password is incorrect.')
      } else if (err instanceof ApiError && err.status === 429) {
        setFormError('Too many incorrect attempts. Please try again in a few minutes.')
      } else if (err instanceof ApiError && err.status === 400) {
        setNewError('This password does not meet the requirements.')
      } else if (err instanceof ApiError && err.status === 401) {
        setFormError('Your session has expired. Please sign in again.')
      } else {
        setFormError('Unable to change your password. Please try again.')
      }
    } finally {
      setIsSubmitting(false)
    }
  }

  if (!isOpen) {
    return (
      <div className="password-collapsed">
        <button type="button" className="button-secondary" onClick={open}>
          Change password
        </button>
        {wasUpdated && (
          <span className="toggle-label-on" role="status">
            Password updated
          </span>
        )}
      </div>
    )
  }

  return (
    <form className="password-form" onSubmit={handleSubmit}>
      <div className="form-field">
        <label htmlFor="current-password">Current password</label>
        <input
          id="current-password"
          type="password"
          required
          autoFocus
          autoComplete="current-password"
          value={currentPassword}
          onChange={(e) => setCurrentPassword(e.target.value)}
          aria-invalid={currentError !== null}
          aria-describedby={currentError ? 'current-password-error' : undefined}
        />
        {currentError && (
          <p className="field-error" id="current-password-error" role="alert">
            {currentError}
          </p>
        )}
      </div>

      <div className="form-field">
        <label htmlFor="new-password">New password</label>
        <div className="password-input-row">
          <input
            id="new-password"
            type={showNew ? 'text' : 'password'}
            required
            autoComplete="new-password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            aria-invalid={newError !== null}
            aria-describedby="new-password-hint"
          />
          <button
            type="button"
            className="button-secondary"
            aria-pressed={showNew}
            onClick={() => setShowNew((v) => !v)}
          >
            {showNew ? 'Hide' : 'Show'}
          </button>
        </div>
        <p
          id="new-password-hint"
          className={longEnough ? 'requirement requirement-met' : 'requirement'}
        >
          {longEnough ? '\u2713 ' : ''}At least {MIN_LENGTH} characters
        </p>
        {newError && (
          <p className="field-error" role="alert">
            {newError}
          </p>
        )}
      </div>

      {formError && (
        <p className="field-error" role="alert">
          {formError}
        </p>
      )}

      <div className="password-form-actions">
        <button type="button" className="button-secondary" onClick={cancel} disabled={isSubmitting}>
          Cancel
        </button>
        <button type="submit" className="button-primary" disabled={isSubmitting}>
          {isSubmitting ? 'Saving...' : 'Update password'}
        </button>
      </div>
    </form>
  )
}
