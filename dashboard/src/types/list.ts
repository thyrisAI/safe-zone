/**
 * Raw shape returned by the Safe Zone backend's GET /allowlist and
 * GET /blacklist endpoints.
 *
 * Field names mix casing: gorm.Model's automatic fields (ID, CreatedAt,
 * UpdatedAt, DeletedAt) are PascalCase, while the hand-written fields
 * (value, description) are lowercase per their json tags. This mirrors
 * the backend's actual response -- verified via curl against a running
 * instance, not assumed from the Go struct alone.
 */
export interface ListItem {
  ID: number
  CreatedAt: string
  UpdatedAt: string
  DeletedAt: string | null
  value: string
  description: string
}

/**
 * Which list a ListItem belongs to. Used to pick the right API
 * endpoint and copy (e.g. "Add Allowlist Entry" vs "Add Blacklist Entry").
 */
export type ListKind = 'allowlist' | 'blacklist'