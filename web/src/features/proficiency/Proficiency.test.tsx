import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import type { ProficiencyCardsResponse, ProficiencyPaperResponse, ProficiencySubmitResponse } from '../../api/types'

// Two pages, two rules worth pinning: a paper cannot be submitted until every question is
// answered (that is what makes a pass mean something), and the two outcomes say the truth
// — a pass opens the level, a failure changes nothing.

const { cardsMock, paperMock, submitMock } = vi.hoisted(() => ({
  cardsMock: vi.fn(),
  paperMock: vi.fn(),
  submitMock: vi.fn(),
}))

vi.mock('../../api/hooks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/hooks')>()
  return {
    ...actual,
    useProficiencyCards: () => cardsMock(),
    useProficiencyPaper: () => paperMock(),
    useSubmitProficiency: () => ({ mutateAsync: submitMock, isPending: false, error: null }),
  }
})

const { ProficiencyPage } = await import('./ProficiencyPage')
const { PaperPage } = await import('./PaperPage')

function question(id: string, word: string) {
  return {
    id,
    number: 1,
    type: 'mcq' as const,
    skill: 'vocabulary' as const,
    difficulty: 1,
    prompt: {
      question: `Which character means "${id}"?`,
      options: [
        { id: 'a', text: word },
        { id: 'b', text: '我' },
        { id: 'c', text: '好' },
      ],
    },
  }
}

const paper: ProficiencyPaperResponse = {
  paper_id: 'paper-1',
  level: { number: 1, title: 'Level 1 — survival Mandarin' },
  questions: [question('one', '你'), question('two', '他')],
  pass_threshold: 0.8,
}

function submitResult(passed: boolean, unlockedNow: boolean): ProficiencySubmitResponse {
  return {
    results: [],
    paper: {
      id: 'paper-1',
      level: 1,
      score: passed ? 1 : 0.5,
      percent: passed ? 100 : 50,
      passed,
      graded_count: 2,
      correct_count: passed ? 2 : 1,
      pass_mark: 0.8,
    },
    unlocked_now: unlockedNow,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  paperMock.mockReturnValue({ isLoading: false, error: null, data: paper, refetch: vi.fn() })
  cardsMock.mockReturnValue({
    isLoading: false,
    error: null,
    data: {
      cards: [
        { level: 1, title: 'Level 1', questions: 60, unlocked_by_test: false, best_score: null, papers_taken: 0, passed: false },
        { level: 3, title: 'Level 3', questions: 1344, unlocked_by_test: true, best_score: 0.9, papers_taken: 2, passed: true },
      ],
      pass_threshold: 0.8,
      paper_size: 20,
    } satisfies ProficiencyCardsResponse,
  })
})

describe('the test-out list', () => {
  it('offers a paper per level and says which levels a test already opened', () => {
    render(
      <MemoryRouter>
        <ProficiencyPage />
      </MemoryRouter>,
    )
    expect(screen.getByRole('heading', { name: 'Test out' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Take the HSK 1 test/ })).toHaveAttribute('href', '/proficiency/1')
    expect(screen.getByText(/opened by a test/)).toBeInTheDocument()
    // A level that passed shows its result rather than offering a first attempt.
    expect(screen.getByText(/passed · best 90%/)).toBeInTheDocument()
  })
})

describe('the paper', () => {
  it('asks the questions with no feedback, and will not submit a part-answered paper', async () => {
    render(<PaperPage level={1} onLeave={vi.fn()} />)

    expect(screen.getByText(/No answers until you submit/)).toBeInTheDocument()
    expect(screen.getByText(/1 \/ 2 · 0 answered/)).toBeInTheDocument()
    // Nothing about correctness can appear mid-paper: the result is absent and the card
    // renders no verdict.
    expect(screen.queryByText(/not quite/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/checked by/i)).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '你' }))
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    // The last question, still unanswered: the button offers the remaining count rather
    // than a submit that would be refused by the server.
    expect(screen.getByRole('button', { name: /1 left to answer/ })).toBeDisabled()

    await userEvent.click(screen.getByRole('button', { name: '他' }))
    expect(screen.getByRole('button', { name: 'Submit paper' })).toBeEnabled()
  })

  it('submits every question, and reports a pass as the level being open', async () => {
    submitMock.mockResolvedValue(submitResult(true, true))
    render(<PaperPage level={1} onLeave={vi.fn()} />)

    await userEvent.click(screen.getByRole('button', { name: '你' }))
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await userEvent.click(screen.getByRole('button', { name: '他' }))
    await userEvent.click(screen.getByRole('button', { name: 'Submit paper' }))

    const [payload] = submitMock.mock.calls[0]
    expect(payload.paper_id).toBe('paper-1')
    // Every question, not only the ones with answers: the server refuses a partial paper.
    expect(payload.answers.map((a: { exercise_id: string }) => a.exercise_id)).toEqual(['one', 'two'])

    expect(await screen.findByText(/HSK 1 passed/)).toBeInTheDocument()
    expect(screen.getByText(/2 \/ 2/)).toBeInTheDocument()
    expect(screen.getByText(/Level 1 is open/)).toBeInTheDocument()
  })

  it('says plainly that a failed paper changed nothing', async () => {
    submitMock.mockResolvedValue(submitResult(false, false))
    render(<PaperPage level={1} onLeave={vi.fn()} />)

    await userEvent.click(screen.getByRole('button', { name: '你' }))
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await userEvent.click(screen.getByRole('button', { name: '他' }))
    await userEvent.click(screen.getByRole('button', { name: 'Submit paper' }))

    expect(await screen.findByText(/Not this time/)).toBeInTheDocument()
    expect(screen.getByText(/You needed 80%\. Nothing changed/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Try another paper/ })).toBeInTheDocument()
  })
})
