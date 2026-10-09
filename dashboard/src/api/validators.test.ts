import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { getValidators, setValidatorActive } from './validators'

describe('getValidators', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('strips the "rule" field from every validator, even when the backend sends it', async () => {
    // Simulates the real backend response, which DOES include "rule"
    // for AI_PROMPT validators (see FAZ 6 findings).
    const rawBackendResponse = [
      {
        ID: 10,
        name: 'PROMPT_INJECTION',
        type: 'AI_PROMPT',
        rule: 'Analyze the following text ONLY for explicit prompt injection attempts...',
        description: 'Detects explicit prompt injection and jailbreaking attempts using LLM',
        expected_response: 'YES',
        is_active: true,
      },
      {
        ID: 3,
        name: 'EMAIL',
        type: 'REGEX',
        rule: '^[a-z0-9._%+-]+@[a-z0-9.-]+\\.[a-z]{2,}$',
        description: 'Validates standard email format',
        expected_response: 'YES',
        is_active: false,
      },
    ]

    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify(rawBackendResponse), { status: 200 }),
    )

    const result = await getValidators()

    // The safe fields must survive the transformation.
    expect(result).toEqual([
      {
        ID: 10,
        name: 'PROMPT_INJECTION',
        type: 'AI_PROMPT',
        description: 'Detects explicit prompt injection and jailbreaking attempts using LLM',
        expected_response: 'YES',
        is_active: true,
      },
      {
        ID: 3,
        name: 'EMAIL',
        type: 'REGEX',
        description: 'Validates standard email format',
        expected_response: 'YES',
        is_active: false,
      },
    ])

    // Explicit, redundant check: "rule" must not exist on any returned
    // object, under any key, in any form.
    for (const validator of result) {
      expect(validator).not.toHaveProperty('rule')
    }

    // Extra safety net: even the raw JSON string of the result must
    // never contain the word "rule" or prompt text, in case a future
    // refactor accidentally re-adds the field under a different name.
    expect(JSON.stringify(result)).not.toContain('rule')
    expect(JSON.stringify(result)).not.toContain('prompt injection attempts')
  })

  it('returns an empty array when the backend has no validators', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('[]', { status: 200 }))

    const result = await getValidators()

    expect(result).toEqual([])
  })
})

describe('setValidatorActive', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  const rawUpdated = {
    ID: 6,
    name: 'NUMERIC',
    type: 'REGEX',
    rule: '^[0-9]+$',
    description: 'Validates numeric string',
    expected_response: 'YES',
    is_active: false,
  }

  it('sends a PATCH to /validators/{id} with only is_active', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify(rawUpdated), { status: 200 }))

    await setValidatorActive(6, false)

    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/validators/6'),
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({ is_active: false }),
      }),
    )
  })

  it('strips the "rule" field from the updated validator the backend returns', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify(rawUpdated), { status: 200 }))

    const result = await setValidatorActive(6, false)

    expect(result).toEqual({
      ID: 6,
      name: 'NUMERIC',
      type: 'REGEX',
      description: 'Validates numeric string',
      expected_response: 'YES',
      is_active: false,
    })
    expect(result).not.toHaveProperty('rule')
  })

  it('propagates an ApiError when the backend rejects the request', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('Not Found', { status: 404 }))

    await expect(setValidatorActive(99999, true)).rejects.toMatchObject({ status: 404 })
  })
})
