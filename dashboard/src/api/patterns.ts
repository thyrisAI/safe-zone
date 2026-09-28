import { request } from './client'
import type { Pattern } from '../types/pattern'

export async function getPatterns(): Promise<Pattern[]> {
  return request<Pattern[]>('/patterns')
}

export async function setPatternActive(id: number, isActive: boolean): Promise<Pattern> {
  return request<Pattern>(`/patterns/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ is_active: isActive }),
  })
}