import { describe, expect, it } from 'vitest'
import type { Exercise } from '../api/types'
import { emptyAnswer, isAnswered } from './answers'

function exercise(type: Exercise['type'], prompt: Record<string, unknown> = {}): Exercise {
  return { id: 'e1', number: 1, type, skill: 'vocabulary', difficulty: 1, prompt }
}

// "Next" must stay disabled until an exercise is genuinely answered, or a learner
// can finish a lesson having skipped half of it.

describe('isAnswered', () => {
  it('rejects an empty answer for every type', () => {
    const types: Exercise['type'][] = [
      'mcq',
      'mcq_multi',
      'true_false',
      'fill_blank',
      'sentence_order',
      'translation',
      'hanzi_trace',
      'hanzi_recall',
      'short_answer',
    ]
    for (const type of types) {
      const ex = exercise(type, { tokens: ['a', 'b'] })
      expect(isAnswered(ex, emptyAnswer(ex)), `${type} should not count as answered`).toBe(false)
      expect(isAnswered(ex, undefined), `${type} with no answer should not count`).toBe(false)
    }
  })

  it('accepts a real answer for each type', () => {
    expect(isAnswered(exercise('mcq'), { option_id: 'a' })).toBe(true)
    expect(isAnswered(exercise('mcq_multi'), { option_ids: ['a'] })).toBe(true)
    expect(isAnswered(exercise('true_false'), { value: false })).toBe(true)
    expect(isAnswered(exercise('fill_blank'), { text: '好' })).toBe(true)
    expect(isAnswered(exercise('translation'), { text: 'hello' })).toBe(true)
    expect(isAnswered(exercise('short_answer'), { text: '今天' })).toBe(true)
  })

  it('treats whitespace as no answer', () => {
    expect(isAnswered(exercise('fill_blank'), { text: '   ' })).toBe(false)
  })

  it('requires every token to be placed before a sentence is answered', () => {
    const ex = exercise('sentence_order', { tokens: ['叫', '我', '小明'] })
    expect(isAnswered(ex, { order: [1, 0] })).toBe(false)
    expect(isAnswered(ex, { order: [1, 0, 2] })).toBe(true)
  })

  it('requires a completed hanzi attempt with at least one stroke', () => {
    const ex = exercise('hanzi_trace', { character: '你', stroke_count: 7 })
    expect(isAnswered(ex, { strokes: [{ index: 0 }], completed: false })).toBe(false)
    expect(isAnswered(ex, { strokes: [], completed: true })).toBe(false)
    expect(isAnswered(ex, { strokes: [{ index: 0 }], completed: true })).toBe(true)
  })

  it('accepts a listening item only once something has been chosen or typed', () => {
    expect(isAnswered(exercise('listening_mcq'), { option_id: 'b' })).toBe(true)
    expect(isAnswered(exercise('listening_mcq'), {})).toBe(false)
    expect(isAnswered(exercise('listening_dictation'), { text: '谢谢你。' })).toBe(true)
    expect(isAnswered(exercise('listening_dictation'), { text: '  ' })).toBe(false)
  })

  it('does not count a speaking drill as answered until the learner acts', () => {
    // A freshly rendered speaking item must be unanswered. Seeding a 0 ms duration
    // (or reporting one on mount) would satisfy isAnswered and let the learner page
    // through every speaking drill without speaking.
    for (const type of ['speak_repeat', 'read_aloud', 'speak_answer'] as const) {
      const ex = exercise(type)
      expect(emptyAnswer(ex)).toEqual({})
      expect(isAnswered(ex, emptyAnswer(ex))).toBe(false)
      expect(isAnswered(ex, undefined)).toBe(false)
      // A real take counts…
      expect(isAnswered(ex, { duration_ms: 2600, self_rating: 4 })).toBe(true)
      // …and so does the explicit "I cannot record" skip, which the server records
      // honestly as an unrecorded attempt.
      expect(isAnswered(ex, { duration_ms: 0 })).toBe(true)
    }
  })
})
