import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { getPatterns, setPatternActive } from './patterns'

const mockPattern = {
  ID: 5,
  Name: 'EMAIL',
  Regex: '.*',
  Description: 'Email address detection',
  Category: 'PII',
  IsActive: false,
  BlockThreshold: null,
  AllowThreshold: null,
}

describe('patterns API', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('getPatterns requests /patterns and returns the parsed list', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify([mockPattern]), { status: 200 }))

    const result = await getPatterns()

    expect(result).toEqual([mockPattern])
    expect(fetch).toHaveBeenCalledWith(expect.stringContaining('/patterns'), expect.anything())
  })

  it('setPatternActive sends a PATCH to /patterns/{id} with only is_active', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify(mockPattern), { status: 200 }))

    await setPatternActive(5, false)

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/patterns/5'),
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({ is_active: false }),
      }),
    )
  })

  it('setPatternActive propagates an ApiError when the backend rejects the request', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('Not Found', { status: 404 }))

    await expect(setPatternActive(99999, true)).rejects.toMatchObject({ status: 404 })
  })
})
