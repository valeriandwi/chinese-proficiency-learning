import { useMemo } from 'react'
import type { AnswerPayload, AttemptResult, Exercise } from '../../api/types'
import { describeExerciseType, describeGrader, describeSkill, formatPercent } from '../../lib/labels'
import { modelAnswer } from '../../lib/answers'
import { Chip } from '../../components/ui'
import { AudioPanel } from './AudioPanel'
import { HanziPanel } from './HanziPanel'
import { RecorderPanel } from './RecorderPanel'

// One renderer per exercise type. Adding a type means adding a case here and a
// grader on the server; nothing else in the app changes.

type Props = {
  exercise: Exercise
  answer: AnswerPayload | undefined
  disabled?: boolean
  showPinyin: boolean
  result?: AttemptResult
  onChange: (payload: AnswerPayload) => void
}

export function ExerciseCard({ exercise, answer, disabled, showPinyin, result, onChange }: Props) {
  const prompt = exercise.prompt as Record<string, never> & {
    question?: string
    statement?: string
    options?: { id: string; text: string }[]
    sentence?: string
    pinyin?: string
    hint?: string
    word_bank?: string[]
    instruction?: string
    tokens?: string[]
    source?: string
    direction?: string
    character?: string
    mode?: string
    stroke_count?: number
    min_sentences?: number
    // Listening and speaking. `audio_url` is resolved by the server from the stored
    // audio_key, so the client never builds a media path itself.
    audio_url?: string
    audio_key?: string
    replay_limit?: number
    text?: string
    key_points?: string[]
    prep_seconds?: number
    max_seconds?: number
  }

  return (
    <div className="flex flex-col gap-4">
      <header className="flex flex-wrap items-center gap-2">
        <Chip tone="accent">{describeExerciseType(exercise.type)}</Chip>
        <Chip tone={describeSkill(exercise.skill).tone}>{describeSkill(exercise.skill).label}</Chip>
        <Chip>difficulty {exercise.difficulty}/5</Chip>
        {result ? (
          <Chip tone={result.is_correct === null ? 'warning' : result.is_correct ? 'positive' : 'danger'}>
            {result.is_correct === null ? 'needs review' : result.is_correct ? 'correct' : 'not quite'}
          </Chip>
        ) : null}
      </header>

      {prompt.instruction ? <p className="text-sm text-ink-2">{prompt.instruction}</p> : null}

      {renderer(exercise, prompt, answer, disabled, showPinyin, onChange, result)}

      {result ? <Feedback exercise={exercise} result={result} /> : null}
    </div>
  )
}

type PromptShape = {
  question?: string
  statement?: string
  options?: { id: string; text: string }[]
  sentence?: string
  pinyin?: string
  hint?: string
  word_bank?: string[]
  tokens?: string[]
  source?: string
  direction?: string
  character?: string
  mode?: string
  stroke_count?: number
    min_sentences?: number
    // Listening and speaking. `audio_url` is resolved by the server from the stored
    // audio_key, so the client never builds a media path itself.
    audio_url?: string
    audio_key?: string
    replay_limit?: number
    text?: string
    key_points?: string[]
    prep_seconds?: number
    max_seconds?: number
}

