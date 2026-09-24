import './StatusBadge.css'

interface StatusPillProps {
  active: boolean
  activeLabel?: string
  inactiveLabel?: string
}

export default function StatusPill({
  active,
  activeLabel = 'Enabled',
  inactiveLabel = 'Disabled',
}: StatusPillProps) {
  const className = active
    ? 'status-badge status-badge-success'
    : 'status-badge status-badge-neutral'

  return (
    <span className={className}>
      <span className="status-badge-dot" aria-hidden="true" />
      {active ? activeLabel : inactiveLabel}
    </span>
  )
}