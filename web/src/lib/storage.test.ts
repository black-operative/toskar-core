import { beforeEach, describe, expect, it } from 'vitest'
import { moveStored } from './storage'

describe('moveStored', () => {
  beforeEach(() => localStorage.clear())

  it('moves a value to its new name and removes the old one', () => {
    localStorage.setItem('yggdrasil.apiKey', 'ygg_secret')
    moveStored('yggdrasil.apiKey', 'toskar.apiKey')
    expect(localStorage.getItem('toskar.apiKey')).toBe('ygg_secret')
    expect(localStorage.getItem('yggdrasil.apiKey')).toBeNull()
  })

  it('keeps a value already under the new name', () => {
    localStorage.setItem('yggdrasil.apiKey', 'old')
    localStorage.setItem('toskar.apiKey', 'new')
    moveStored('yggdrasil.apiKey', 'toskar.apiKey')
    expect(localStorage.getItem('toskar.apiKey')).toBe('new')
    expect(localStorage.getItem('yggdrasil.apiKey')).toBeNull()
  })

  it('does nothing when there is nothing to move', () => {
    moveStored('yggdrasil.apiKey', 'toskar.apiKey')
    expect(localStorage.getItem('toskar.apiKey')).toBeNull()
  })
})
