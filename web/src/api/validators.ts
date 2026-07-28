import { request } from './client'
import type { Validator } from '../types/validator'

/**
 * Backend'in ham /validators response'u -- "rule" alanını İÇERİR.
 * Bu tip SADECE bu dosyanın içinde kullanılır, dışarı asla export edilmez.
 */
interface RawValidator {
  ID: number
  name: string
  type: string
  rule: string
  description: string
  expected_response: string
}

/**
 * Dashboard'un kullanacağı GÜVENLİ validator listesi.
 * "rule" alanını (tam AI prompt metnini) burada, backend'den geldiği
 * anda filtreleyip atıyoruz -- bu veri sayfalara (pages/) hiç ulaşmıyor.
 * Gerekçe: FAZ 6 ve FAZ 17 kararı, bkz. src/types/validator.ts yorumu.
 */
export async function getValidators(): Promise<Validator[]> {
  const raw = await request<RawValidator[]>('/validators')

  return raw.map((v) => ({
    ID: v.ID,
    name: v.name,
    type: v.type as Validator['type'],
    description: v.description,
    expected_response: v.expected_response,
    // "rule" bilinçli olarak dahil edilmiyor.
  }))
}