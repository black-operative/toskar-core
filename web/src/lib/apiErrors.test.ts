import { afterEach, describe, expect, it, vi } from 'vitest'
import { applyLanguage } from '@/i18n'
import { ApiError, api, errorText, streamChat } from './api'

// Errors carry a stable code from the service, and the UI shows the code's
// text from errors.json in the App language (multilingual spec §10).

afterEach(async () => {
  vi.unstubAllGlobals()
  await applyLanguage('en')
  localStorage.clear()
})

function errorResponse(status: number, error: object) {
  return vi.fn().mockResolvedValue(new Response(JSON.stringify({ error }), { status, headers: { 'Content-Type': 'application/json' } }))
}

describe('errorText', () => {
  it('shows the code’s text with the values in details', () => {
    expect(errorText('MODEL_NOT_INSTALLED', 'model "qwen3" not installed', { model_id: 'qwen3' })).toBe("qwen3 isn't installed.")
    expect(errorText('INVALID_SETTING', 'invalid theme value "pink"', { setting: 'theme', value: 'pink' })).toBe("“pink” isn't a valid value for theme.")
  })

  it('keeps the service’s text for a code the catalog lacks, or only passes on', () => {
    expect(errorText('SOMETHING_NEW', 'A newer Yggdrasil said this.')).toBe('A newer Yggdrasil said this.')
    expect(errorText('INSTALL_FAILED', 'download qwen3: disk full')).toBe('download qwen3: disk full')
    expect(errorText(undefined, 'No code at all.')).toBe('No code at all.')
  })

  it('follows the App language', async () => {
    await applyLanguage('en-XA')
    const text = errorText('MEMORY_LOOKS_SECRET', 'that looks like a password, key, or token, so Yggdrasil did not save it')
    expect(text).toMatch(/^\[!! .* !!\]$/)
    expect(text).not.toContain('password')
  })
})

describe('ApiError', () => {
  it('reads the code and details, and keeps the service’s English text', async () => {
    vi.stubGlobal('fetch', errorResponse(400, { code: 'MEMORY_LOOKS_SECRET', message: 'that looks like a password, key, or token, so Yggdrasil did not save it' }))
    const error = await api.getSettings().catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    const apiError = error as ApiError
    expect(apiError.code).toBe('MEMORY_LOOKS_SECRET')
    expect(apiError.message).toBe("That looks like a password, key, or token, so Toskar didn't save it.")
    expect(apiError.serviceMessage).toBe('that looks like a password, key, or token, so Yggdrasil did not save it')
  })

  it('says the service is not reachable in the App language', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))
    const error = (await api.getSettings().catch((e: unknown) => e)) as ApiError
    expect(error.code).toBe('SERVICE_UNREACHABLE')
    expect(error.message).toBe("Couldn't reach the local Toskar service (Failed to fetch). If this is the desktop app, quit and reopen it so the service restarts.")
  })
})

describe('streamChat', () => {
  const stream = (sse: string) =>
    vi.fn().mockResolvedValue(new Response(sse, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }))

  it('passes on the error’s code from error_code', async () => {
    vi.stubGlobal(
      'fetch',
      stream('event: error_code\ndata: {"code":"OUT_OF_MEMORY","message":"out of memory"}\n\nevent: error\ndata: out of memory\n\n'),
    )
    const onError = vi.fn()
    await streamChat({ body: { conversation_id: 'c1', message: 'hi' }, onToken: () => {}, onError })
    expect(onError).toHaveBeenCalledWith('out of memory', 'OUT_OF_MEMORY')
  })

  it('works with a service that sends no code', async () => {
    vi.stubGlobal('fetch', stream('event: error\ndata: out of memory\n\n'))
    const onError = vi.fn()
    await streamChat({ body: { conversation_id: 'c1', message: 'hi' }, onToken: () => {}, onError })
    expect(onError).toHaveBeenCalledWith('out of memory', undefined)
  })
})
