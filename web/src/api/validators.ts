import { request } from './client'
import type { Validator } from '../types/validator'

/**
 * Raw shape of the /validators response, INCLUDING the "rule" field.
 * Used only within this file, never exported.
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
 * SAFE validator list for dashboard use. The "rule" field (the full
 * AI prompt text) is stripped out here, right at the fetch boundary,
 * so it never reaches page components. See src/types/validator.ts
 * for the security rationale.
 */
export async function getValidators(): Promise<Validator[]> {
  const raw = await request<RawValidator[]>('/validators')

  return raw.map((v) => ({
    ID: v.ID,
    name: v.name,
    type: v.type as Validator['type'],
    description: v.description,
    expected_response: v.expected_response,
    // "rule" intentionally omitted.
  }))
}