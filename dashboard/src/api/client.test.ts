import { describe, it, expect, beforeEach } from 'vitest'
import { getMyUID } from './client'
import { makeToken } from '../test/helpers'

describe('getMyUID', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('returns the uid claim from a valid token', () => {
    localStorage.setItem('totra_token', makeToken({ uid: 'user-123', role: 'admin' }))
    expect(getMyUID()).toBe('user-123')
  })

  it('returns "" when no token is stored', () => {
    expect(getMyUID()).toBe('')
  })

  it('returns "" for a malformed token', () => {
    localStorage.setItem('totra_token', 'not-a-jwt')
    expect(getMyUID()).toBe('')
  })

  it('returns "" when the token has no uid claim', () => {
    localStorage.setItem('totra_token', makeToken({ role: 'admin' }))
    expect(getMyUID()).toBe('')
  })
})
