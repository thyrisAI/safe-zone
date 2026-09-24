import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { request, requestText, ApiError } from './client'

describe('request', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns parsed JSON on a successful response', async () => {
    const mockData = { name: 'EMAIL', category: 'PII' }
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify(mockData), { status: 200 }),
    )

    const result = await request<typeof mockData>('/patterns')

    expect(result).toEqual(mockData)
  })

  it('throws an ApiError with the response status on a non-2xx response', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response('Internal Server Error', { status: 500 }),
    )

    await expect(request('/patterns')).rejects.toMatchObject({
      name: 'ApiError',
      status: 500,
    })
  })

  it('throws an ApiError on a network failure', async () => {
    vi.mocked(fetch).mockRejectedValueOnce(new TypeError('Failed to fetch'))

    await expect(request('/patterns')).rejects.toBeInstanceOf(ApiError)
  })
})

describe('requestText', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns ok:true with the body text on success', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('UP', { status: 200 }))

    const result = await requestText('/healthz')

    expect(result).toEqual({ text: 'UP', ok: true })
  })

  it('returns ok:false instead of throwing when the request fails', async () => {
    vi.mocked(fetch).mockRejectedValueOnce(new TypeError('Failed to fetch'))

    const result = await requestText('/healthz')

    expect(result).toEqual({ text: '', ok: false })
  })
})