import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { ProgressOverview, User } from '../../api/types'

// The statistics page. Two things matter beyond "it renders": a skill nobody has
// attempted is not reported as a failure, and the rebuild control — which was sitting
// in the primary position of the learner's home screen — lives here.

const learner: User = {
  id: 'u1',
  email: 'learner@example.com',
  display_name: 'Lan',
  role: 'learner',
  email_verified_at: null,
  timezone: 'Asia/Jakarta',
  daily_goal_value: 20,
  daily_goal_kind: 'minutes',
  pinyin_default: 'always',
  target_curriculum: 'hsk2.0',
  target_level: 2,
  placement_completed_at: null,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

const overview: ProgressOverview = {
  streak: {
    current_streak: 6,
    longest_streak: 9,
    last_active_day: '2026-10-13',
    goal_kind: 'minutes',
    goal_value: 20,
    today_minutes: 7,
    today_xp: 30,
    goal_met_today: false,
    freezes: 0,
  },
  skills: [
    { skill: 'vocabulary', attempts: 9, correct: 9, mastery: 1, last_attempt_at: '2026-10-13T10:00:00Z' },
    // The case from the screenshot: this build cannot assess speaking without a
    // provider, and a learner must not be shown 0% for it.
    { skill: 'speaking', attempts: 0, correct: 0, mastery: 0, last_attempt_at: null },
  ],
  activity: [{ day: '2026-10-13', minutes: 7, xp: 30, attempts: 12, correct: 9, lessons_completed: 1, goal_met: false }],
  total_xp: 120,
  lessons_completed: 3,
  total_attempts: 51,
  total_correct: 41,
  due_reviews: 0,
  curriculum_version: 'hsk2.0',
}

const refresh = vi.fn()

vi.mock('../../api/hooks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/hooks')>()
  return {
    ...actual,
    useSession: () => ({ isLoading: false, data: { authenticated: true, user: learner } }),
    useOverview: () => ({ isLoading: false, error: null, data: overview }),
    useCurricula: () => ({ isLoading: false, data: { curricula: [{ version: 'hsk2.0', level_count: 5 }] } }),
    useRefreshProgress: () => ({ mutate: refresh, isPending: false }),
  }
})

const { ProgressPage } = await import('./ProgressPage')

function renderPage() {
  return render(
    <MemoryRouter>
      <ProgressPage />
    </MemoryRouter>,
  )
}

describe('ProgressPage', () => {
  it('carries the statistics the home screen used to hold', () => {
    renderPage()
    expect(screen.getByRole('heading', { name: 'Progress' })).toBeInTheDocument()
    expect(screen.getByText('Skill mastery')).toBeInTheDocument()
    expect(screen.getByText('Last 30 days')).toBeInTheDocument()
    expect(screen.getByText('Streak')).toBeInTheDocument()
    expect(screen.getByText("Today's goal")).toBeInTheDocument()
    expect(screen.getByText('51')).toBeInTheDocument()
  })

  it('does not report an unattempted skill as zero mastery', () => {
    renderPage()
    const speaking = screen.getByText('Speaking').closest('li')!
    expect(within(speaking).getByText('not yet assessed')).toBeInTheDocument()
    expect(within(speaking).queryByText(/^0% · 0\/0$/)).not.toBeInTheDocument()
    // ...while an attempted skill still reports its real number.
    const vocabulary = screen.getByText('Vocabulary').closest('li')!
    expect(within(vocabulary).getByText(/100% · 9\/9/)).toBeInTheDocument()
  })

  it('keeps the rebuild control here rather than on the home screen', () => {
    renderPage()
    const recompute = screen.getByRole('button', { name: /recompute/i })
    recompute.click()
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('offers the way back to the path', () => {
    renderPage()
    expect(screen.getByRole('link', { name: /back to your path/i })).toHaveAttribute('href', '/')
  })
})
