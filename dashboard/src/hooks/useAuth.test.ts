import { describe, it, expect, beforeEach, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { useAuth } from './useAuth'
import { login } from '../api/client'

vi.mock('../api/client', () => ({
  login: vi.fn(),
}))

const mockedLogin = vi.mocked(login)

describe('useAuth', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('stores the token and authenticates on a successful sign-in', async () => {
    mockedLogin.mockResolvedValue({
      data: { token: 'jwt-token' },
    } as unknown as Awaited<ReturnType<typeof login>>)

    const { result } = renderHook(() => useAuth())

    let ok: boolean | undefined
    await act(async () => {
      ok = await result.current.signIn('a@b.com', 'pw')
    })

    expect(ok).toBe(true)
    expect(localStorage.getItem('totra_token')).toBe('jwt-token')
    expect(result.current.isAuthenticated).toBe(true)
  })

  it('sets an error and stays unauthenticated on a failed sign-in', async () => {
    mockedLogin.mockRejectedValue(new Error('401'))

    const { result } = renderHook(() => useAuth())

    let ok: boolean | undefined
    await act(async () => {
      ok = await result.current.signIn('a@b.com', 'wrong')
    })

    expect(ok).toBe(false)
    expect(result.current.error).toBe('Invalid credentials')
    expect(result.current.isAuthenticated).toBe(false)
  })

  it('clears the token on sign-out', () => {
    localStorage.setItem('totra_token', 'existing-token')
    const { result } = renderHook(() => useAuth())
    expect(result.current.isAuthenticated).toBe(true)

    act(() => {
      result.current.signOut()
    })

    expect(localStorage.getItem('totra_token')).toBeNull()
    expect(result.current.isAuthenticated).toBe(false)
  })
})
