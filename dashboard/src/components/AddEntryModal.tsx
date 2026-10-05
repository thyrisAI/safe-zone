import { useState } from 'react'
import type { FormEvent } from 'react'
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
    <Dialog open onOpenChange={(open) => !open && submitState !== 'submitting' && onCancel()}>
      <DialogContent
        onEscapeKeyDown={(event) => submitState === 'submitting' && event.preventDefault()}
        onInteractOutside={(event) => submitState === 'submitting' && event.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            Add a value and an optional description to the {listKind}.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="entry-value">
                Value <span className="text-destructive">*</span>
              </FieldLabel>
              <Input
                id="entry-value"
                type="text"
                value={value}
                onChange={(e) => setValue(e.target.value)}
                placeholder="e.g. user@example.com or 192.168.1.1"
                aria-invalid={Boolean(validationError)}
                aria-describedby={validationError ? 'entry-value-error' : undefined}
                autoFocus
              />
              {validationError && (
                <p id="entry-value-error" className="text-sm text-destructive">
                  {validationError}
                </p>
              )}
            </Field>

            <Field>
              <FieldLabel htmlFor="entry-description">Description (optional)</FieldLabel>
              <Input
                id="entry-description"
                type="text"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Brief note about this entry"
              />
            </Field>

            {submitState === 'error' && submitError && (
              <Alert variant="destructive">
                <AlertDescription>{submitError}</AlertDescription>
              </Alert>
            )}
          </FieldGroup>

          <DialogFooter className="mt-6">
            <Button type="button" variant="outline" onClick={onCancel} disabled={submitState === 'submitting'}>
              Cancel
            </Button>
            <Button type="submit" disabled={submitState === 'submitting'}>
              {submitState === 'submitting' ? 'Adding...' : 'Add Entry'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
