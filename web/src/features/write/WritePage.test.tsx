import { beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AttemptResult, StudioAnswerResponse, StudyDrill } from '../../api/types'
import type { StudioStroke } from './StudioCanvas'

// The writing studio is the one surface where practice deliberately does NOT advance
// anything. Three properties are worth pinning, and each is something a learner or a
// grader would notice if it broke:
//
//   1. the promise the page makes (recorded, but no path / XP / mastery / streak),
//   2. the exact payload the canvas submits — a hand-built client payload IS the
//      contract the server's strict decoder reads, and its `completed` flag is what
//      decides whether the character counts as written,
//   3. the round: a graded character shows its verdict, and the summary counts the
//      round the learner actually did.

const { queueMock, answerMock, refetchMock, prefsMock } = vi.hoisted(() => ({
  queueMock: vi.fn(),
  answerMock: vi.fn(),
  refetchMock: vi.fn(),
  prefsMock: vi.fn(),
}))

vi.mock('../../api/hooks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/hooks')>()
  return {
    ...actual,
    useStudioQueue: (kind: string, level: number | null) => queueMock(kind, level),
    useAnswerStudio: () => ({ mutate: answerMock, isPending: false, error: null }),
  }
})

vi.mock('../../stores/prefs', () => ({ usePrefs: () => prefsMock() }))

// The canvas itself is browser geometry (pointer capture, stroke order); this file tests
// the page's contract WITH it, so the stub reports strokes the way the canvas does — a
// list, complete or stopped short — and nothing else.
vi.mock('./StudioCanvas', () => ({
  StudioCanvas: ({
    character,
    strokeCount,
    onStrokesChange,
  }: {
    character: string
    strokeCount: number
    onStrokesChange: (strokes: StudioStroke[]) => void
  }) => {
    const draw = (n: number): StudioStroke[] =>
      Array.from({ length: n }, (_, i) => ({ index: i, mistaken: i === 0, d: 'M0 0' }) as StudioStroke)
    return (
      <>
        <button type="button" onClick={() => onStrokesChange(draw(strokeCount))}>
          trace {character}
        </button>
        <button type="button" onClick={() => onStrokesChange(draw(Math.max(1, strokeCount - 1)))}>
          partial {character}
        </button>
      </>
    )
  },
}))

const { WritePage } = await import('./WritePage')

function drill(id: string, character: string, strokes: number, type: StudyDrill['type'] = 'hanzi_trace'): StudyDrill {
  return {
    id,
    number: 1,
    type,
    skill: 'writing',
    difficulty: 1,
    level: 1,
    prompt: { instruction: 'Write the character.', character, stroke_count: strokes, pinyin: 'nǐ hǎo' },
  }
}

function queue(items: StudyDrill[], level = 1) {
  return { items, level, stats: [] }
}

function graded(isCorrect: boolean): StudioAnswerResponse {
  const one: AttemptResult = {
    exercise_id: 'w1',
    client_ref: 'w1:studio:1',
    is_correct: isCorrect,
    score: isCorrect ? 1 : 0,
    grader: 'hanzi_trace',
    feedback: { strokes_expected: 2 },
    duplicate: false,
    needs_review: false,
  }
  return { results: [one], stats: [] }
}

beforeEach(() => {
  vi.clearAllMocks()
  prefsMock.mockReturnValue({ pinyinVisible: false, togglePinyin: vi.fn() })
  queueMock.mockReturnValue({
    isLoading: false,
    error: null,
    data: queue([drill('w1', '你好', 2)]),
    refetch: refetchMock,
  })
  answerMock.mockImplementation(
    (_vars: unknown, opts?: { onSuccess?: (r: StudioAnswerResponse) => void }) => opts?.onSuccess?.(graded(true)),
  )
})

