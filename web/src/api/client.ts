/**
 * Shared API client. Single entry point for all backend requests.
 *
 * Centralizing base URL, headers, and error handling here means an
 * endpoint change only touches this file (or the relevant api/*.ts
 * file), never the page components.
 */

// Resolved via the Vite proxy, so the real backend address
// (localhost:8080) never appears in frontend code.
const BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '/api'

// Default timeout to prevent a request from hanging indefinitely.
const DEFAULT_TIMEOUT_MS = 8000

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

/**
 * For JSON-returning endpoints (e.g. /patterns, /validators).
 * T is the expected response type, e.g. request<Pattern[]>('/patterns').
 */
export async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), DEFAULT_TIMEOUT_MS)

  try {
    const response = await fetch(`${BASE_URL}${path}`, {
      ...options,
      signal: controller.signal,
      headers: {
        'Content-Type': 'application/json',
        ...options?.headers,
      },
    })

    if (!response.ok) {
      // Never surface the raw backend error to the user.
      throw new ApiError(`Request failed with status ${response.status}`, response.status)
    }

    return (await response.json()) as T
  } catch (err) {
    if (err instanceof ApiError) throw err
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw new ApiError('Request timed out', 0)
    }
    throw new ApiError('Network error, please check your connection', 0)
  } finally {
    clearTimeout(timeoutId)
  }
}

/**
 * For plain-text endpoints (e.g. /healthz -> "UP", /ready -> "READY").
 * These do not return JSON, so no parsing is attempted.
 */
export async function requestText(path: string): Promise<{ text: string; ok: boolean }> {
  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), DEFAULT_TIMEOUT_MS)

  try {
    const response = await fetch(`${BASE_URL}${path}`, { signal: controller.signal })
    const text = await response.text()
    return { text, ok: response.ok }
  } catch {
    // Return an "unreachable" result instead of throwing, so the
    // Overview screen can render it as "Unreachable".
    return { text: '', ok: false }
  } finally {
    clearTimeout(timeoutId)
  }
}