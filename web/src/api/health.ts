import { requestText } from './client'

export type SystemStatus = 'operational' | 'degraded' | 'unreachable'

/**
 * healthz ve ready'yi birlikte kontrol edip tek bir sistem durumu üretir.
 * FAZ 15'te Overview ekranında bu duruma göre yeşil/amber/kırmızı gösterge çizeceğiz.
 */
export async function getSystemStatus(): Promise<SystemStatus> {
  const health = await requestText('/healthz')

  if (!health.ok) {
    return 'unreachable'
  }

  const ready = await requestText('/ready')

  if (!ready.ok) {
    // Servis ayakta (healthz geçti) ama DB/Redis'e henüz bağlanamamış.
    return 'degraded'
  }

  return 'operational'
}