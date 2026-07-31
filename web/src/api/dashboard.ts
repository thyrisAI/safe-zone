import { request } from './client'
import type { DashboardSummary, DashboardEvent, DashboardConfig } from '../types/dashboard'

export async function getDashboardSummary(): Promise<DashboardSummary> {
  return request<DashboardSummary>('/dashboard/summary')
}

export async function getDashboardEvents(limit = 20): Promise<DashboardEvent[]> {
  return request<DashboardEvent[]>(`/dashboard/events?limit=${limit}`)
}

export async function getDashboardConfig(): Promise<DashboardConfig> {
  return request<DashboardConfig>('/dashboard/config')
}