import { useState, type FormEvent } from 'react'
import { ApiError } from '../api/client'
import type { CreateUserInput, UserRole } from '../types/users'

interface AddUserModalProps {
  onCancel: () => void
  onSubmit: (input: CreateUserInput) => Promise<void>
}

export default function AddUserModal({ onCancel, onSubmit }: AddUserModalProps) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<UserRole>('viewer')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setIsSubmitting(true)
    try {
      await onSubmit({ email: email.trim(), password, role })
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setError('A user with this email already exists.')
      } else if (err instanceof ApiError && err.status === 400) {
        setError('Please check the email, password and role.')
      } else if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
        setError("You don't have permission to create users.")
      } else {
        setError('Unable to create this user. Please try again.')
      }
      setIsSubmitting(false)
    }
  }

  return (
    <div className="modal-overlay" role="presentation">
      <div className="modal" role="dialog" aria-modal="true" aria-labelledby="add-user-title">
        <div className="modal-header">
          <h2 className="modal-title" id="add-user-title">
            Add User
          </h2>
          <button type="button" className="modal-close" aria-label="Close" onClick={onCancel}>
            ×
          </button>
        </div>

        <form onSubmit={handleSubmit}>
          <div className="form-field">
            <label htmlFor="user-email">
              Email <span className="required-mark">*</span>
            </label>
            <input
              id="user-email"
              type="email"
              required
              autoComplete="off"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </div>

          <div className="form-field">
            <label htmlFor="user-password">
              Password <span className="required-mark">*</span>
            </label>
            <input
              id="user-password"
              type="password"
              required
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>

          <div className="form-field">
            <label htmlFor="user-role">Role</label>
            <select
              id="user-role"
              value={role}
              onChange={(e) => setRole(e.target.value as UserRole)}
            >
              <option value="viewer">Viewer (read-only)</option>
              <option value="admin">Admin (full access)</option>
            </select>
          </div>

          {error && (
            <p className="field-error" role="alert">
              {error}
            </p>
          )}

          <div className="modal-footer">
            <button type="button" className="button-secondary" onClick={onCancel} disabled={isSubmitting}>
              Cancel
            </button>
            <button type="submit" className="button-primary" disabled={isSubmitting}>
              {isSubmitting ? 'Creating...' : 'Create User'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}