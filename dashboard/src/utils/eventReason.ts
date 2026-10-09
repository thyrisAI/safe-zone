import type { DashboardEvent } from '../types/dashboard'

// Backend sends short codes; show a readable label instead.
export function reasonLabel(event: DashboardEvent): string {
  switch (event.reason) {
    case 'PII':
      return 'PII detected'
    case 'GUARDRAIL':
      return 'Guardrail check failed'
    case 'RULE':
      return event.blocked ? 'Blocked by rule' : 'No findings'
  }
}