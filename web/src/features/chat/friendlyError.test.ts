import { describe, expect, it } from 'vitest'
import { explainError } from './friendlyError'

describe('explainError', () => {
  it.each([
    ['llama-server not installed: llama-server is not installed. Install from the Runtimes page', "The AI engine isn't installed yet", ['models']],
    ['llama-server error 400: {"error":{"message":"the request exceeds the available context size"}}', 'This conversation is too long for the model', ['new-chat', 'models']],
    ['read tcp 127.0.0.1:51234->127.0.0.1:8081: read: connection reset by peer', "Couldn't reach the model", ['retry']],
    ['ggml_metal: failed to allocate buffer, out of memory', 'Not enough memory for this model', ['retry', 'models']],
    ['Studio is offline. Turn that computer on and open Yggdrasil, or change the role to Automatic placement.', "A computer you're using is offline", ['retry', 'computers']],
    ['llama-server error 500: Internal Server Error', 'The model ran into a problem', ['retry', 'models']],
    ['profile "x" needs model assignments (or pass a model id); no installed model', 'No model is ready', ['models']],
  ])('explains %s', (raw, title, actions) => {
    const e = explainError(raw)
    expect(e.title).toBe(title)
    expect(e.actions).toEqual(actions)
    expect(e.detail).toBe(raw)
  })

  it('passes a plain sentence through', () => {
    const e = explainError('Tread Right is not deployed yet. Finish training and deploy it on the Train page.')
    expect(e.title).toBe('That didn’t work')
    expect(e.body).toMatch(/^Tread Right is not deployed yet/)
    expect(e.detail).toBeUndefined()
  })

  it('hides machine text behind Details', () => {
    const e = explainError('panic: runtime error: index out of range [3] with length 3')
    expect(e.title).toBe('Something went wrong')
    expect(e.body).not.toMatch(/panic/)
    expect(e.detail).toMatch(/^panic/)
  })
})

describe('explainError with a code', () => {
  it('reads the code before the text', () => {
    const e = explainError('メモリが足りません', 'OUT_OF_MEMORY')
    expect(e.title).toBe('Not enough memory for this model')
    expect(e.actions).toEqual(['retry', 'models'])
    expect(e.detail).toBe('メモリが足りません')
  })

  it('shows a code’s catalog text when chat has no card for it', () => {
    const e = explainError('that looks like a password, key, or token, so Yggdrasil did not save it', 'MEMORY_LOOKS_SECRET')
    expect(e.title).toBe('That didn’t work')
    expect(e.body).toBe("That looks like a password, key, or token, so Toskar didn't save it.")
    expect(e.detail).toBe('that looks like a password, key, or token, so Yggdrasil did not save it')
  })

  it('falls back to the text for a code it does not know', () => {
    expect(explainError('ggml_metal: failed to allocate buffer, out of memory', 'SOMETHING_NEW').title).toBe('Not enough memory for this model')
  })
})
