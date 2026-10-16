import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, Check, X } from 'lucide-react'
import { useProficiencyPaper, useSubmitProficiency } from '../../api/hooks'
import type { AnswerPayload, ProficiencySubmitResponse } from '../../api/types'
import { emptyAnswer, isAnswered } from '../../lib/answers'
import { Button, Card, Chip, ErrorNote, ProgressBar, Spinner } from '../../components/ui'
import { ExerciseCard } from '../lesson/ExerciseCard'
import { usePrefs } from '../../stores/prefs'

/**
 * The paper.
 *
 * Exam behaviour throughout: no feedback per answer and nothing revealed until the whole
 * paper is submitted — the server strips solutions until then, so the page could not show
 * the answer mid-paper even if it wanted to.
 *
 * The paper is drawn AND stored by the server before it is sat, and every drawn question
 * must be answered before submitting. That is what makes the pass mean something: a client
 * cannot answer the twenty questions it likes best and call that a paper.
 */
export function PaperRoute() {
  const { level } = useParams<{ level: string }>()
  const navigate = useNavigate()
  const parsed = Number(level)
  if (!Number.isInteger(parsed) || parsed < 1 || parsed > 9) {
    return <ErrorNote>That is not a level.</ErrorNote>
  }
  return <PaperPage level={parsed} onLeave={() => navigate('/proficiency')} />
}

export function PaperPage({ level, onLeave }: { level: number; onLeave: () => void }) {
  const prefs = usePrefs()
  const paper = useProficiencyPaper(level)
  const submit = useSubmitProficiency(level)
  const [index, setIndex] = useState(0)
  const [answers, setAnswers] = useState<Record<string, AnswerPayload>>({})
  const [result, setResult] = useState<ProficiencySubmitResponse | null>(null)

  // The paper is fetched once and held: refetching would draw a different one and throw
  // away the answers already given.
  const questions = useMemo(() => paper.data?.questions ?? [], [paper.data])

  useEffect(() => {
    setIndex(0)
    setAnswers({})
    setResult(null)
  }, [paper.data?.paper_id])

  if (paper.isLoading) return <Spinner label="Drawing your paper" />
  if (paper.error) return <ErrorNote>{(paper.error as Error).message}</ErrorNote>
  if (!paper.data) return null

  if (result) {
    const scored = result.paper
    return (
      <div className="mx-auto flex w-full max-w-2xl flex-col gap-4">
        <Card className="animate-rise text-center" aria-live="polite">
          <span
            className={`mx-auto grid h-20 w-20 place-items-center rounded-full text-white shadow-[0_5px_0_rgba(0,0,0,0.18)] ${
              scored.passed ? 'bg-success-600' : 'bg-danger-600'
            }`}
            aria-hidden
          >
            {scored.passed ? <Check size={42} /> : <X size={42} />}
          </span>
          <h1 className="mt-4 text-3xl font-extrabold tracking-tight text-ink">
            {scored.passed ? `HSK ${level} passed` : 'Not this time'}
          </h1>
          <p className="mt-1 text-2xl font-extrabold text-ink">
            {scored.correct_count} / {scored.graded_count}{' '}
            <span className="text-lg font-bold text-ink-2">({scored.percent}%)</span>
          </p>
          <p className="mt-1 text-sm text-ink-2">
            {scored.passed
              ? result.unlocked_now
                ? `Level ${level} is open — its lessons are available from the path.`
                : `Level ${level} was already open; this paper is recorded too.`
              : `You needed ${Math.round(scored.pass_mark * 100)}%. Nothing changed — sit another paper whenever you like.`}
          </p>
          <div className="mt-6 flex flex-col gap-2 sm:flex-row sm:justify-center">
            {scored.passed ? (
              <Button onClick={onLeave} className="sm:min-w-48">
                Back to the path
              </Button>
            ) : (
              <>
                <Button
                  onClick={() => {
                    void paper.refetch().then(() => {
                      setResult(null)
                      setIndex(0)
                      setAnswers({})
                    })
                  }}
                  className="sm:min-w-48"
                >
                  Try another paper
                </Button>
                <Button variant="secondary" onClick={onLeave}>
                  Later
                </Button>
              </>
            )}
          </div>
        </Card>
      </div>
    )
  }

  const current = questions[index]
  if (!current) return <ErrorNote>This paper has no questions.</ErrorNote>
  const answeredCount = questions.filter((q) => isAnswered(q, answers[q.id])).length
  const isLast = index === questions.length - 1

  const submitPaper = async () => {
    const payload = questions
      .filter((q) => isAnswered(q, answers[q.id]))
      .map((q) => ({
        exercise_id: q.id,
        client_ref: `${paper.data.paper_id}:${q.id}`,
        response: answers[q.id] ?? emptyAnswer(q),
      }))
    const res = await submit.mutateAsync({ paper_id: paper.data.paper_id, answers: payload })
    setResult(res)
  }

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-5">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs font-bold uppercase tracking-wide text-ink-3">HSK {level} test-out</p>
          <h1 className="text-xl font-extrabold text-ink">{paper.data.level.title}</h1>
        </div>
        <Chip tone="warning">No answers until you submit</Chip>
      </header>

      <div className="flex flex-col gap-1.5">
        <ProgressBar
          value={(index + 1) / questions.length}
          tone="reward"
          label={`Question ${index + 1} of ${questions.length}`}
        />
        <p className="text-center text-xs font-bold text-ink-3">
          {index + 1} / {questions.length} · {answeredCount} answered
        </p>
      </div>

      <section className="flex flex-col gap-4 py-2">
        <ExerciseCard
          exercise={current}
          answer={answers[current.id]}
          showPinyin={prefs.pinyinVisible}
          onChange={(payload) => setAnswers((a) => ({ ...a, [current.id]: payload }))}
        />
        {submit.error ? <ErrorNote>{(submit.error as Error).message}</ErrorNote> : null}
      </section>

      <div className="flex items-center gap-3">
        <Button
          variant="secondary"
          onClick={() => setIndex((i) => Math.max(0, i - 1))}
          disabled={index === 0}
          aria-label="Previous question"
          className="shrink-0"
        >
          <ArrowLeft size={16} aria-hidden />
        </Button>
        {isLast ? (
          <Button
            onClick={submitPaper}
            disabled={submit.isPending || answeredCount !== questions.length}
            className="flex-1"
          >
            {submit.isPending
              ? 'Marking…'
              : answeredCount === questions.length
                ? 'Submit paper'
                : `${questions.length - answeredCount} left to answer`}
          </Button>
        ) : (
          <Button
            onClick={() => setIndex((i) => i + 1)}
            disabled={!isAnswered(current, answers[current.id])}
            className="flex-1"
          >
            Continue
          </Button>
        )}
      </div>

      <p className="text-center text-xs text-ink-3">
        Every question is answered before the paper is marked, and the pass is decided from the whole paper — answering
        only the questions you like is not a paper.
      </p>
    </div>
  )
}
