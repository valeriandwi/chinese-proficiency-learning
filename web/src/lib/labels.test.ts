import { describe, expect, it } from 'vitest'
import { describeGoal, describeGrader, describeLessonState, describeSkill, formatPercent } from './labels'

describe('enum labels', () => {
  it('maps a known lesson state to a label and a tone', () => {
    expect(describeLessonState('completed')).toEqual({ label: 'Completed', tone: 'positive', raw: 'completed' })
    expect(describeLessonState('locked').tone).toBe('neutral')
  })

  it('degrades an unknown value to a neutral chip that still shows the value', () => {
    // A server that adds a state must not produce a blank chip on an old client.
    expect(describeLessonState('archived')).toEqual({ label: 'archived', tone: 'neutral', raw: 'archived' })
    expect(describeSkill('calligraphy').label).toBe('calligraphy')
  })

  it('never marks every skill with the same tone', () => {
    const tones = new Set(['vocabulary', 'reading', 'writing'].map((s) => describeSkill(s).tone))
    expect(tones.size).toBeGreaterThan(1)
  })

  it('describes how an answer was judged', () => {
    expect(describeGrader('rubric')).toBe('keyword rubric')
    expect(describeGrader('something_new')).toBe('something_new')
  })

  it('describes a goal in words', () => {
    expect(describeGoal('xp', 50)).toBe('50 XP a day')
    expect(describeGoal('minutes', 20)).toBe('20 minutes a day')
  })

  it('formats a score as a percentage', () => {
    expect(formatPercent(0.6666)).toBe('67%')
    expect(formatPercent(1)).toBe('100%')
  })
})
