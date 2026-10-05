import type { SystemStatus } from '../api/health'
import { Badge } from '@thyris/ui'
import './StatusBadge.css'

interface StatusBadgeProps {
  status: SystemStatus | 'loading'
}

const statusConfig: Record<StatusBadgeProps['status'], { label: string; className: string }> = {
  operational: { label: 'Operational', className: 'status-badge status-badge-success' },
  degraded: { label: 'Degraded', className: 'status-badge status-badge-warning' },
  unreachable: { label: 'Unreachable', className: 'status-badge status-badge-danger' },
  loading: { label: 'Checking...', className: 'status-badge status-badge-neutral' },
}

export default function StatusBadge({ status }: StatusBadgeProps) {
  const config = statusConfig[status]

  return (
    <Badge variant="outline" className={config.className}>
      <span className="status-badge-dot" aria-hidden="true" />
      {config.label}
    </Badge>
  )
}
