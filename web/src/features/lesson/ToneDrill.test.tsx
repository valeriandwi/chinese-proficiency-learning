import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AttemptResult, Exercise } from '../../api/types'
import { isAnswered, modelAnswer } from '../../lib/answers'
import { ExerciseCard } from './ExerciseCard'

// A one-syllable word is asked about by SOUND: a single syllable cannot undergo tone
// sandhi, so the audio and the answer key can never disagree.
const sounded: Exercise = {
  id: 'e1',
  number: 1,
  type: 'tone_mcq',
  skill: 'listening',
  difficulty: 1,
  prompt: {
    instruction: 'Listen, then choose the tone you heard.',
    text: '穿',
    audio_url: '/media/chuan.mp3',
    replay_limit: 3,
    options: [
      { id: 'a', text: '2nd tone — rising' },
      { id: 'b', text: '1st tone — high level' },
      { id: 'c', text: '3rd tone — falling then rising' },
      { id: 'd', text: '4th tone — falling' },
    ],
  },
}

// More than one syllable is asked about as WRITTEN, and carries no audio: 你好 is
// written 3-3 and spoken 2-3, so a listening question would be unfair.
const written: Exercise = {
  id: 'e2',
  number: 2,
  type: 'tone_mcq',
  skill: 'reading',
  difficulty: 1,
  prompt: {
    instruction: 'Choose the tones written for this word.',
    text: '你好',
    options: [
      { id: 'a', text: '3-3' },
      { id: 'b', text: '2-3' },
      { id: 'c', text: '4-3' },
      { id: 'd', text: '1-1' },
    ],
  },
}

const graded: AttemptResult = {
  exercise_id: 'e2',
  client_ref: 'r1',
  is_correct: false,
  score: 0,
  grader: 'tone_mcq',
  feedback: { correct_id: 'a', tones: '3-3', why: '你好 (nǐ hǎo) is 3-3 — spoken, the first becomes a second tone.' },
  solution: { correct_id: 'a', tones: '3-3' },
  duplicate: false,
  needs_review: false,
}

describe('tone drills', () => {
  it('asks a one-syllable word by sound and shows the word under test', async () => {
    const onChange = vi.fn()
    render(<ExerciseCard exercise={sounded} answer={undefined} showPinyin onChange={onChange} />)

    // The word, the question, and the audio the answer comes from.
    expect(screen.getByText('穿')).toBeInTheDocument()
    // The instruction is both the legend and the card's heading, so it appears twice.
    expect(screen.getAllByText(/listen, then choose the tone you heard/i).length).toBeGreaterThan(0)
    expect(screen.getByText(/transcript appears after you answer/i)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /1st tone/i }))
    expect(onChange).toHaveBeenCalledWith({ option_id: 'b' })
  })

  it('asks a multi-syllable word as written and never plays audio for it', () => {
    render(<ExerciseCard exercise={written} answer={undefined} showPinyin onChange={vi.fn()} />)

    expect(screen.getByText('你好')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '3-3' })).toBeInTheDocument()
    // Without audio the learner cannot be told their ear was wrong for a pattern the
    // written form does not use.
    expect(screen.queryByText(/transcript appears after you answer/i)).not.toBeInTheDocument()
  })

  it('counts as answered once a tone is chosen, and explains the answer', () => {
    expect(isAnswered(written, { option_id: 'a' })).toBe(true)
    expect(isAnswered(written, undefined)).toBe(false)

    const answer = modelAnswer(written, graded)
    expect(answer).toContain('3-3')
    // The sandhi has to reach the learner: 3-3 written is 2-3 spoken.
    expect(answer).toMatch(/second tone/i)
  })

  it('names the tone it picked in the verdict, not just "wrong"', () => {
    render(<ExerciseCard exercise={written} answer={{ option_id: 'b' }} showPinyin onChange={vi.fn()} result={graded} />)
    expect(screen.getByText(/checked by exact match/i)).toBeInTheDocument()
    // The right answer is named in the option list and in the explanation, so it appears
    // more than once — which is the point: the learner is told what the tones are.
    expect(screen.getAllByText(/3-3/).length).toBeGreaterThan(0)
  })
})
