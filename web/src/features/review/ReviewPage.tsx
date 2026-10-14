import { useState } from 'react'
import { Check, RotateCcw, X } from 'lucide-react'
import { useAnswerReview, useOverview, useReviewQueue } from '../../api/hooks'
import { relativeDue } from '../../lib/labels'
import { Button, Card, Chip, EmptyState, ErrorNote, ProgressBar, Spinner } from '../../components/ui'
import { usePrefs } from '../../stores/prefs'

export function ReviewPage() {
  const prefs = usePrefs()
  const queue = useReviewQueue(20)
  const overview = useOverview()
  const answer = useAnswerReview()
  const [revealed, setRevealed] = useState<Record<string, boolean>>({})
  const [lastInterval, setLastInterval] = useState<number | null>(null)
  const [answered, setAnswered] = useState(0)

  if (queue.isLoading) return <Spinner label="Loading your review queue" />
  if (queue.error) return <ErrorNote>{(queue.error as Error).message}</ErrorNote>

  const items = queue.data?.items ?? []
  const dueTotal = queue.data?.due_total ?? 0
  // The deck is dealt as a batch of at most `limit` cards; the session bar tracks
  // this sitting, not the lifetime queue, so it can actually reach 100%.
  const presented = items.length

  const record = (lexemeId: string, correct: boolean) =>
    answer.mutate(
      { lexeme_id: lexemeId, correct },
      {
        onSuccess: (res) => {
          setLastInterval(res.interval_index)
          setAnswered((n) => n + 1)
        },
      },
    )

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-5">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-3xl font-extrabold tracking-tight text-ink">复习</h1>
          <p className="text-sm text-ink-2">
            {dueTotal} cards due · spaced repetition advances each word along an interval ladder
          </p>
        </div>
        <button
          type="button"
          onClick={prefs.togglePinyin}
          className="rounded-full border border-line-strong bg-surface px-3 py-1.5 text-xs font-bold text-ink-2 transition hover:text-ink"
        >
          Pinyin {prefs.pinyinVisible ? 'on' : 'off'}
        </button>
      </header>

      {presented > 0 ? (
        <div className="flex flex-col gap-1.5">
          <ProgressBar
            value={presented > 0 ? answered / presented : 0}
            tone="reward"
            label={`${answered} of ${presented} cards answered this session`}
          />
          <p className="text-center text-xs font-bold text-ink-3">
            {answered} / {presented} answered this session
          </p>
        </div>
      ) : null}

      {items.length === 0 ? (
        <EmptyState
          icon={<Check size={22} aria-hidden />}
          title="Nothing due right now"
          hint="Words you have learned enter the deck when a lesson is completed, then come back on a widening interval."
        />
      ) : null}

      {items.map((item) => {
        const isRevealed = revealed[item.id]
        const meaning = item.senses?.[0]?.en ?? ''
        return (
          <Card key={item.id} className="flex flex-col gap-4">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                {prefs.pinyinVisible || isRevealed ? <p className="text-sm text-ink-3">{item.pinyin}</p> : null}
                <p className="hanzi text-5xl font-bold text-ink">{item.simplified}</p>
                {item.traditional !== item.simplified ? (
                  <p className="hanzi text-lg text-ink-3">{item.traditional}</p>
                ) : null}
              </div>
              <div className="flex flex-col items-end gap-1">
                <Chip tone="accent">{relativeDue(item.due_at)}</Chip>
                <span className="text-xs text-ink-3">
                  interval {item.interval_index} · reps {item.reps} · lapses {item.lapses}
                </span>
              </div>
            </div>

            {isRevealed ? (
              <p className="rounded-tile bg-surface-2 px-3 py-2 text-base font-semibold text-ink">{meaning}</p>
            ) : (
              <Button variant="secondary" onClick={() => setRevealed((r) => ({ ...r, [item.id]: true }))}>
                Show meaning
              </Button>
            )}

            {/* Two answers, two colours, two icons — the same pairing the lesson
                feedback uses, so "I knew it" always looks like a correct answer. */}
            <div className="flex gap-2">
              <Button onClick={() => record(item.lexeme_id, false)} disabled={answer.isPending} variant="danger" className="flex-1">
                <X size={16} aria-hidden /> I didn&apos;t know it
              </Button>
              <Button onClick={() => record(item.lexeme_id, true)} disabled={answer.isPending} variant="success" className="flex-1">
                <Check size={16} aria-hidden /> I knew it
              </Button>
            </div>
          </Card>
        )
      })}

      {answer.error ? <ErrorNote>{(answer.error as Error).message}</ErrorNote> : null}
      {lastInterval !== null ? (
        <p className="flex items-center justify-center gap-1.5 text-center text-xs font-bold text-ink-3">
          <RotateCcw size={14} aria-hidden />
          Card scheduled at interval {lastInterval} · {overview.data?.due_reviews ?? 0} cards still due
        </p>
      ) : null}
    </div>
  )
}
