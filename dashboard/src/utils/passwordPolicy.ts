// Mirrors the backend policy (internal/auth/password_policy.go). The
// backend stays the authority; this only gives feedback before sending.
export const MIN_PASSWORD_LENGTH = 8
export const MAX_PASSWORD_LENGTH = 128

/** Length in characters (not UTF-16 units), the way the backend counts. */
export function passwordLength(value: string): number {
  return Array.from(value).length
}