function renderer(
  exercise: Exercise,
  prompt: PromptShape,
  answer: AnswerPayload | undefined,
  disabled: boolean | undefined,
  showPinyin: boolean,
  onChange: (payload: AnswerPayload) => void,
  result?: AttemptResult,
) {
  const options = prompt.options ?? []
  const chosen = answer?.option_id as string | undefined
  const multi = (answer?.option_ids as string[] | undefined) ?? []
  const text = (answer?.text as string | undefined) ?? ''

  switch (exercise.type) {
    case 'mcq':
      return <OptionChoices legend={prompt.question} options={options} chosen={chosen} disabled={disabled} result={result} onChange={onChange} />

    case 'listening_mcq':
      return (
        <div className="flex flex-col gap-4">
          <AudioPanel
            url={audioUrlOf(prompt)}
            replayLimit={prompt.replay_limit ?? 3}
            disabled={disabled}
            reveal={result ? revealedTranscript(result) : null}
          />
          <OptionChoices options={options} chosen={chosen} disabled={disabled} result={result} onChange={onChange} />
        </div>
      )

    case 'listening_dictation':
      return (
        <div className="flex flex-col gap-4">
          <AudioPanel
            url={audioUrlOf(prompt)}
            replayLimit={prompt.replay_limit ?? 3}
            disabled={disabled}
            reveal={result ? revealedTranscript(result) : null}
          />
          <label className="flex flex-col gap-2">
            <span className="text-sm text-ink-2">Type what you heard, in Chinese characters</span>
            <input
              aria-label="What you heard"
              value={text}
              disabled={disabled}
              onChange={(e) => onChange({ text: e.target.value })}
              className="hanzi w-72 rounded-xl border border-line-strong px-3 py-2 text-2xl focus:border-brand-500 focus:outline-none"
              placeholder="写你听到的句子…"
            />
          </label>
          {prompt.hint ? <p className="text-sm text-ink-3">Hint: {prompt.hint}</p> : null}
        </div>
      )

    case 'speak_repeat':
      return (
        <div className="flex flex-col gap-4">
          <AudioPanel url={audioUrlOf(prompt)} replayLimit={prompt.replay_limit ?? 3} disabled={disabled} />
          <RecorderPanel
            maxSeconds={prompt.max_seconds ?? 20}
            reference={{ zh: prompt.text ?? '', pinyin: prompt.pinyin }}
            disabled={disabled}
            onChange={onChange}
          />
        </div>
      )

    case 'read_aloud':
      return (
        <RecorderPanel
          maxSeconds={prompt.max_seconds ?? 20}
          reference={{ zh: prompt.text ?? '', pinyin: prompt.pinyin }}
          disabled={disabled}
          onChange={onChange}
        />
      )

    case 'speak_answer': {
      const points = prompt.key_points ?? []
      return (
        <div className="flex flex-col gap-4">
          {prompt.audio_url ? <AudioPanel url={prompt.audio_url} disabled={disabled} /> : null}
          <p className="text-lg font-medium">{prompt.question}</p>
          {points.length ? (
            <div className="rounded-xl bg-surface-2 px-4 py-3">
              <p className="text-xs uppercase tracking-wide text-ink-3">Try to include</p>
              <ul className="mt-1 list-disc pl-5 text-sm text-ink-2">
                {points.map((point) => (
                  <li key={point} className="hanzi">
                    {point}
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
          {prompt.prep_seconds ? (
            <p className="text-sm text-ink-3">You get {prompt.prep_seconds} seconds to think before you record.</p>
          ) : null}
          <RecorderPanel
            maxSeconds={prompt.max_seconds ?? 20}
            reference={{ zh: prompt.question ?? '' }}
            disabled={disabled}
            onChange={onChange}
          />
        </div>
      )
    }

    case 'mcq_multi':
      return (
        <fieldset className="flex flex-col gap-2" disabled={disabled}>
          <legend className="mb-2 text-lg font-medium">{prompt.question}</legend>
          {options.map((option) => {
            const isChosen = multi.includes(option.id)
            const correct = (result?.solution?.correct_ids as string[] | undefined) ?? []
            const isSolution = result ? correct.includes(option.id) : false
            return (
              <button
                key={option.id}
                type="button"
                onClick={() =>
                  onChange({
                    option_ids: isChosen ? multi.filter((id) => id !== option.id) : [...multi, option.id],
                  })
                }
                disabled={disabled}
                className={`rounded-card border-2 px-4 py-3.5 text-left transition active:translate-y-px ${optionClass(isChosen, isSolution, Boolean(result))}`}
              >
                <span className="mr-3 inline-block h-4 w-4 rounded border border-line-strong align-middle" aria-hidden />
                <span className="hanzi text-xl">{option.text}</span>
              </button>
            )
          })}
        </fieldset>
      )

    case 'true_false':
      return (
        <div className="flex flex-col gap-3">
          <p className="hanzi text-2xl">{prompt.statement}</p>
          <div className="flex gap-2">
            {[true, false].map((value) => (
              <button
                key={String(value)}
                type="button"
                onClick={() => onChange({ value })}
                disabled={disabled}
                className={`min-w-28 rounded-card border-2 px-4 py-3 font-extrabold transition active:translate-y-px ${optionClass(
                  answer?.value === value,
                  result ? result.solution?.correct === value : false,
                  Boolean(result),
                )}`}
              >
                {value ? 'True' : 'False'}
              </button>
            ))}
          </div>
        </div>
      )

    case 'fill_blank': {
      const parts = (prompt.sentence ?? '').split('___')
      return (
        <div className="flex flex-col gap-3">
          <p className="hanzi text-3xl">
            {parts[0]}
            <span className="mx-1 inline-block min-w-16 border-b-2 border-brand-500 px-2 text-center align-bottom text-brand-700">
              {text || '\u00a0'}
            </span>
            {parts[1]}
          </p>
          {showPinyin && prompt.pinyin ? <p className="text-sm text-ink-3">{prompt.pinyin}</p> : null}
          <input
            aria-label="Your answer"
            value={text}
            disabled={disabled}
            onChange={(e) => onChange({ text: e.target.value })}
            className="hanzi w-56 rounded-xl border border-line-strong px-3 py-2 text-2xl focus:border-brand-500 focus:outline-none"
            placeholder="type the missing word"
          />
          {prompt.word_bank?.length ? (
            <div className="flex flex-wrap gap-2">
              {prompt.word_bank.map((word) => (
                <button
                  key={word}
                  type="button"
                  disabled={disabled}
                  onClick={() => onChange({ text: word })}
                  className="hanzi rounded-lg border border-line-strong bg-surface px-3 py-1.5 text-xl hover:border-brand-500"
                >
                  {word}
                </button>
              ))}
            </div>
          ) : null}
          {prompt.hint ? <p className="text-sm text-ink-3">Hint: {prompt.hint}</p> : null}
        </div>
      )
    }

    case 'sentence_order': {
      const tokens = prompt.tokens ?? []
      const order = (answer?.order as number[] | undefined) ?? []
      const used = new Set(order)
      return (
        <div className="flex flex-col gap-3">
          <p className="min-h-14 rounded-xl border border-dashed border-line-strong bg-surface p-3 text-2xl">
            {order.length ? (
              order.map((idx, position) => (
                <button
                  key={`${idx}-${position}`}
                  type="button"
                  disabled={disabled}
                  onClick={() => onChange({ order: order.filter((_, i) => i !== position) })}
                  className="hanzi mr-2 hover:text-danger-600"
                >
                  {tokens[idx]}
                </button>
              ))
            ) : (
              <span className="text-base text-ink-3">Tap the words in the right order…</span>
            )}
          </p>
          <div className="flex flex-wrap gap-2">
            {tokens.map((token, idx) => (
              <button
                key={`${token}-${idx}`}
                type="button"
                disabled={disabled || used.has(idx)}
                onClick={() => onChange({ order: [...order, idx] })}
                className={`hanzi rounded-lg border px-3 py-2 text-2xl transition ${
                  used.has(idx) ? 'border-line bg-surface-2 text-ink-4' : 'border-line-strong bg-surface hover:border-brand-500'
                }`}
              >
                {token}
              </button>
            ))}
          </div>
          {showPinyin && prompt.pinyin ? <p className="text-sm text-ink-3">{prompt.pinyin}</p> : null}
        </div>
      )
    }

    case 'translation':
      return (
        <div className="flex flex-col gap-3">
          <p className="text-lg">
            <span className="mr-2 text-sm uppercase tracking-wide text-ink-3">
              {prompt.direction === 'en->zh' ? 'write in Chinese' : 'translate into English'}
            </span>
          </p>
          <p className="hanzi rounded-xl bg-surface-2 px-4 py-3 text-2xl">{prompt.source}</p>
          <textarea
            aria-label="Your translation"
            value={text}
            disabled={disabled}
            rows={2}
            onChange={(e) => onChange({ text: e.target.value })}
            className="hanzi rounded-xl border border-line-strong px-3 py-2 text-xl focus:border-brand-500 focus:outline-none"
            placeholder={prompt.direction === 'en->zh' ? '写中文…' : 'Type your translation'}
          />
          {prompt.hint ? <p className="text-sm text-ink-3">Hint: {prompt.hint}</p> : null}
        </div>
      )

    case 'hanzi_trace':
    case 'hanzi_recall':
      return (
        <HanziPanel
          character={prompt.character ?? ''}
          strokeCount={prompt.stroke_count ?? 0}
          mode={(prompt.mode as 'guided' | 'recall' | 'sentence') ?? 'guided'}
          disabled={disabled}
          onAnswer={onChange}
        />
      )

    case 'short_answer': {
      const chars = [...text].length
      return (
        <div className="flex flex-col gap-3">
          <p className="text-lg font-medium">{prompt.question}</p>
          <textarea
            aria-label="Your answer"
            value={text}
            disabled={disabled}
            rows={4}
            onChange={(e) => onChange({ text: e.target.value })}
            className="hanzi rounded-xl border border-line-strong px-3 py-2 text-xl focus:border-brand-500 focus:outline-none"
            placeholder="写几句中文…"
          />
          <p className="text-xs text-ink-3">
            {chars} characters written
            {prompt.min_sentences ? ` · aim for ${prompt.min_sentences} sentences` : ''}
          </p>
        </div>
      )
    }

    default:
      return <p className="text-sm text-ink-2">This exercise type is not supported by this build yet.</p>
  }
}

/** The server resolves audio_key to a URL so the client never builds a media path. */
function audioUrlOf(prompt: PromptShape): string {
  return prompt.audio_url ?? ''
}

/**
 * The transcript a listening item only receives once an attempt exists. Exam mode
 * withholds both feedback and solution, so this legitimately returns null there.
 */
function revealedTranscript(result: AttemptResult): { zh: string; pinyin?: string } | null {
  const source = { ...(result.feedback ?? {}), ...(result.solution ?? {}) }
  const zh = source.transcript as string | undefined
  if (!zh) return null
  return { zh, pinyin: source.pinyin as string | undefined }
}

/** Shared by mcq and listening_mcq: identical choice behaviour, different prompt. */
function OptionChoices({
  legend,
  options,
  chosen,
  disabled,
  result,
  onChange,
}: {
  legend?: string
  options: { id: string; text: string }[]
  chosen?: string
  disabled?: boolean
  result?: AttemptResult
  onChange: (payload: AnswerPayload) => void
}) {
  return (
    <fieldset className="flex flex-col gap-2" disabled={disabled}>
      {legend ? <legend className="mb-2 text-lg font-medium">{legend}</legend> : null}
      {options.map((option) => {
        const isChosen = chosen === option.id
        const isSolution = result ? (result.solution?.correct_id as string | undefined) === option.id : false
        return (
          <button
            key={option.id}
            type="button"
            onClick={() => onChange({ option_id: option.id })}
            disabled={disabled}
            className={`rounded-card border-2 px-4 py-3.5 text-left transition active:translate-y-px ${optionClass(isChosen, isSolution, Boolean(result))}`}
          >
            <span className="hanzi mr-3 text-xl">{option.text}</span>
          </button>
        )
      })}
    </fieldset>
  )
}

function optionClass(chosen: boolean | undefined, isSolution: boolean, graded: boolean): string {
  if (graded) {
    // The right answer wears the "correct" colour of the design language; what the
    // learner picked and got wrong wears the mistake colour. Same pair as the
    // feedback banner, so the two ends of the screen agree.
    if (isSolution) return 'border-success-300 bg-success-50'
    if (chosen) return 'border-danger-300 bg-danger-50'
    return 'border-line bg-surface'
  }
  return chosen
    ? 'border-brand-500 bg-brand-50'
    : 'border-line-strong bg-surface hover:border-brand-300 hover:bg-surface-2'
}

function Feedback({ exercise, result }: { exercise: Exercise; result: AttemptResult }) {
  const feedback = result.feedback ?? {}
  const solution = result.solution ?? {}
  // The model answer is computed by one shared helper so the lesson feedback and the
  // end-of-lesson result screen can never disagree about what the answer was.
  const model = useMemo(() => modelAnswer(exercise, result), [exercise, result])

  const why = (solution.why as string | undefined) ?? (feedback.why as string | undefined)
  const limitation = feedback.limitation as string | undefined
  // What a speaking check could actually see, stated plainly: a score built from
  // these facts must not read as a pronunciation judgement.
  const window = feedback.expected_seconds as number[] | undefined
  const recorded = feedback.recorded as boolean | undefined
  const selfRating = feedback.self_rating as number | undefined
  const missing = (feedback.missing as string[] | undefined) ?? []

  return (
    <div
      className={`rounded-xl border px-4 py-3 text-sm ${
        result.is_correct === null
          ? 'border-reward-300 bg-reward-50'
          : result.is_correct
            ? 'border-success-300 bg-success-50'
            : 'border-danger-300 bg-danger-50'
      }`}
    >
      <p className="font-semibold">
        {result.is_correct === null
          ? 'Recorded for review'
          : result.is_correct
            ? `Correct (${formatPercent(result.score)})`
            : `Not quite (${formatPercent(result.score)})`}
        <span className="ml-2 font-normal text-ink-3">checked by {describeGrader(result.grader)}</span>
      </p>
      {model ? (
        <p className="mt-1">
          <span className="text-ink-2">Answer: </span>
          <span className="hanzi text-lg">{model}</span>
        </p>
      ) : null}
      {why ? <p className="mt-1 text-ink-2">{why}</p> : null}
      {result.explanation ? <p className="mt-1 text-ink-2">{result.explanation}</p> : null}
      {missing.length ? <p className="mt-1 text-ink-2">Not found in your answer: {missing.join(', ')}</p> : null}
      {window ? (
        <p className="mt-1 text-ink-2">
          <span className="text-ink-3">what was checked: </span>
          {recorded ? 'a recording was received' : 'no recording was received'}
          {`, length window ${window[0]}–${window[1]}s`}
          {selfRating ? `, your own rating ${selfRating}/5` : ''}
        </p>
      ) : null}
      {limitation ? <p className="mt-1 text-ink-2">{limitation}</p> : null}
      {feedback.message ? <p className="mt-1 text-ink-2">{String(feedback.message)}</p> : null}
    </div>
  )
}
