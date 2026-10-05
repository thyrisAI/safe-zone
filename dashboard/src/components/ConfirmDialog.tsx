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
} from '@thyris/ui'

interface ConfirmDialogProps {
  title: string
  message: string
  itemLabel?: string
  isConfirming: boolean
  confirmError: string | null
  confirmLabel?: string
  confirmingLabel?: string
  confirmVariant?: 'danger' | 'success'
  onCancel: () => void
  onConfirm: () => void
}

export default function ConfirmDialog({
  title,
  message,
  itemLabel,
  isConfirming,
  confirmError,
  confirmLabel = 'Delete',
  confirmingLabel = 'Deleting...',
  confirmVariant = 'danger',
  onCancel,
  onConfirm,
}: ConfirmDialogProps) {
  return (
    <Dialog open onOpenChange={(open) => !open && !isConfirming && onCancel()}>
      <DialogContent
        onEscapeKeyDown={(event) => isConfirming && event.preventDefault()}
        onInteractOutside={(event) => isConfirming && event.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{message}</DialogDescription>
        </DialogHeader>

        {itemLabel && (
          <p className="rounded-md bg-muted px-3 py-2 font-mono text-sm font-semibold break-all">
            {itemLabel}
          </p>
        )}

        {confirmError && (
          <Alert variant="destructive">
            <AlertDescription>{confirmError}</AlertDescription>
          </Alert>
        )}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={onCancel} disabled={isConfirming}>
            Cancel
          </Button>
          <Button
            type="button"
            variant={confirmVariant === 'danger' ? 'destructive' : 'default'}
            onClick={onConfirm}
            disabled={isConfirming}
          >
            {isConfirming ? confirmingLabel : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
