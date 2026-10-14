import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, Check, Sparkles } from 'lucide-react'
import { useLesson, useSubmitAttempts } from '../../api/hooks'
import type { AnswerPayload, AttemptInput, AttemptResult, Exercise, SubmitSummary } from '../../api/types'
import { emptyAnswer, isAnswered, modelAnswer } from '../../lib/answers'
import { describeGrader, formatPercent } from '../../lib/labels'
import { Button, Card, Chip, ErrorNote, ProgressBar, ProgressRing, Spinner } from '../../components/ui'
import { useLessonDraft, usePrefs } from '../../stores/prefs'
import { ExerciseCard } from './ExerciseCard'

export function LessonPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { data: lesson, isLoading, error } = useLesson(id)
  const draft = useLessonDraft()
  const prefs = usePrefs()
  const [mode, setMode] = useState<'practice' | 'exam'>('practice')
  const [summary, setSummary] = useState<SubmitSummary | null>(null)

  const submit = useSubmitAttempts(id ?? '', mode)

  // Time per exercise feeds the reading-time and minutes-learned rollups.
  const exerciseStartedAt = useRef<number>(Date.now())
  const lessonStartedAt = useRef<number>(Date.now())

  useEffect(() => {
    if (!lesson) return
    draft.begin(
      lesson.id,
      lesson.exercises.map((e) => e.id),
    )
    lessonStartedAt.current = Date.now()
    exerciseStartedAt.current = Date.now()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lesson?.id])

  useEffect(() => {
    exerciseStartedAt.current = Date.now()
  }, [draft.index])

  const current = lesson?.exercises[draft.index]
  const answers = draft.answers
  const resultsByExercise = useMemo(() => {
    const map = new Map<string, AttemptResult>()
    for (const result of summary?.results ?? []) map.set(result.exercise_id, result)
    return map
  }, [summary])

  if (isLoading) return <Spinner label="Opening the lesson" />
  if (error) return <ErrorNote>{(error as Error).message}</ErrorNote>
  if (!lesson || !current) return <ErrorNote>This lesson has no exercises yet.</ErrorNote>

  if (lesson.state === 'locked') {
    return (
      <Card className="mx-auto max-w-xl text-center">
        <span className="mx-auto grid h-14 w-14 place-items-center rounded-full bg-surface-2 text-ink-2" aria-hidden>
          🔒
        </span>
        <h1 className="mt-3 text-xl font-extrabold text-ink">{lesson.title}</h1>
        <p className="mt-2 text-ink-2">
          This lesson is locked. Finish the earlier lessons in the path first — the server enforces this, so submitting
          here would be refused.
        </p>
        <Link to="/path" className="mt-4 inline-block text-sm font-bold text-brand-700">
          ← Back to the path
        </Link>
      </Card>
    )
  }

  const answerFor = (exerciseId: string) => answers[exerciseId]
  const setAnswer = (lesson.exercises[draft.index] ? (payload: AnswerPayload) => draft.answer(current.id, payload) : () => undefined)

  const finish = async () => {
    const attempts: AttemptInput[] = lesson.exercises
      .filter((ex) => isAnswered(ex, answers[ex.id]))
      .map((ex, i) => ({
        exercise_id: ex.id,
        client_ref: draft.clientRefs[ex.id] ?? `${lesson.id.slice(0, 8)}-${ex.id.slice(0, 8)}-${i}`,
        response: answers[ex.id] ?? emptyAnswer(ex),
        duration_ms: ex.id === current.id ? Date.now() - exerciseStartedAt.current : 1000,
      }))
    if (attempts.length === 0) return
    const result = await submit.mutateAsync(attempts)
    setSummary(result)
    draft.reset()
  }

  if (summary) {
    return (
      <LessonResult
        summary={summary}
        title={lesson.title}
        exercises={lesson.exercises}
        mode={mode}
        onDone={() => navigate('/path')}
      />
    )
  }

  const isLast = draft.index === lesson.exercises.length - 1
  const answeredCount = lesson.exercises.filter((ex) => isAnswered(ex, answers[ex.id])).length
  const answeredCurrent = isAnswered(current, answerFor(current.id))

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-5">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs font-bold uppercase tracking-wide text-ink-3">
            {lesson.unit_title} · Level {lesson.level_number}
          </p>
          <h1 className="truncate text-xl font-extrabold text-ink">{lesson.title}</h1>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={prefs.togglePinyin}
            className="rounded-full border border-line-strong bg-surface px-3 py-1.5 text-xs font-bold text-ink-2 transition hover:text-ink"
          >
            Pinyin {prefs.pinyinVisible ? 'on' : 'off'}
          </button>
          <Chip tone={mode === 'exam' ? 'warning' : 'accent'}>
            <button type="button" onClick={() => setMode(mode === 'exam' ? 'practice' : 'exam')}>
              {mode === 'exam' ? 'Exam mode: on' : 'Practice mode'}
            </button>
          </Chip>
        </div>
      </header>

      {/* Where the learner is in the lesson, and how far there is to go. */}
      <div className="flex flex-col gap-1.5">
        <ProgressBar
          value={(draft.index + 1) / lesson.exercises.length}
          tone={mode === 'exam' ? 'reward' : 'brand'}
          label={`Question ${draft.index + 1} of ${lesson.exercises.length}`}
        />
        <p className="text-center text-xs font-bold text-ink-3">
          {draft.index + 1} / {lesson.exercises.length} · {answeredCount} answered
        </p>
      </div>

      {lesson.vocabulary.length ? (
        <Card className="flex flex-wrap gap-4">
          {lesson.vocabulary.map((word) => (
            <div key={word.id} className="flex flex-col items-center">
              {prefs.pinyinVisible ? <span className="text-xs text-ink-3">{word.pinyin}</span> : null}
              <span className="hanzi text-2xl">{word.simplified}</span>
              <span className="max-w-24 text-center text-[11px] text-ink-3">{word.senses?.[0]?.en}</span>
            </div>
          ))}
        </Card>
      ) : null}

      {lesson.passages.map((passage) => (
        <Card key={passage.id}>
          <h2 className="hanzi text-lg font-extrabold text-ink">{passage.title}</h2>
          {passage.instruction ? <p className="mt-1 text-sm text-ink-2">{passage.instruction}</p> : null}
          <div className="mt-3 flex flex-col gap-2">
            {passage.sentences.map((sentence, i) => (
              <div key={i} className="rounded-tile bg-surface-2 px-3 py-2">
                <p className="hanzi text-xl">{sentence.zh}</p>
                {prefs.pinyinVisible && sentence.pinyin ? <p className="text-xs text-ink-3">{sentence.pinyin}</p> : null}
                <p className="text-xs text-ink-2">{sentence.en}</p>
              </div>
            ))}
          </div>
        </Card>
      ))}

      <Card>
        <ExerciseCard
          exercise={current}
          answer={answerFor(current.id)}
          showPinyin={prefs.pinyinVisible}
          result={mode === 'practice' ? resultsByExercise.get(current.id) : undefined}
          onChange={setAnswer}
        />
        {submit.error ? <div className="mt-3"><ErrorNote>{(submit.error as Error).message}</ErrorNote></div> : null}
      </Card>

      {/* The one action bar, pinned within thumb reach. On a phone the app's bottom
          navigation also lives down here, so the bar sits directly above it. */}
      <div className="sticky bottom-[calc(4rem+env(safe-area-inset-bottom))] z-10 -mx-4 mt-1 border-t border-line bg-paper/95 px-4 py-3 backdrop-blur lg:bottom-0">
        <div className="flex items-center justify-between gap-3">
          <Button variant="ghost" onClick={() => draft.goto(Math.max(0, draft.index - 1))} disabled={draft.index === 0}>
            <ArrowLeft size={16} aria-hidden /> Back
          </Button>
          {isLast ? (
            <Button onClick={finish} disabled={submit.isPending || answeredCount === 0} className="min-w-40">
              {submit.isPending ? 'Checking…' : mode === 'exam' ? 'Submit paper' : 'Finish lesson'}
            </Button>
          ) : (
            <Button
              onClick={() => draft.goto(draft.index + 1)}
              disabled={!answeredCurrent}
              title={answeredCurrent ? undefined : 'Answer this one first'}
              className="min-w-32"
            >
              Next →
            </Button>
          )}
        </div>
      </div>

      <p className="text-center text-xs text-ink-3">
        {mode === 'exam'
          ? 'Exam mode withholds every answer and explanation until you submit — the server strips them.'
          : 'Answers are graded on the server. Free-form writing is keyword-checked and speaking is not judged at all — both are flagged for review rather than marked right or wrong.'}
      </p>
    </div>
  )
}

