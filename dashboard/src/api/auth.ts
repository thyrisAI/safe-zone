import { request, ApiError } from './client'
import type { User } from '../types/auth'

export async function login(email: string, password: string): Promise<User> {
  return request<User>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })
}

export async function logout(): Promise<void> {
  await request<void>('/auth/logout', { method: 'POST' })
}

/**
 * Returns the logged-in user, or null if there is no active session.
 * A 401 here is an expected "not logged in" state, not a real error --
 * every other page treats it as unauthorized, but this one is the check
 * itself, so it's handled here instead of bubbling up.
 */
export async function getMe(): Promise<User | null> {
  try {
    return await request<User>('/auth/me')
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      return null
    }
    throw err
  }
}