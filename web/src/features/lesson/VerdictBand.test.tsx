import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AttemptResult, Exercise } from '../../api/types'
import { VerdictBand } from './LessonPage'

// The verdict band. Three outcomes, each stated in words: the colour and the shake are
// emphasis, and a learner who cannot see either still learns what happened.

const exercise: Exercise = {
  id: 'e1',
  number: 1,
  type: 'mcq',
  skill: 'vocabulary',
  difficulty: 1,
  prompt: {
    question: 'Which character means "you"?',
    options: [
      { id: 'a', text: '你' },
      { id: 'b', text: '我' },
    ],
  },
}

function result(overrides: Partial<AttemptResult> = {}): AttemptResult {
  return {
    exercise_id: 'e1',
    client_ref: 'r1',
    is_correct: true,
    score: 1,
    grader: 'mcq',
    feedback: { correct_id: 'a' },
    solution: { correct_id: 'a' },
    duplicate: false,
    needs_review: false,
    ...overrides,
  }
}

describe('VerdictBand', () => {
  it('states a correct answer, names the answer and offers the way on', async () => {
    const next = vi.fn()
    render(<VerdictBand result={result()} exercise={exercise} isLast={false} onContinue={next} />)

    expect(screen.getByText('Correct!')).toBeInTheDocument()
    expect(screen.getByText('你')).toBeInTheDocument()
    expect(screen.getByText(/checked by exact match/i)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /continue/i }))
    expect(next).toHaveBeenCalledTimes(1)
  })

  it('says "Not quite" for a wrong answer, shows why, and shakes', () => {
    render(
      <VerdictBand
        result={result({ is_correct: false, score: 0, explanation: '你 (nǐ) means you; 我 (wǒ) means I.' })}
        exercise={exercise}
        isLast={false}
        onContinue={vi.fn()}
      />,
    )

    expect(screen.getByText('Not quite')).toBeInTheDocument()
    expect(screen.getByText(/你 \(nǐ\) means you/)).toBeInTheDocument()
    // The shake is emphasis on top of the words, never the message itself.
    expect(screen.getByTestId('lesson-action-bar').className).toMatch(/animate-shake/)
  })

  it('never calls an ungradable answer correct, and does not shake for it', () => {
    render(
      <VerdictBand
        result={result({ is_correct: null, score: 0.5, needs_review: true, feedback: { limitation: 'nothing here judges your pronunciation' } })}
        exercise={exercise}
        isLast={false}
        onContinue={vi.fn()}
      />,
    )

    expect(screen.getByText(/saved for you to review/i)).toBeInTheDocument()
    expect(screen.queryByText('Correct!')).not.toBeInTheDocument()
    expect(screen.getByText(/nothing here judges your pronunciation/)).toBeInTheDocument()
    expect(screen.getByTestId('lesson-action-bar').className).not.toMatch(/animate-shake/)
  })

  it('offers to finish the lesson on the last question', () => {
    render(<VerdictBand result={result()} exercise={exercise} isLast onContinue={vi.fn()} />)
    expect(screen.getByRole('button', { name: /finish lesson/i })).toBeInTheDocument()
  })
})
