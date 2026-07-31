import { requestText } from './client'

export type SystemStatus = 'operational' | 'degraded' | 'unreachable'

/**
 * Checks healthz and ready together to derive a single system status,
 * rendered in the Overview screen as a green/amber/red indicator.
 */
export async function getSystemStatus(): Promise<SystemStatus> {
  const health = await requestText('/healthz')

  if (!health.ok) {
    return 'unreachable'
  }

  const ready = await requestText('/ready')

  if (!ready.ok) {
    // Service is up (healthz passed) but not yet connected to DB/Redis.
    return 'degraded'
  }

  return 'operational'
}