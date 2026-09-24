import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  getAllowlist,
  getBlacklist,
  createAllowlistItem,
  createBlacklistItem,
  deleteAllowlistItem,
  deleteBlacklistItem,
} from './lists'

const mockItem = {
  ID: 1,
  CreatedAt: '2026-09-24T10:15:57.249286618Z',
  UpdatedAt: '2026-09-24T10:15:57.249286618Z',
  DeletedAt: null,
  value: 'test@example.com',
  description: 'Test entry',
}

describe('lists API', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('getAllowlist requests GET /allowlist and returns the parsed list', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify([mockItem]), { status: 200 }))

    const result = await getAllowlist()

    expect(result).toEqual([mockItem])
    expect(fetch).toHaveBeenCalledWith(expect.stringContaining('/allowlist'), expect.anything())
  })

  it('getBlacklist requests GET /blacklist and returns the parsed list', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify([mockItem]), { status: 200 }))

    const result = await getBlacklist()

    expect(result).toEqual([mockItem])
    expect(fetch).toHaveBeenCalledWith(expect.stringContaining('/blacklist'), expect.anything())
  })

  it('createAllowlistItem sends a POST with the given value and description', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify(mockItem), { status: 201 }))

    const result = await createAllowlistItem({ value: 'test@example.com', description: 'Test entry' })

    expect(result).toEqual(mockItem)
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/allowlist'),
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ value: 'test@example.com', description: 'Test entry' }),
      }),
    )
  })

  it('createBlacklistItem sends a POST to /blacklist', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify(mockItem), { status: 201 }))

    await createBlacklistItem({ value: 'bad-actor.net' })

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/blacklist'),
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('deleteAllowlistItem sends a DELETE to /allowlist/{id} and resolves without a value', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }))

    const result = await deleteAllowlistItem(1)

    expect(result).toBeUndefined()
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/allowlist/1'),
      expect.objectContaining({ method: 'DELETE' }),
    )
  })

  it('deleteBlacklistItem sends a DELETE to /blacklist/{id}', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }))

    await deleteBlacklistItem(42)

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/blacklist/42'),
      expect.objectContaining({ method: 'DELETE' }),
    )
  })

  it('createAllowlistItem propagates an ApiError when the backend rejects the request', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('Internal Server Error', { status: 500 }))

    await expect(createAllowlistItem({ value: 'duplicate@example.com' })).rejects.toMatchObject({
      status: 500,
    })
  })
})