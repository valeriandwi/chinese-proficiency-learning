import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, Flame, Lock, Sparkles } from 'lucide-react'
import { useLesson, useSubmitAttempts } from '../../api/hooks'
import type { AnswerPayload, AttemptInput, AttemptResult, Exercise, SubmitSummary } from '../../api/types'
import { emptyAnswer, isAnswered, modelAnswer } from '../../lib/answers'
import { describeGrader, formatPercent } from '../../lib/labels'
import { Button, Card, Chip, ErrorNote, ProgressBar, ProgressRing, Spinner } from '../../components/ui'
import { ChunkyCheck, ChunkyCross, InkSeal } from '../../components/icons'
import { Confetti } from '../../components/Confetti'
import { useCountUp } from '../../lib/motion'
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
  // Verdicts collected during the run, by exercise. Practice mode fills this one answer
  // at a time (each answer is graded as it is checked, so the band can be the server's
  // verdict rather than this component's opinion); exam mode fills it at the end.
  const [results, setResults] = useState<Record<string, AttemptResult>>({})
  const [combo, setCombo] = useState(0)
  const [lastOutcome, setLastOutcome] = useState<SubmitSummary['lessons'][number] | undefined>()

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
    setResults({})
    setCombo(0)
    setSummary(null)
    setLastOutcome(undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lesson?.id])

  useEffect(() => {
    exerciseStartedAt.current = Date.now()
  }, [draft.index])

  const current = lesson?.exercises[draft.index]
  const answers = draft.answers
  const answeredCount = useMemo(
    () => (lesson ? lesson.exercises.filter((ex) => isAnswered(ex, answers[ex.id])).length : 0),
    [lesson, answers],
  )

  if (isLoading) return <Spinner label="Opening the lesson" />
  if (error) return <ErrorNote>{(error as Error).message}</ErrorNote>
  if (!lesson || !current) return <ErrorNote>This lesson has no exercises yet.</ErrorNote>

  if (lesson.state === 'locked') {
    return (
      <Card className="mx-auto max-w-xl text-center">
        <span className="mx-auto grid h-14 w-14 place-items-center rounded-full bg-surface-2 text-ink-2" aria-hidden>
          <Lock size={24} />
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

  const attemptsFor = (only?: Exercise): AttemptInput[] =>
    lesson.exercises
      .filter((ex) => (only ? ex.id === only.id : isAnswered(ex, answers[ex.id])))
      .map((ex, i) => ({
        exercise_id: ex.id,
        client_ref: draft.clientRefs[ex.id] ?? `${lesson.id.slice(0, 8)}-${ex.id.slice(0, 8)}-${i}`,
        response: answers[ex.id] ?? emptyAnswer(ex),
        duration_ms: ex.id === current.id ? Date.now() - exerciseStartedAt.current : 1000,
      }))

  /** Practice mode: submit this answer alone and show what the server said about it. */
  const check = async () => {
    const outcome = await submit.mutateAsync(attemptsFor(current))
    const result = outcome.results[0]
    if (result) {
      setResults((all) => ({ ...all, [current.id]: result }))
      // Only a definite wrong answer breaks a run; free-form writing awaiting review is
      // not the learner's mistake.
      setCombo((run) => (result.is_correct === false ? 0 : run + 1))
    }
    if (outcome.lessons[0]) setLastOutcome(outcome.lessons[0])
  }

  /** Exam mode: one submission for the whole paper, graded at the end. */
  const finishExam = async () => {
    const attempts = attemptsFor()
    if (attempts.length === 0) return
    const outcome = await submit.mutateAsync(attempts)
    setSummary(outcome)
    draft.reset()
  }

  const advance = () => {
    if (draft.index < lesson.exercises.length - 1) {
      draft.goto(draft.index + 1)
      return
    }
    // Every answer is already in the log, so the celebration is assembled from the
    // verdicts collected during the lesson plus the lesson state the server last sent.
    setSummary({
      results: lesson.exercises.map((ex) => results[ex.id]).filter(Boolean),
      lessons: lastOutcome ? [lastOutcome] : [],
    })
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
  const answeredCurrent = isAnswered(current, answerFor(current.id))
  const currentResult = mode === 'practice' ? results[current.id] : undefined
  // Writing and speaking ask the learner to PRODUCE something, and they may not be able
  // to: a character can be impossible to hand-write today, and a microphone may refuse.
  // Those two can be passed over in practice mode, ungraded — which is what the drill
  // would have been anyway (the server flags them for review rather than judging them).
  // A choice drill cannot be skipped: tapping an option is always possible, and
  // Duolingo does not let you slide past a question you can answer.
  const canSkip = ['hanzi_trace', 'hanzi_recall', 'speak_repeat', 'read_aloud', 'speak_answer', 'short_answer'].includes(
    current.type,
  )

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
          {combo >= 2 ? (
            <span
              key={combo}
              className="animate-pop inline-flex items-center gap-1.5 rounded-full bg-reward-100 px-3 py-1.5 text-sm font-extrabold text-reward-700"
              aria-label={`${combo} correct in a row`}
            >
              <Flame size={16} aria-hidden />×{combo}
            </span>
          ) : null}
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
          value={answeredCount / lesson.exercises.length}
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

      {/* The question is the screen: no card around it, a large prompt, and options as
          full-width rows. Wrapping it in a panel made the drill one block among several
          and shrank the type a learner reads most. */}
      <section key={current.id} className="animate-rise flex flex-col gap-4 py-2">
        <ExerciseCard
          exercise={current}
          answer={answerFor(current.id)}
          showPinyin={prefs.pinyinVisible}
          onChange={setAnswer}
        />
        {submit.error ? <ErrorNote>{(submit.error as Error).message}</ErrorNote> : null}
      </section>

      {/* The action bar is where the verdict arrives, because it is the control that was
          pressed. Practice mode turns it into the band; exam mode keeps it a plain
          action, since a paper must not tell you after every question. */}
      {currentResult ? (
        <VerdictBand result={currentResult} exercise={current} isLast={isLast} onContinue={advance} />
      ) : (
        <div
          data-testid="lesson-action-bar"
          className="sticky bottom-[calc(4rem+env(safe-area-inset-bottom))] z-10 -mx-4 mt-1 border-t border-line bg-paper/95 px-4 py-3 backdrop-blur lg:bottom-0"
        >
          <div className="flex items-center gap-3">
            <Button
              variant="secondary"
              onClick={() => draft.goto(Math.max(0, draft.index - 1))}
              disabled={draft.index === 0}
              aria-label="Previous question"
              className="shrink-0"
            >
              <ArrowLeft size={16} aria-hidden />
            </Button>
            {mode === 'exam' ? (
              isLast ? (
                <Button onClick={() => void finishExam()} disabled={submit.isPending || answeredCount === 0} className="flex-1">
                  {submit.isPending ? 'Checking…' : 'Submit paper'}
                </Button>
              ) : (
                <Button onClick={() => draft.goto(draft.index + 1)} disabled={!answeredCurrent} className="flex-1">
                  Next
                </Button>
              )
            ) : (
              <>
                {/* advance(), not goto(): on the last drill, skipping has to finish the
                    lesson rather than sit on a question it has already passed. */}
                {!answeredCurrent && canSkip ? (
                  <Button
                    variant="secondary"
                    onClick={advance}
                    title="Move on without answering — nothing is recorded for this one"
                    className="shrink-0"
                  >
                    Skip
                  </Button>
                ) : null}
                <Button onClick={() => void check()} disabled={!answeredCurrent || submit.isPending} className="flex-1">
                  {submit.isPending ? 'Checking…' : 'Check'}
                </Button>
              </>
            )}
          </div>
        </div>
      )}

      <p className="text-center text-xs text-ink-3">
        {mode === 'exam'
          ? 'Exam mode withholds every answer and explanation until you submit — the server strips them.'
          : 'Answers are graded on the server. Free-form writing is keyword-checked and speaking is not judged at all — both are flagged for review rather than marked right or wrong.'}
      </p>
    </div>
  )
}

/**
 * The verdict, sliding up from the action bar in the colours of the outcome.
 *
 * The words come first and the colour reinforces them: "Correct!" / "Not quite" /
 * "Saved for you to review" is what a screen reader reads and what a reduced-motion
 * learner sees. A wrong answer also shakes — emphasis, never the message.
 */
export function VerdictBand({
  result,
  exercise,
  isLast,
  onContinue,
}: {
  result: AttemptResult
  exercise: Exercise
  isLast: boolean
  onContinue: () => void
}) {
  const outcome = result.is_correct === null ? 'review' : result.is_correct ? 'correct' : 'wrong'
  const tone = { correct: 'bg-success-700', wrong: 'bg-danger-600', review: 'bg-reward-700' }[outcome]
  const heading = { correct: 'Correct!', wrong: 'Not quite', review: 'Saved for you to review' }[outcome]
  const answer = modelAnswer(exercise, result)
  const limitation = (result.feedback?.limitation as string | undefined) ?? ''

  return (
    <div
      data-testid="lesson-action-bar"
      className={`animate-slide-up sticky bottom-[calc(4rem+env(safe-area-inset-bottom))] z-10 -mx-4 border-t-2 border-white/25 px-4 py-3 text-white lg:bottom-0 ${
        outcome === 'wrong' ? 'animate-shake' : ''
      } ${tone}`}
    >
      <div className="flex items-start gap-3">
        <span className="mt-0.5 shrink-0 rounded-full bg-white/20 p-1.5" aria-hidden>
          {outcome === 'correct' ? (
            <ChunkyCheck className="h-[22px] w-[22px]" />
          ) : (
            <ChunkyCross className="h-[22px] w-[22px]" />
          )}
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-lg font-extrabold">{heading}</p>
          {answer && outcome !== 'review' ? (
            <p className="mt-0.5 text-sm text-white/90">
              <span className="hanzi text-base">{answer}</span>
            </p>
          ) : null}
          {result.explanation ? <p className="mt-1 text-sm text-white/90">{result.explanation}</p> : null}
          {limitation ? <p className="mt-1 text-sm text-white/90">{limitation}</p> : null}
          {/* Who judged it stays visible: a learner should not have to take the verdict on
              faith, and it is how a wrong answer is traceable to its rule. */}
          <p className="mt-1 text-xs text-white/85">
            checked by {describeGrader(result.grader)} · score {formatPercent(result.score)}
          </p>
        </div>
        <Button onClick={onContinue} className="shrink-0 !bg-surface !text-ink !shadow-none hover:!bg-surface-2">
          {isLast ? 'Finish lesson' : 'Continue'}
        </Button>
      </div>
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
  // The number earns its arrival: it counts up to what the server actually awarded.
  const xp = useCountUp(outcome?.xp_awarded ?? 0)

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-4">
      {/* The finish line. It states what happened in words and numbers, so the
          celebration does not depend on animation or colour to be understood. */}
      <Card className="relative animate-rise overflow-hidden text-center" aria-live="polite">
        {completedNow ? <Confetti /> : null}
        <div className="relative">
        {/* The ink seal: our own mark for work finished, and the one decoration in the
            app that is not an icon. */}
        <InkSeal character="汉" size={92} className="mx-auto text-success-700" />
        <h1 className="mt-4 text-3xl font-extrabold tracking-tight text-ink">
          {completedNow ? 'Lesson complete!' : 'Lesson saved'}
        </h1>
        <p className="mt-1 text-ink-2">{title}</p>
        {completedNow && (outcome?.xp_awarded ?? 0) > 0 ? (
          <p className="mt-3 inline-flex items-center gap-2 rounded-full bg-reward-100 px-4 py-2 text-2xl font-extrabold text-reward-700">
            <Sparkles size={22} aria-hidden />+{xp} XP
          </p>
        ) : null}

        {/* Two different numbers live on this screen and they must not be read as a
            contradiction: this ring is the share of answers a grader could judge, while
            "lesson score" below counts every exercise. Saying so is cheaper than a
            learner wondering which one was lying. */}
        <div className="mt-5 flex flex-wrap items-center justify-center gap-6">
          <div className="flex flex-col items-center gap-1">
            <ProgressRing
              value={accuracy}
              tone="success"
              size={96}
              stroke={9}
              label={`${correct} of ${graded.length} graded answers correct`}
            >
              <span className="text-lg font-extrabold text-ink">{formatPercent(accuracy)}</span>
            </ProgressRing>
            <span className="text-xs font-bold uppercase tracking-wide text-ink-3">of graded answers</span>
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

        <div className="mt-6">
          <Button onClick={onDone} className="w-full">
            Continue
          </Button>
        </div>
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
