import { StrictMode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { User } from '../../api/types'
import { ProfilePage } from './ProfilePage'

// Settings save themselves: one save per pause in typing, never an invalid one.

const { mutateAsync } = vi.hoisted(() => ({ mutateAsync: vi.fn() }))

const user: User = {
  id: 'u1',
  email: 'a@example.com',
  display_name: 'Lan',
  role: 'learner',
  email_verified_at: null,
  timezone: 'Asia/Jakarta',
  daily_goal_value: 20,
  daily_goal_kind: 'minutes',
  pinyin_default: 'always',
  target_curriculum: 'hsk2.0',
  target_level: 1,
  placement_completed_at: null,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

vi.mock('../../api/hooks', () => ({
  useSession: () => ({ data: { authenticated: true, user } }),
  useOverview: () => ({ data: undefined }),
  useLogout: () => ({ mutateAsync: vi.fn() }),
  useUpdateMe: () => ({ mutateAsync, isPending: false, error: null }),
}))

beforeEach(() => {
  vi.useFakeTimers()
  mutateAsync.mockReset().mockResolvedValue(undefined)
})
afterEach(() => vi.useRealTimers())

const renderPage = () =>
  render(
    <MemoryRouter>
      <ProfilePage />
    </MemoryRouter>,
  )

describe('ProfilePage auto-save', () => {
  it('does not save on open, then saves once after a change settles', async () => {
    renderPage()
    await vi.advanceTimersByTimeAsync(2000)
    expect(mutateAsync).not.toHaveBeenCalled()

    const target = screen.getByLabelText('Target per day')
    fireEvent.change(target, { target: { value: '3' } })
    fireEvent.change(target, { target: { value: '30' } })
    await vi.advanceTimersByTimeAsync(800)

    expect(mutateAsync).toHaveBeenCalledTimes(1)
    expect(mutateAsync).toHaveBeenCalledWith(expect.objectContaining({ daily_goal_value: 30 }))
  })

  it('does not save an untouched form, even when effects run twice', async () => {
    render(
      <StrictMode>
        <MemoryRouter>
          <ProfilePage />
        </MemoryRouter>
      </StrictMode>,
    )
    await vi.advanceTimersByTimeAsync(2000)
    expect(mutateAsync).not.toHaveBeenCalled()
  })

  it('never sends an invalid form', async () => {
    renderPage()
    fireEvent.change(screen.getByLabelText('Target per day'), { target: { value: '0' } })
    await vi.advanceTimersByTimeAsync(2000)
    expect(mutateAsync).not.toHaveBeenCalled()
  })
})
