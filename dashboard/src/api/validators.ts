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
  is_active: boolean
}

/**
 * Converts a raw backend validator into the SAFE dashboard shape. The
 * "rule" field (the full AI prompt text) is dropped here, at the fetch
 * boundary, so it never reaches page components. See
 * src/types/validator.ts for the security rationale. Every response that
 * carries a validator must go through this function.
 */
function toValidator(v: RawValidator): Validator {
  return {
    ID: v.ID,
    name: v.name,
    type: v.type as Validator['type'],
    description: v.description,
    expected_response: v.expected_response,
    is_active: v.is_active,
    // "rule" intentionally omitted.
  }
}

export async function getValidators(): Promise<Validator[]> {
  const raw = await request<RawValidator[]>('/validators')
  return raw.map(toValidator)
}

/**
 * Switches a guardrail on or off without deleting it. The backend answers
 * with the full validator including "rule", so the answer is filtered the
 * same way as the list.
 */
export async function setValidatorActive(id: number, isActive: boolean): Promise<Validator> {
  const raw = await request<RawValidator>(`/validators/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ is_active: isActive }),
  })
  return toValidator(raw)
}
