import { describe, it, expect, beforeEach, vi } from 'vitest'
import type { ReactNode } from 'react'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { UsersPage } from './UsersPage'
import { listUsers } from '../../api/client'

vi.mock('../../api/client', () => ({
  listUsers: vi.fn(),
  createUser: vi.fn(),
}))

const mockedListUsers = vi.mocked(listUsers)

function renderWithQuery(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

describe('UsersPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders a row per user with the correct status badge', async () => {
    mockedListUsers.mockResolvedValue({
      data: {
        total: 2,
        users: [
          {
            id: '1',
            name: 'Alice',
            email: 'alice@acme.com',
            role: 'admin',
            quota_scu: 1000,
            is_active: true,
          },
          {
            id: '2',
            name: 'Bob',
            email: 'bob@acme.com',
            role: 'standard',
            quota_scu: 500,
            is_active: false,
          },
        ],
      },
    } as unknown as Awaited<ReturnType<typeof listUsers>>)

    renderWithQuery(<UsersPage />)

    expect(await screen.findByText('Alice')).toBeInTheDocument()
    expect(screen.getByText('Bob')).toBeInTheDocument()
    expect(screen.getByText('Active')).toBeInTheDocument()
    expect(screen.getByText('Inactive')).toBeInTheDocument()
  })
})
