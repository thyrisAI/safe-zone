import { request } from './client'
import type { ListItem } from '../types/list'

export async function getAllowlist(): Promise<ListItem[]> {
  return request<ListItem[]>('/allowlist')
}

export async function getBlacklist(): Promise<ListItem[]> {
  return request<ListItem[]>('/blacklist')
}

interface CreateListItemInput {
  value: string
  description?: string
}

export async function createAllowlistItem(input: CreateListItemInput): Promise<ListItem> {
  return request<ListItem>('/allowlist', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function createBlacklistItem(input: CreateListItemInput): Promise<ListItem> {
  return request<ListItem>('/blacklist', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function deleteAllowlistItem(id: number): Promise<void> {
  await request<void>(`/allowlist/${id}`, { method: 'DELETE' })
}

export async function deleteBlacklistItem(id: number): Promise<void> {
  await request<void>(`/blacklist/${id}`, { method: 'DELETE' })
}