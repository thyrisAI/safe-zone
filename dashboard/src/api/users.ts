import { request } from './client'
import type { CreateUserInput, DashboardUser } from '../types/users'

/** Admin-only: list all dashboard accounts. */
export async function listUsers(): Promise<DashboardUser[]> {
  return request<DashboardUser[]>('/users')
}

/** Admin-only: create a new dashboard account. */
export async function createUser(input: CreateUserInput): Promise<{ email: string; role: string }> {
  return request<{ email: string; role: string }>('/users', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}