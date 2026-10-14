import type { AnswerPayload, AttemptResult, Exercise } from '../api/types'

// Building a response payload is the one place the SPA has to know each exercise
// type's wire shape. Keeping it here (instead of inside the components) means the
// renderers stay presentational and the payloads stay testable.

export type AnswerState = Record<string, AnswerPayload>

export function emptyAnswer(exercise: Exercise): AnswerPayload {
  switch (exercise.type) {
    case 'mcq_multi':
      return { option_ids: [] }
    case 'sentence_order':
      return { order: [] }
    case 'hanzi_trace':
    case 'hanzi_recall':
      return { strokes: [], completed: false, hints_used: 0 }
    case 'speak_repeat':
    case 'read_aloud':
    case 'speak_answer':
      // Nothing is submitted until the learner records or explicitly skips, so a
      // fresh speaking item carries no duration and is NOT answered. Seeding a 0
      // here would let a learner page past every drill without speaking.
      return {}
    default:
      return {}
  }
}

export function isAnswered(exercise: Exercise, answer: AnswerPayload | undefined): boolean {
  if (!answer) return false
  switch (exercise.type) {
    case 'mcq':
      return typeof answer.option_id === 'string' && answer.option_id !== ''
    case 'mcq_multi':
      return Array.isArray(answer.option_ids) && answer.option_ids.length > 0
    case 'true_false':
      return typeof answer.value === 'boolean'
    case 'fill_blank':
    case 'translation':
    case 'short_answer':
      return typeof answer.text === 'string' && answer.text.trim() !== ''
    case 'sentence_order': {
      const tokens = (exercise.prompt.tokens as unknown[] | undefined) ?? []
      return Array.isArray(answer.order) && answer.order.length === tokens.length
    }
    case 'hanzi_trace':
    case 'hanzi_recall': {
      const strokes = (answer.strokes as unknown[] | undefined) ?? []
      return answer.completed === true && strokes.length > 0
    }
    case 'tone_mcq':
    case 'listening_mcq':
      return typeof answer.option_id === 'string' && answer.option_id !== ''
    case 'listening_dictation':
      return typeof answer.text === 'string' && answer.text.trim() !== ''
    case 'speak_repeat':
    case 'read_aloud':
    case 'speak_answer':
      // Answered once the panel has reported a duration — whether that is a real
      // recording or an explicit "I cannot record" skip. A skip is a legitimate
      // answer: the server records it as unrecorded instead of pretending.
      return typeof answer.duration_ms === 'number'
    default:
      return false
  }
}

/**
 * The answer to show a learner after grading, resolved from the result and falling
 * back to the exercise's own prompt where the type puts its answer there.
 *
 * Shared by the in-lesson feedback panel and the end-of-lesson result screen: a
 * learner reviewing a listening drill cannot re-read audio, so the transcript IS the
 * only way they can learn what they missed, and it has to appear wherever results do.
 */
export function modelAnswer(exercise: Exercise, result: AttemptResult): string {
  const feedback = result.feedback ?? {}
  const solution = result.solution ?? {}
  const options = (exercise.prompt.options as { id: string; text: string }[] | undefined) ?? []

  switch (exercise.type) {
    case 'mcq': {
      const id = (solution.correct_id as string | undefined) ?? (feedback.correct_id as string | undefined)
      return options.find((o) => o.id === id)?.text ?? id ?? ''
    }
    case 'tone_mcq': {
      // The tone it picked, and the explanation that names the tones — which is where
      // the sandhi lives for a word whose spoken pattern differs from the written one.
      const id = (solution.correct_id as string | undefined) ?? (feedback.correct_id as string | undefined)
      const option = options.find((o) => o.id === id)?.text ?? (feedback.tones as string | undefined) ?? ''
      const why = (solution.why as string | undefined) ?? (feedback.why as string | undefined) ?? ''
      return [option, why].filter(Boolean).join(' — ')
    }
    case 'listening_mcq': {
      // Both halves matter for a listening item: the sentence that was said (which a
      // learner cannot re-read, so it is the only way to learn what they missed) and
      // the option it corresponds to.
      const id = (solution.correct_id as string | undefined) ?? (feedback.correct_id as string | undefined)
      const option = options.find((o) => o.id === id)?.text ?? id ?? ''
      const transcript = (solution.transcript as string | undefined) ?? (feedback.transcript as string | undefined) ?? ''
      return [transcript, option].filter(Boolean).join(' — ')
    }
    case 'mcq_multi': {
      const ids = (solution.correct_ids as string[] | undefined) ?? (feedback.correct_ids as string[] | undefined) ?? []
      return ids.map((id) => options.find((o) => o.id === id)?.text ?? id).join('、')
    }
    case 'true_false':
      return (solution.correct ?? feedback.correct) ? 'True' : 'False'
    case 'fill_blank': {
      const accepted = (solution.accepted as string[] | undefined) ?? (feedback.accepted as string[] | undefined) ?? []
      return (feedback.assembled as string | undefined) ?? accepted[0] ?? ''
    }
    case 'sentence_order': {
      const tokens = (exercise.prompt.tokens as string[] | undefined) ?? []
      const order = (solution.order as number[] | undefined) ?? (feedback.correct_order as number[] | undefined) ?? []
      return order.map((i) => tokens[i]).join('')
    }
    case 'translation':
    case 'short_answer': {
      const reference = solution.reference as string | undefined
      const accepted = (solution.accepted as string[] | undefined) ?? []
      return reference ?? (feedback.reference as string | undefined) ?? accepted[0] ?? ''
    }
    case 'hanzi_trace':
    case 'hanzi_recall':
      return (solution.character as string | undefined) ?? (feedback.character as string | undefined) ?? ''
    case 'listening_dictation':
      // The transcript is what was said; it is the answer for a dictation item.
      return (solution.transcript as string | undefined) ?? (feedback.transcript as string | undefined) ?? ''
    case 'speak_repeat':
    case 'read_aloud':
    case 'speak_answer':
      return (solution.reference_text as string | undefined) ?? (feedback.reference_text as string | undefined) ?? ''
    default:
      return ''
  }
}

/** A client_ref must be unique per attempt so a retried submit is idempotent. */
export function newClientRef(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) return crypto.randomUUID()
  return `ref-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

// --- Hanzi quiz bookkeeping -------------------------------------------------
// hanzi-writer reports stroke-level outcomes as the learner draws. We collect them
// and hand the summary to the server, which owns the actual grading: the client is
// never trusted to declare its own correctness.

export type StrokeReport = {
  index: number
  mistaken?: boolean
  order_mismatch?: boolean
  direction_error?: boolean
  out_of_bounds?: boolean
}

export function hanziPayload(character: string, strokeCount: number, strokes: StrokeReport[], completed: boolean, hintsUsed: number): AnswerPayload {
  return {
    character,
    stroke_count: strokeCount,
    strokes,
    completed,
    hints_used: hintsUsed,
  }
}
