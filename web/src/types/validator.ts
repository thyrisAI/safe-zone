/**
 * SAFE validator shape used by the dashboard.
 *
 * IMPORTANT: the backend's actual GET /validators response includes
 * a "rule" field, which for AI validators contains the full prompt
 * text (e.g. exactly how jailbreak detection works, which keywords
 * it looks for -- all in plain text).
 *
 * This field is NOT exposed in the dashboard: an attacker could read
 * the prompt and learn precisely what the system checks for, then
 * craft input to evade it.
 *
 * "rule" is therefore intentionally absent from this type. The raw
 * backend data is stripped of this field at the fetch boundary
 * (see api/validators.ts).
 */
export interface Validator {
    ID: number
    name: string
    type: 'BUILTIN' | 'REGEX' | 'SCHEMA' | 'AI_PROMPT'
    description: string
    expected_response: string
  }