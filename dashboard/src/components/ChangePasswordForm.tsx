import { useState, type FormEvent } from 'react'
import {
  Alert,
  AlertDescription,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Field,
  FieldGroup,
  FieldLabel,
  Input,
} from '@thyris/ui'
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
    if (isSubmitting) return
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

  return (
    <>
      <div className="password-collapsed">
        <Button type="button" variant="outline" size="sm" onClick={open}>
          Change password
        </Button>
        {wasUpdated && (
          <span className="toggle-label-on" role="status">
            Password updated. Your other sessions were signed out.
          </span>
        )}
      </div>

      {isOpen && (
        <Dialog open onOpenChange={(next) => !next && cancel()}>
          <DialogContent
            onEscapeKeyDown={(event) => isSubmitting && event.preventDefault()}
            onInteractOutside={(event) => isSubmitting && event.preventDefault()}
          >
            <DialogHeader>
              <DialogTitle>Change password</DialogTitle>
              <DialogDescription>
                Choose a new password. Your other sessions on other browsers and devices will be
                signed out.
              </DialogDescription>
            </DialogHeader>

            <form onSubmit={handleSubmit}>
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="current-password">Current password</FieldLabel>
                  <Input
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
                    <p id="current-password-error" className="text-sm text-destructive" role="alert">
                      {currentError}
                    </p>
                  )}
                </Field>

                <Field>
                  <FieldLabel htmlFor="new-password">New password</FieldLabel>
                  <div className="flex gap-2">
                    <Input
                      id="new-password"
                      type={showNew ? 'text' : 'password'}
                      required
                      autoComplete="new-password"
                      value={newPassword}
                      onChange={(e) => setNewPassword(e.target.value)}
                      aria-invalid={newError !== null}
                      aria-describedby="new-password-hint"
                    />
                    <Button
                      type="button"
                      variant="outline"
                      aria-pressed={showNew}
                      onClick={() => setShowNew((v) => !v)}
                    >
                      {showNew ? 'Hide' : 'Show'}
                    </Button>
                  </div>
                  <p
                    id="new-password-hint"
                    className={
                      longEnough ? 'text-sm text-[var(--color-success)]' : 'text-sm text-muted-foreground'
                    }
                  >
                    {longEnough ? '✓ ' : ''}At least {MIN_LENGTH} characters
                  </p>
                  {newError && (
                    <p className="text-sm text-destructive" role="alert">
                      {newError}
                    </p>
                  )}
                </Field>

                {formError && (
                  <Alert variant="destructive">
                    <AlertDescription>{formError}</AlertDescription>
                  </Alert>
                )}
              </FieldGroup>

              <DialogFooter className="mt-6">
                <Button type="button" variant="outline" onClick={cancel} disabled={isSubmitting}>
                  Cancel
                </Button>
                <Button type="submit" disabled={isSubmitting}>
                  {isSubmitting ? 'Saving...' : 'Update password'}
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}
