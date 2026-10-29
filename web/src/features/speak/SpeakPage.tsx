import { BackToPractice } from '../../components/BackToPractice'
import { useEffect, useRef, useState } from 'react'
import { Mic, RotateCcw } from 'lucide-react'
import { useAnswerStudio, useStudioQueue } from '../../api/hooks'
import type { AnswerPayload, AttemptResult, StudyDrill } from '../../api/types'
import { isAnswered } from '../../lib/answers'
import { Button, Card, Chip, EmptyState, ErrorNote, Spinner } from '../../components/ui'
import { ExerciseCard } from '../lesson/ExerciseCard'
import { usePrefs } from '../../stores/prefs'
import { notifyGraded } from '../../lib/sound'

// The speaking studio: speak outside the path, from any level.
//
// Every drill here is a real speaking exercise from the curriculum, and the
// result is honest: speaking is never judged for pronunciation without a
// provider — the recording stays in the browser, and the result says so. Studio
// attempts are recorded with mode "studio" and move no lesson, no XP, no
// mastery, no streak.

const LEVEL_CHOICES = [
  { value: 0, label: 'All' },
  { value: 1, label: 'HSK 1' },
  { value: 2, label: 'HSK 2' },
  { value: 3, label: 'HSK 3' },
  { value: 4, label: 'HSK 4' },
  { value: 5, label: 'HSK 5' },
]

export function SpeakPage() {
  const [level, setLevel] = useState<number | null>(null)
  const queue = useStudioQueue('speaking', level)
  const answer = useAnswerStudio()
  const prefs = usePrefs()

  const [session, setSession] = useState<{ drills: StudyDrill[]; sequence: number } | null>(null)
  const [index, setIndex] = useState(0)
  const [payloads, setPayloads] = useState<Record<string, AnswerPayload>>({})
  const [results, setResults] = useState<Record<string, AttemptResult>>({})
  const sequence = useRef(0)

  useEffect(() => {
    if (session !== null || !queue.data) return
    if (queue.data.items.length === 0) return
    sequence.current += 1
    setSession({ drills: queue.data.items, sequence: sequence.current })
    setIndex(0)
    setPayloads({})
    setResults({})
  }, [queue.data, session])

  const nextSession = () => {
    setSession(null)
    void queue.refetch()
  }

  const chooseLevel = (value: number) => {
    setLevel(value)
    setSession(null)
  }

  if (queue.isLoading) return <Spinner label="Loading speaking drills" />
  if (queue.error) return <ErrorNote>{(queue.error as Error).message}</ErrorNote>
  if (!queue.data) return null

  const drills = session?.drills ?? []
  const current = drills[index]
  const finished = session !== null && index >= drills.length

  const submit = (drill: StudyDrill, payload: AnswerPayload) => {
    setPayloads((p) => ({ ...p, [drill.id]: payload }))
    answer.mutate(
      {
        exercise_id: drill.id,
        client_ref: `${drill.id}:studio:${session?.sequence ?? 0}`,
        response: payload,
      },
      {
        onSuccess: (res) => {
          const result = res.results?.[0]
          if (result) {
            setResults((r) => ({ ...r, [drill.id]: result }))
            notifyGraded(result)
          }
        },
      },
    )
  }

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-5">
      <header className="flex flex-col gap-3">
        <BackToPractice />
        <div>
          <h1 className="flex items-center gap-2 text-3xl font-extrabold tracking-tight text-ink">
            <Mic size={26} aria-hidden className="text-brand-600" />
            说话 <span className="text-lg font-bold text-ink-3">speaking studio</span>
          </h1>
          <p className="text-sm text-ink-2">
            Speak after the model, read aloud, answer questions aloud. Nothing here moves your path and nothing is
            judged for pronunciation — the recording stays in your browser.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Level">
          {LEVEL_CHOICES.map((choice) => {
            const selected = level === null ? queue.data?.level === choice.value : level === choice.value
            return (
              <Button
                key={choice.value}
                variant={selected ? 'primary' : 'secondary'}
                aria-pressed={selected}
                onClick={() => chooseLevel(choice.value)}
                className="px-3 py-1.5 text-xs"
              >
                {choice.label}
              </Button>
            )
          })}
        </div>
      </header>

      {queue.data.items.length === 0 && session === null ? (
        <EmptyState
          icon={<Mic size={22} aria-hidden />}
          title="No speaking drills at this level"
          hint="Try another level — level 1 has a speaking drill for every phrase it teaches."
        />
      ) : null}

      {current && !finished ? (
        <Card className="flex flex-col gap-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Chip tone="neutral">HSK {current.level}</Chip>
              <Chip tone="accent">{current.type === 'read_aloud' ? 'read aloud' : current.type === 'speak_answer' ? 'answer aloud' : 'repeat after the model'}</Chip>
            </div>
            {results[current.id] ? (
              <Chip tone={results[current.id].needs_review ? 'warning' : 'positive'}>
                {results[current.id].needs_review ? 'recorded for review' : 'recorded'}
              </Chip>
            ) : null}
          </div>

          <ExerciseCard
            exercise={current}
            answer={payloads[current.id]}
            showPinyin={prefs.pinyinVisible}
            onChange={(payload) => submit(current, payload)}
            result={results[current.id]}
          />

          {results[current.id] || isAnswered(current, payloads[current.id]) ? (
            <Button onClick={() => setIndex((i) => i + 1)} className="self-end">
              {index + 1 === drills.length ? 'See how you did' : 'Next'}
            </Button>
          ) : null}
        </Card>
      ) : null}

      {answer.error ? <ErrorNote>{(answer.error as Error).message}</ErrorNote> : null}

      {finished ? (
        <Card className="flex flex-col items-center gap-4 text-center">
          <p className="text-2xl font-extrabold text-ink">{drills.length} spoken</p>
          <p className="text-sm text-ink-2">
            Speaking is not scored without a pronunciation provider — each recording is kept for you to compare against
            the model.
          </p>
          <Button onClick={nextSession}>
            <RotateCcw size={16} aria-hidden /> Another round
          </Button>
        </Card>
      ) : null}

      <p className="text-center text-xs text-ink-3">
        Studio practice is recorded with mode <code>studio</code> and excluded from your path, XP, mastery and streak.
      </p>
    </div>
  )
}
