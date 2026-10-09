export type UserRole = 'admin' | 'viewer'

export interface DashboardUser {
  ID: number
  email: string
  role: UserRole
  is_active: boolean
  CreatedAt: string
}

export interface CreateUserInput {
  email: string
  password: string
  role: UserRole
}