import { request } from './client'
import type { Pattern } from '../types/pattern'

export async function getPatterns(): Promise<Pattern[]> {
  return request<Pattern[]>('/patterns')
}