import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AttemptResult, Exercise } from '../../api/types'
import { ExerciseCard } from './ExerciseCard'

const mcq: Exercise = {
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
      { id: 'c', text: '好' },
    ],
  },
}

const wrongResult: AttemptResult = {
  exercise_id: 'e1',
  client_ref: 'r1',
  is_correct: false,
  score: 0,
  grader: 'mcq',
  feedback: { correct_id: 'a', why: '你 (nǐ) is you' },
  solution: { correct_id: 'a', why: '你 (nǐ) is you' },
  duplicate: false,
  needs_review: false,
}

describe('ExerciseCard', () => {
  it('reports the option the learner picks, using the wire shape the API expects', async () => {
    const onChange = vi.fn()
    render(<ExerciseCard exercise={mcq} answer={undefined} showPinyin onChange={onChange} />)

    await userEvent.click(screen.getByRole('button', { name: /我/ }))

    expect(onChange).toHaveBeenCalledWith({ option_id: 'b' })
  })

  it('picks an option from the keyboard with its number, and stops once graded', async () => {
    const onChange = vi.fn()
    const { rerender } = render(<ExerciseCard exercise={mcq} answer={undefined} showPinyin onChange={onChange} />)

    await userEvent.keyboard('3')
    expect(onChange).toHaveBeenCalledWith({ option_id: 'c' })

    onChange.mockClear()
    rerender(<ExerciseCard exercise={mcq} answer={{ option_id: 'c' }} showPinyin disabled onChange={onChange} />)
    await userEvent.keyboard('1')
    expect(onChange).not.toHaveBeenCalled()
  })

  it('shows the verdict, the correct answer and who judged it once graded', () => {
    render(<ExerciseCard exercise={mcq} answer={{ option_id: 'b' }} showPinyin onChange={vi.fn()} result={wrongResult} />)

    // The verdict appears once as a chip and once with the score; assert the scored one.
    const verdict = screen.getByText(/not quite \(0%\)/i)
    expect(verdict).toBeInTheDocument()
    expect(screen.getByText(/checked by exact match/i)).toBeInTheDocument()
    expect(verdict.closest('div')).toHaveTextContent('你')
  })

  it('never claims a free-form answer was correct', () => {
    const shortAnswer: Exercise = {
      id: 'e2',
      number: 2,
      type: 'short_answer',
      skill: 'writing',
      difficulty: 4,
      prompt: { question: 'Write about your day', min_sentences: 2 },
    }
    const rubricResult: AttemptResult = {
      exercise_id: 'e2',
      client_ref: 'r2',
      is_correct: null,
      score: 0.6,
      grader: 'rubric',
      feedback: {
        missing: ['今天'],
        limitation: 'keyword/coverage rubric only: this check cannot judge grammar, tone or argument quality',
        needs_review: true,
      },
      duplicate: false,
      needs_review: true,
    }

    render(
      <ExerciseCard
        exercise={shortAnswer}
        answer={{ text: '我早上八点起床' }}
        showPinyin
        onChange={vi.fn()}
        result={rubricResult}
      />,
    )

    expect(screen.getByText(/recorded for review/i)).toBeInTheDocument()
    expect(screen.getByText(/cannot judge grammar/i)).toBeInTheDocument()
    expect(screen.queryByText(/^correct/i)).not.toBeInTheDocument()
  })

  it('clears the input for a fill-in-the-blank through the word bank', async () => {
    const fillBlank: Exercise = {
      id: 'e3',
      number: 3,
      type: 'fill_blank',
      skill: 'writing',
      difficulty: 1,
      prompt: { sentence: '你___！', word_bank: ['好', '是'] },
    }
    const onChange = vi.fn()
    render(<ExerciseCard exercise={fillBlank} answer={undefined} showPinyin onChange={onChange} />)

    await userEvent.click(screen.getByRole('button', { name: '好' }))

    expect(onChange).toHaveBeenCalledWith({ text: '好' })
  })
})
