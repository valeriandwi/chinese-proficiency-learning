import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReviewItem } from '../../api/types'
import { ReviewPage } from './ReviewPage'

// One card at a time, and a forecast whose numbers add up to the deck.

const { answerMutate } = vi.hoisted(() => ({ answerMutate: vi.fn() }))

const card = (id: string, zh: string): ReviewItem =>
  ({ id, lexeme_id: `lx-${id}`, simplified: zh, traditional: zh, pinyin: 'p', senses: [{ en: 'meaning' }], due_at: '2026-01-01T00:00:00Z', interval_index: 0, reps: 0, lapses: 0 }) as unknown as ReviewItem

vi.mock('../../api/hooks', () => ({
  useReviewQueue: () => ({
    isLoading: false,
    error: null,
    data: {
      items: [card('1', '你'), card('2', '我')],
      due_total: 2,
      forecast: { now: 2, day: 5, days3: 0, week: 3, later: 1 },
    },
  }),
  useOverview: () => ({ data: { due_reviews: 2 } }),
  useAnswerReview: () => ({ mutate: answerMutate, isPending: false, error: null }),
}))

describe('ReviewPage', () => {
  it('prints every forecast window, even an empty one', () => {
    render(<ReviewPage />)
    const forecast = screen.getByRole('heading', { name: 'Coming up' }).closest('div')!
    for (const label of ['now', 'next 24 h', '2–3 days', 'this week', 'later']) {
      expect(within(forecast).getByText(label)).toBeInTheDocument()
    }
    expect(within(forecast).getByText('5')).toBeInTheDocument()
  })

  it('deals one card at a time and moves on after an answer', async () => {
    answerMutate.mockImplementation((_vars: unknown, opts?: { onSuccess?: (r: { interval_index: number }) => void }) =>
      opts?.onSuccess?.({ interval_index: 1 }),
    )
    render(<ReviewPage />)
    expect(screen.getByText('你')).toBeInTheDocument()
    expect(screen.queryByText('我')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /I knew it/ }))

    expect(screen.getByText('我')).toBeInTheDocument()
    expect(screen.queryByText('你')).not.toBeInTheDocument()
  })
})
