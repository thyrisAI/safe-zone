/**
 * Raw pattern shape returned by the Safe Zone backend's
 * GET /patterns endpoint.
 *
 * Field names use PascalCase, matching the backend's actual JSON
 * response (GORM's default serialization). This differs from the
 * Validator type -- we mirror the backend's inconsistency here
 * rather than normalizing it away.
 */
export interface Pattern {
  ID: number
  Name: string
  Regex: string
  Description: string
  Category: 'PII' | 'SECRET' | 'INJECTION'
  IsActive: boolean
  BlockThreshold: number | null
  AllowThreshold: number | null
}