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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@thyris/ui'
import { ApiError } from '../api/client'
import type { CreateUserInput, UserRole } from '../types/users'
import { MAX_PASSWORD_LENGTH, MIN_PASSWORD_LENGTH, passwordLength } from '../utils/passwordPolicy'

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

  const longEnough = passwordLength(password) >= MIN_PASSWORD_LENGTH

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)

    const length = passwordLength(password)
    if (length < MIN_PASSWORD_LENGTH || length > MAX_PASSWORD_LENGTH) {
      setError(`Password must be ${MIN_PASSWORD_LENGTH}-${MAX_PASSWORD_LENGTH} characters.`)
      return
    }

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
    <Dialog open onOpenChange={(open) => !open && !isSubmitting && onCancel()}>
      <DialogContent
        onEscapeKeyDown={(event) => isSubmitting && event.preventDefault()}
        onInteractOutside={(event) => isSubmitting && event.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle>Add User</DialogTitle>
          <DialogDescription>
            Create a dashboard account. Viewers are read-only, admins have full access.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit}>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="user-email">
                Email <span className="text-destructive">*</span>
              </FieldLabel>
              <Input
                id="user-email"
                type="email"
                required
                autoFocus
                autoComplete="off"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="name@example.com"
              />
            </Field>

            <Field>
              <FieldLabel htmlFor="user-password">
                Password <span className="text-destructive">*</span>
              </FieldLabel>
              <Input
                id="user-password"
                type="password"
                required
                autoComplete="new-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                aria-describedby="user-password-hint"
              />
              <p
                id="user-password-hint"
                className={longEnough ? 'text-sm text-[var(--color-success)]' : 'text-sm text-muted-foreground'}
              >
                {longEnough ? '✓ ' : ''}At least {MIN_PASSWORD_LENGTH} characters
              </p>
            </Field>

            <Field>
              <FieldLabel htmlFor="user-role">Role</FieldLabel>
              <Select value={role} onValueChange={(value) => setRole(value as UserRole)}>
                <SelectTrigger id="user-role">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="viewer">Viewer (read-only)</SelectItem>
                  <SelectItem value="admin">Admin (full access)</SelectItem>
                </SelectContent>
              </Select>
            </Field>

            {error && (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
          </FieldGroup>

          <DialogFooter className="mt-6">
            <Button type="button" variant="outline" onClick={onCancel} disabled={isSubmitting}>
              Cancel
            </Button>
            <Button type="submit" disabled={isSubmitting}>
              {isSubmitting ? 'Creating...' : 'Create User'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
