import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ProgressOverview, User } from './api/types'

// The shell's contract: a learner always has a navigation, the page they are on is
// announced to assistive tech, and the progress rail is a real landmark rather than
// a decoration. These are the acceptance criteria for the layout phase.

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
  target_level: 1,
  placement_completed_at: null,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

const overview: ProgressOverview = {
  streak: {
    current_streak: 4,
    longest_streak: 9,
    last_active_day: '2026-10-12',
    freezes: 1,
    goal_met_today: false,
    goal_value: 20,
    goal_kind: 'minutes',
    today_xp: 30,
    today_minutes: 10,
  },
  skills: [],
  total_attempts: 12,
  total_correct: 9,
  lessons_completed: 2,
  total_xp: 120,
  due_reviews: 3,
  activity: [],
  curriculum_version: 'hsk2.0',
  next_lesson_id: 'lesson-3',
  next_lesson_title: '你好',
}

const authenticated = { value: true }

vi.mock('./api/hooks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api/hooks')>()
  return {
    ...actual,
    useSession: () =>
      authenticated.value
        ? { isLoading: false, data: { authenticated: true, user: learner } }
        : { isLoading: false, data: { authenticated: false } },
    useOverview: () => ({ isLoading: false, data: overview }),
    useLogout: () => ({ mutateAsync: vi.fn() }),
    useRefreshProgress: () => ({ mutate: vi.fn(), isPending: false }),
    useCurricula: () => ({ isLoading: false, data: { curricula: [] } }),
    useReviewQueue: () => ({ isLoading: false, data: { items: [], due_total: 0 } }),
    // The home screen is the path, so the shell's test needs it to resolve.
    usePath: () => ({
      isLoading: false,
      error: null,
      data: {
        version: 'hsk2.0',
        levels: [
          {
            level_number: 1,
            title: 'Level 1',
            units: [
              {
                unit_number: 1,
                title: 'greetings and names',
                lessons: [
                  { id: 'l1', lesson_number: 1, title: '你好 — saying hello', state: 'in_progress' },
                  { id: 'l2', lesson_number: 2, title: '你叫什么名字 — asking names', state: 'locked' },
                ],
              },
            ],
          },
        ],
      },
    }),
  }
})

const { App } = await import('./app')

function renderAt(path: string) {
  // A real client, because the pages behind the shell genuinely use one.
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  authenticated.value = true
})

describe('the application shell', () => {
  it('offers every destination in the sidebar and the bottom bar', () => {
    renderAt('/')
    // Both navigations are rendered; CSS media queries decide which one a device
    // shows, so both are asserted here (hidden: true includes the display-none one).
    const navs = screen.getAllByRole('navigation', { name: 'Main', hidden: true })
    expect(navs).toHaveLength(2)
    for (const navigation of navs) {
      // Five: "Today" and "Path" were two labels for one screen; Progress has its own tab,
      // and Tones now lives in Practice with the other studios.
      for (const label of ['Learn', 'Practice', 'Reviews', 'Progress', 'Profile']) {
        expect(
          within(navigation).getByRole('link', { name: new RegExp(label), hidden: true }),
        ).toBeInTheDocument()
      }
    }
  })

  it('announces the current page to assistive technology', () => {
    renderAt('/reviews')
    const current = screen.getAllByRole('link', { current: 'page', hidden: true })
    expect(current.length).toBeGreaterThan(0)
    expect(current.every((link) => /Reviews/.test(link.textContent ?? ''))).toBe(true)
  })

  it('shows the review count on the destinations that lead to it', () => {
    renderAt('/')
    const [sidebar, bottom] = screen.getAllByRole('navigation', { name: 'Main', hidden: true })
    expect(within(sidebar).getByText('3')).toBeInTheDocument()
    expect(within(bottom).getByText('3')).toBeInTheDocument()
  })

  it('renders the path as the home screen, not a dashboard', async () => {
    renderAt('/')
    // The home's job is to say what to do next. The statistics that used to share this
    // screen — and the database operation that sat in its primary position — moved to
    // /progress.
    expect(await screen.findByRole('heading', { name: /saying hello/ })).toBeInTheDocument()
    expect(screen.queryByText(/Skill mastery/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/Last 30 days/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /recompute/i })).not.toBeInTheDocument()
  })

  it('shows the learner numbers once, in one strip that leads to the details', () => {
    renderAt('/')
    // One strip, because the same numbers used to be a tile row here AND a desktop
    // rail — two copies of the streak and the goal on a single screen.
    const strips = screen.getAllByTestId('gamified-header')
    expect(strips).toHaveLength(1)
    const strip = strips[0]

    expect(within(strip).getByRole('link', { name: /Streak: 4 days/ })).toBeInTheDocument()
    // The ring is an img with an accessible name that states the percentage, not a
    // progressbar role: it is a picture of a number, and the number is in the label.
    expect(within(strip).getByRole('img', { name: /of today's goal/i })).toBeInTheDocument()
    expect(within(strip).getByRole('link', { name: /120 XP earned/ })).toBeInTheDocument()

    // Every number in the strip is a way into the full picture.
    for (const link of within(strip).getAllByRole('link')) {
      expect(link).toHaveAttribute('href', '/progress')
    }
    // And the duplication is gone, not merely restyled.
    expect(screen.queryByRole('complementary', { name: 'Your progress' })).not.toBeInTheDocument()
  })

  it('does not render a navigation when signed out', () => {
    authenticated.value = false
    renderAt('/login')
    expect(screen.queryByRole('navigation', { name: 'Main', hidden: true })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Sign in' })).toBeInTheDocument()
  })
})
