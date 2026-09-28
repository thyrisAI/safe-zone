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
    <div role="dialog" aria-modal="true" aria-label={title} className="modal-overlay">
      <div className="modal">
        <div className="modal-header">
          <h2 className="modal-title">{title}</h2>
          <button type="button" className="modal-close" aria-label="Close" onClick={onCancel}>
            ×
          </button>
        </div>

        <p>{message}</p>

        {itemLabel && <p className="confirm-item-label">{itemLabel}</p>}

        {confirmError && <p className="field-error">{confirmError}</p>}

        <div className="modal-footer">
          <button type="button" className="button-secondary" onClick={onCancel} disabled={isConfirming}>
            Cancel
          </button>
          <button
            type="button"
            className={`button-${confirmVariant}`}
            onClick={onConfirm}
            disabled={isConfirming}
          >
            {isConfirming ? confirmingLabel : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
