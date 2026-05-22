import { describe, it, expect, beforeEach } from 'vitest'
import type { ReactNode } from 'react'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { ProtectedRoute } from './ProtectedRoute'

function makeToken(payload: object): string {
  return `header.${btoa(JSON.stringify(payload))}.signature`
}

/** Render `ui` at /secret, with stub /login and /me routes to observe redirects. */
function renderAt(ui: ReactNode) {
  return render(
    <MemoryRouter initialEntries={['/secret']}>
      <Routes>
        <Route path="/secret" element={ui} />
        <Route path="/login" element={<div>login page</div>} />
        <Route path="/me" element={<div>my page</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('ProtectedRoute', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('redirects to /login when there is no token', () => {
    renderAt(
      <ProtectedRoute>
        <div>protected content</div>
      </ProtectedRoute>,
    )
    expect(screen.getByText('login page')).toBeInTheDocument()
    expect(screen.queryByText('protected content')).not.toBeInTheDocument()
  })

  it('renders children for an authenticated user', () => {
    localStorage.setItem('totra_token', makeToken({ uid: 'u1', role: 'standard' }))
    renderAt(
      <ProtectedRoute>
        <div>protected content</div>
      </ProtectedRoute>,
    )
    expect(screen.getByText('protected content')).toBeInTheDocument()
  })

  it('renders an admin-only route for an admin', () => {
    localStorage.setItem('totra_token', makeToken({ uid: 'u1', role: 'admin' }))
    renderAt(
      <ProtectedRoute adminOnly>
        <div>admin content</div>
      </ProtectedRoute>,
    )
    expect(screen.getByText('admin content')).toBeInTheDocument()
  })

  it('redirects a non-admin away from an admin-only route', () => {
    localStorage.setItem('totra_token', makeToken({ uid: 'u1', role: 'standard' }))
    renderAt(
      <ProtectedRoute adminOnly>
        <div>admin content</div>
      </ProtectedRoute>,
    )
    expect(screen.getByText('my page')).toBeInTheDocument()
    expect(screen.queryByText('admin content')).not.toBeInTheDocument()
  })
})
