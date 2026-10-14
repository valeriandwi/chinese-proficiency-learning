import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ToneAnswersResponse, ToneDrill, ToneQueue } from '../../api/types'

// Tone practice is off-path by design, and the properties worth pinning are the ones a
// learner notices: the round does not reshuffle, every answer is graded by the server
// and explained, and the round ends by naming the tone they actually missed.

const { queueMock, answerMutate } = vi.hoisted(() => ({
  queueMock: vi.fn(),
  answerMutate: vi.fn(),
}))

vi.mock('../../api/hooks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/hooks')>()
  return {
    ...actual,
    useToneQueue: (level: number | null) => queueMock(level),
    useAnswerTone: () => ({ mutate: answerMutate, isPending: false, error: null }),
  }
})

const { TonePage } = await import('./TonePage')

function drill(id: string, text: string, tones: string): ToneDrill {
  return {
    id,
    number: 1,
    type: 'tone_mcq',
    skill: 'reading',
    difficulty: 1,
    level: 2,
    prompt: {
      instruction: 'Choose the tones written for this word.',
      text,
      options: [
        { id: 'a', text: tones },
        { id: 'b', text: '1-1' },
        { id: 'c', text: '2-4' },
        { id: 'd', text: '4-2' },
      ],
      // No answer key: the server owns it, and the prompt the learner sees never has one.
    },
  }
}

function queue(items: ToneDrill[]): ToneQueue {
  return {
    items,
    level: 2,
    stats: [{ level: 2, drills: 147, answered: 0, correct: 0 }],
  }
}

function answerResponse(isCorrect: boolean, tones: string): ToneAnswersResponse {
  return {
    results: [
      {
        exercise_id: 'e1',
        client_ref: 'ref',
        is_correct: isCorrect,
        score: isCorrect ? 1 : 0,
        grader: 'tone_mcq',
        feedback: { correct_id: 'a', tones },
        solution: { correct_id: 'a', tones },
        duplicate: false,
        needs_review: false,
      },
    ],
    tone_tally: [{ tone: tones.split('-')[0], label: '3rd tone — falling then rising', asked: 1, missed: isCorrect ? 0 : 1 }],
    stats: [{ level: 2, drills: 147, answered: 1, correct: isCorrect ? 1 : 0 }],
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  queueMock.mockReturnValue({ isLoading: false, error: null, data: queue([drill('e1', '你好', '3-3')]), refetch: vi.fn() })
  answerMutate.mockImplementation((_vars: unknown, opts?: { onSuccess?: (r: ToneAnswersResponse) => void }) => {
    opts?.onSuccess?.(answerResponse(false, '3-3'))
  })
})

describe('TonePage', () => {
  it('asks the first tone question and counts the round', () => {
    render(<TonePage />)

    expect(screen.getByText('你好')).toBeInTheDocument()
    expect(screen.getAllByText(/choose the tones written for this word/i).length).toBeGreaterThan(0)
    expect(screen.getByText(/1 \/ 1 · 0 correct so far/)).toBeInTheDocument()
  })

  it('sends exactly one answer for the drill, keyed so a retry is idempotent', async () => {
    queueMock.mockReturnValue({
      isLoading: false,
      error: null,
      data: queue([drill('e1', '你好', '3-3'), drill('e2', '现在', '4-4')]),
      refetch: vi.fn(),
    })
    render(<TonePage />)

    await userEvent.click(screen.getByRole('button', { name: '3-3' }))

    expect(answerMutate).toHaveBeenCalledTimes(1)
    const [vars] = answerMutate.mock.calls[0]
    expect(vars.exercise_id).toBe('e1')
    expect(vars.option_id).toBe('a')
    // The ref must name the drill and the round: unique per attempt, stable on retry.
    expect(vars.client_ref).toMatch(/^e1:tone:\d+$/)
  })

  it('shows what the tone was and why, then moves on', async () => {
    queueMock.mockReturnValue({
      isLoading: false,
      error: null,
      data: queue([drill('e1', '你好', '3-3'), drill('e2', '现在', '4-4')]),
      refetch: vi.fn(),
    })
    render(<TonePage />)

    await userEvent.click(screen.getByRole('button', { name: '3-3' }))

    // The verdict appears as a chip and as the scored heading, so assert it exists
    // rather than that it is unique.
    expect(screen.getAllByText(/not quite/i).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/3-3/).length).toBeGreaterThan(0)

    await userEvent.click(screen.getByRole('button', { name: /next/i }))
    expect(screen.getByText('现在')).toBeInTheDocument()
  })

  it('ends the round by naming the tone that was missed', async () => {
    render(<TonePage />)

    await userEvent.click(screen.getByRole('button', { name: '3-3' }))
    await userEvent.click(screen.getByRole('button', { name: /see how you did/i }))

    const summary = screen.getByText(/0 \/ 1/).closest('div')!.parentElement!
    // The missed tone is named twice on purpose: once in the per-tone table and once as
    // the "practice this one" callout.
    expect(within(summary).getAllByText(/3rd tone — falling then rising/).length).toBeGreaterThan(0)
    expect(within(summary).getByText(/0\/1/)).toBeInTheDocument()
    expect(within(summary).getByText(/practice this one/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /another round/i })).toBeInTheDocument()
  })

  it('shows an empty state instead of an empty round', () => {
    queueMock.mockReturnValue({
      isLoading: false,
      error: null,
      data: { items: [], level: 2, stats: [] },
      refetch: vi.fn(),
    })
    render(<TonePage />)

    expect(screen.getByText(/no tone drills at this level/i)).toBeInTheDocument()
  })

  it('asks the server for the chosen level instead of choosing for itself', async () => {
    render(<TonePage />)
    // The page starts by letting the server decide (null), which is why the chip that
    // matches the drills in hand is the highlighted one — not a guessed default.
    expect(queueMock).toHaveBeenCalledWith(null)
    expect(screen.getByRole('button', { name: 'HSK 2' })).toHaveAttribute('aria-pressed', 'true')

    await userEvent.click(screen.getByRole('button', { name: 'HSK 3' }))
    expect(queueMock).toHaveBeenCalledWith(3)
  })
})