describe('WritePage', () => {
  it('says what the studio is, and what it does not do', () => {
    render(<MemoryRouter><WritePage /></MemoryRouter>)

    // The cost of ungated practice is stated before any work, not discovered afterwards.
    expect(screen.getByText(/Nothing here moves your path and nothing here earns XP/i)).toBeInTheDocument()
    expect(screen.getByText('studio', { selector: 'code' })).toBeInTheDocument()
  })

  it('loads the drills it is asked for, and filters them by level', async () => {
    render(<MemoryRouter><WritePage /></MemoryRouter>)

    // Level is part of the query key: the page asks the server rather than filtering
    // its own copy of the curriculum.
    expect(queueMock).toHaveBeenCalledWith('writing', null)

    await userEvent.click(screen.getByRole('button', { name: 'HSK 2' }))

    expect(queueMock).toHaveBeenCalledWith('writing', 2)
    expect(screen.getByRole('button', { name: 'HSK 2' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('shows the character the drill asks for, and its pinyin only when the learner wants it', () => {
    const { unmount } = render(<MemoryRouter><WritePage /></MemoryRouter>)
    expect(screen.getByText('你好')).toBeInTheDocument()
    expect(screen.queryByText('nǐ hǎo')).not.toBeInTheDocument()
    unmount()

    prefsMock.mockReturnValue({ pinyinVisible: true, togglePinyin: vi.fn() })
    render(<MemoryRouter><WritePage /></MemoryRouter>)
    expect(screen.getByText('nǐ hǎo')).toBeInTheDocument()
  })

  it('submits the exact payload the server decodes, keyed so a retry is idempotent', async () => {
    render(<MemoryRouter><WritePage /></MemoryRouter>)

    await userEvent.click(screen.getByRole('button', { name: 'trace 你好' }))

    expect(answerMock).toHaveBeenCalledTimes(1)
    const [vars] = answerMock.mock.calls[0]
    expect(vars.exercise_id).toBe('w1')
    // The ref names the drill and the round: unique per attempt, stable on a retry.
    expect(vars.client_ref).toMatch(/^w1:studio:\d+$/)
    // The whole write contract in one assertion. Note what is NOT here: the canvas's
    // stroke geometry (path data) stays in the browser — the server grades the reports,
    // which are `{index, mistaken}` and nothing else.
    expect(vars.response).toEqual({
      character: '你好',
      stroke_count: 2,
      strokes: [
        { index: 0, mistaken: true },
        { index: 1, mistaken: false },
      ],
      completed: true,
      hints_used: 0,
    })
  })

  it('records a trace that stopped early, and marks it unfinished', async () => {
    // One stroke of a two-stroke character: the attempt is real and must be recorded,
    // but it is not a written character — `completed` is what the grader reads.
    render(<MemoryRouter><WritePage /></MemoryRouter>)

    await userEvent.click(screen.getByRole('button', { name: 'partial 你好' }))

    expect(answerMock).toHaveBeenCalledTimes(1)
    const [vars] = answerMock.mock.calls[0]
    expect(vars.response.strokes).toHaveLength(1)
    expect(vars.response.completed).toBe(false)
  })

  it('shows the verdict for the character and offers the next one', async () => {
    queueMock.mockReturnValue({
      isLoading: false,
      error: null,
      data: queue([drill('w1', '你', 2), drill('w2', '好', 2)]),
      refetch: refetchMock,
    })
    render(<MemoryRouter><WritePage /></MemoryRouter>)

    expect(screen.queryByText('correct')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'trace 你' }))

    expect(await screen.findByText('correct')).toBeInTheDocument()
    // Not the last character, so the button offers another one rather than a summary.
    await userEvent.click(screen.getByRole('button', { name: 'Next character' }))
    expect(screen.getByText('好')).toBeInTheDocument()
  })

  it('counts the round it actually did, and deals a new one from the server', async () => {
    render(<MemoryRouter><WritePage /></MemoryRouter>)

    await userEvent.click(screen.getByRole('button', { name: 'trace 你好' }))
    await userEvent.click(await screen.findByRole('button', { name: 'See how you did' }))

    expect(screen.getByText('1 / 1')).toBeInTheDocument()
    expect(screen.getByText(/Every character in this session/i)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /Another round/i }))

    expect(refetchMock).toHaveBeenCalled()
  })

  it('says so when a level has no writing drills, instead of rendering an empty card', () => {
    queueMock.mockReturnValue({ isLoading: false, error: null, data: queue([], 5), refetch: refetchMock })
    render(<MemoryRouter><WritePage /></MemoryRouter>)

    expect(screen.getByText('No writing drills at this level')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^trace/ })).not.toBeInTheDocument()
  })

  it('reports a failed load rather than an empty studio', () => {
    queueMock.mockReturnValue({
      isLoading: false,
      error: new Error('drills are unavailable'),
      data: undefined,
      refetch: refetchMock,
    })
    render(<MemoryRouter><WritePage /></MemoryRouter>)

    expect(screen.getByRole('alert')).toHaveTextContent('drills are unavailable')
  })
})