function LessonResult({
  summary,
  title,
  exercises,
  mode,
  onDone,
}: {
  summary: SubmitSummary
  title: string
  exercises: Exercise[]
  mode: 'practice' | 'exam'
  onDone: () => void
}) {
  // Answers are looked up by exercise id so the result screen can show what the
  // correct answer was: the API returns answers, not prompts.
  const exercisesByID = new Map(exercises.map((exercise) => [exercise.id, exercise]))
  const outcome = summary.lessons[0]
  const graded = summary.results.filter((r) => r.is_correct !== null)
  const correct = graded.filter((r) => r.is_correct).length
  const needsReview = summary.results.filter((r) => r.needs_review).length
  const completedNow = outcome?.completed_now === true
  const accuracy = graded.length > 0 ? correct / graded.length : 0

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-4">
      {/* The finish line. It states what happened in words and numbers, so the
          celebration does not depend on animation or colour to be understood. */}
      <Card className="animate-rise text-center" aria-live="polite">
        <span
          className="mx-auto grid h-16 w-16 place-items-center rounded-full bg-success-600 text-white shadow-[0_4px_0_var(--color-success-700)]"
          aria-hidden
        >
          <Check size={34} />
        </span>
        <h1 className="mt-3 text-2xl font-extrabold text-ink">
          {completedNow ? 'Lesson complete! ' : 'Lesson saved '}
          <span className="hanzi">🎉</span>
        </h1>
        <p className="mt-1 text-ink-2">{title}</p>

        <div className="mt-5 flex flex-wrap items-center justify-center gap-6">
          <ProgressRing
            value={accuracy}
            tone="success"
            size={96}
            stroke={9}
            label={`${correct} of ${graded.length} graded answers correct`}
          >
            <span className="text-lg font-extrabold text-ink">{formatPercent(accuracy)}</span>
          </ProgressRing>
          <div className="flex flex-col items-center gap-1">
            <span className="inline-flex items-center gap-1.5 rounded-full bg-reward-100 px-3 py-1.5 text-lg font-extrabold text-reward-700">
              <Sparkles size={18} aria-hidden />
              {completedNow ? `+${outcome?.xp_awarded ?? 0}` : '0'} XP
            </span>
            <span className="text-xs font-bold uppercase tracking-wide text-ink-3">earned this lesson</span>
          </div>
        </div>

        <dl className="mt-5 grid grid-cols-2 gap-3 text-left sm:grid-cols-4">
          <Stat label="Correct" value={`${correct}/${graded.length}`} />
          <Stat label="XP" value={completedNow ? `+${outcome?.xp_awarded ?? 0}` : '0'} />
          <Stat label="Status" value={outcome?.status === 'completed' ? 'completed' : 'in progress'} />
          <Stat label="Lesson score" value={outcome ? formatPercent(outcome.correct / Math.max(1, outcome.total)) : '—'} />
        </dl>

        {needsReview > 0 ? (
          <p className="mt-4 rounded-tile border border-reward-300 bg-reward-50 px-3 py-2 text-left text-sm font-semibold text-reward-700">
            {needsReview} answer{needsReview === 1 ? '' : 's'} could not be checked automatically and{' '}
            {needsReview === 1 ? 'was' : 'were'} recorded for you to review — free-form writing is keyword-checked
            rather than judged, and nothing here can assess your pronunciation. Compare them with the answer shown
            below.
          </p>
        ) : null}
        {mode === 'exam' ? (
          <p className="mt-3 text-left text-sm text-ink-2">
            Exam mode: individual answers and explanations are withheld. Review them in practice mode.
          </p>
        ) : null}

        <div className="mt-5 flex justify-center">
          <Button onClick={onDone} className="min-w-48">
            Back to the path
          </Button>
        </div>
      </Card>

      {mode === 'practice'
        ? summary.results.map((result, i) => {
            const exercise = exercisesByID.get(result.exercise_id)
            // A learner reviewing a listening drill cannot replay it forever, so the
            // answer belongs on this screen; for speaking, so does the sentence saying
            // that nothing about it was judged.
            const answer = exercise ? modelAnswer(exercise, result) : ''
            const limitation = (result.feedback?.limitation as string | undefined) ?? ''
            return (
              <Card key={result.exercise_id}>
                <div className="flex items-center justify-between gap-3">
                  <span className="text-sm font-extrabold text-ink">Question {i + 1}</span>
                  <Chip tone={result.is_correct === null ? 'warning' : result.is_correct ? 'positive' : 'danger'}>
                    {result.is_correct === null ? 'needs review' : result.is_correct ? 'correct' : 'not quite'}
                  </Chip>
                </div>
                <p className="mt-2 text-xs text-ink-3">
                  checked by {describeGrader(result.grader)} · score {formatPercent(result.score)}
                </p>
                {answer ? (
                  <p className="mt-2 text-sm">
                    <span className="text-ink-3">Answer: </span>
                    <span className="hanzi text-lg">{answer}</span>
                  </p>
                ) : null}
                {limitation ? <p className="mt-1 text-sm text-ink-2">{limitation}</p> : null}
                {result.explanation ? <p className="mt-2 text-sm text-ink-2">{result.explanation}</p> : null}
              </Card>
            )
          })
        : null}
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-tile bg-surface-2 px-3 py-2">
      <dt className="text-xs font-bold uppercase tracking-wide text-ink-3">{label}</dt>
      <dd className="text-lg font-extrabold text-ink">{value}</dd>
    </div>
  )
}
