import { useEffect, useRef, useState } from 'react'
import { AudioLines, RotateCcw, TrendingDown, Trophy } from 'lucide-react'
import { useAnswerTone, useToneQueue } from '../../api/hooks'
import type { AnswerPayload, AttemptResult, ToneDrill, ToneLevelStat, ToneTallyRow } from '../../api/types'
import { Button, Card, Chip, EmptyState, ErrorNote, ProgressBar, Spinner } from '../../components/ui'
import { ExerciseCard } from '../lesson/ExerciseCard'
import { usePrefs } from '../../stores/prefs'
import { notifyGraded } from '../../lib/sound'

// Tone practice: a page that drills tone and nothing else.
//
// Two things it deliberately is not. It is not the path — nothing here moves lesson
// progress, XP, mastery or the streak, which is why the server records these attempts
// with mode "tone" and never recomputes. And it is not a quiz that hides the point: the
// tones are the question, so every answer is followed by the tone it wanted and why,
// and the round ends with which tones the learner actually missed. "You miss the third
// tone" is the finding a tone test exists to produce.

/** 0 is every level; the server picks the learner's own level when the page asks for nothing. */
const LEVEL_CHOICES: { value: number; label: string }[] = [
  { value: 0, label: 'All' },
  { value: 1, label: 'HSK 1' },
  { value: 2, label: 'HSK 2' },
  { value: 3, label: 'HSK 3' },
  { value: 4, label: 'HSK 4' },
  { value: 5, label: 'HSK 5' },
]

type Round = {
  /** Frozen when the round starts: a refetch must never reshuffle questions mid-round. */
  drills: ToneDrill[]
  /** Makes every attempt key unique per round, so a retry is idempotent and a new round is a new attempt. */
  sequence: number
}

export function TonePage() {
  const prefs = usePrefs()
  const [level, setLevel] = useState<number | null>(null)
  const queue = useToneQueue(level)
  const answer = useAnswerTone()

  const [round, setRound] = useState<Round | null>(null)
  const [index, setIndex] = useState(0)
  const [payloads, setPayloads] = useState<Record<string, AnswerPayload>>({})
  const [results, setResults] = useState<Record<string, AttemptResult>>({})
  const [tally, setTally] = useState<ToneTallyRow[]>([])
  const [stats, setStats] = useState<ToneLevelStat[]>([])
  const sequence = useRef(0)

  // A round starts as soon as the server has questions for the chosen level, and never
  // while one is in progress.
  useEffect(() => {
    if (round !== null || !queue.data) return
    if (queue.data.items.length === 0) return
    sequence.current += 1
    setRound({ drills: queue.data.items, sequence: sequence.current })
    setIndex(0)
    setPayloads({})
    setResults({})
    setTally([])
    setStats(queue.data.stats ?? [])
  }, [queue.data, round])

  const chooseLevel = (value: number) => {
    setLevel(value)
    setRound(null) // the next round is dealt from the new level
    setStats([])
  }

  const nextRound = () => {
    setRound(null)
    void queue.refetch()
  }

  if (queue.isLoading) return <Spinner label="Loading tone drills" />
  if (queue.error) return <ErrorNote>{(queue.error as Error).message}</ErrorNote>
  if (!queue.data) return null

  const drills = round?.drills ?? []
  const current = drills[index]
  const finished = round !== null && index >= drills.length
  const correct = Object.values(results).filter((r) => r.is_correct === true).length

  const submit = (drill: ToneDrill, payload: AnswerPayload) => {
    setPayloads((p) => ({ ...p, [drill.id]: payload }))
    answer.mutate(
      {
        exercise_id: drill.id,
        client_ref: `${drill.id}:tone:${round?.sequence ?? 0}`,
        option_id: String(payload.option_id ?? ''),
      },
      {
        onSuccess: (res) => {
          const result = res.results?.[0]
          if (result) {
            setResults((r) => ({ ...r, [drill.id]: result }))
            notifyGraded(result)
          }
          setTally((rows) => mergeTally(rows, res.tone_tally ?? []))
          if (res.stats?.length) setStats(res.stats)
        },
      },
    )
  }

  const weakest = [...tally].filter((t) => t.missed > 0).sort((a, b) => b.missed - a.missed)[0]

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-5">
      <header className="flex flex-col gap-3">
        <div>
          <h1 className="flex items-center gap-2 text-3xl font-extrabold tracking-tight text-ink">
            <AudioLines size={26} aria-hidden className="text-brand-600" />
            声调 <span className="text-lg font-bold text-ink-3">tones</span>
          </h1>
          <p className="text-sm text-ink-2">
            Tone drills on their own. Nothing here moves your path — practice as much as you like.
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Level">
          {LEVEL_CHOICES.map((choice) => {
            // `null` means "whatever the server says my level is", which is the state the
            // page starts in — so the chip that matches the drills actually in hand is
            // highlighted, not a guess at what the default was.
            const selected = level === null ? queue.data.level === choice.value : level === choice.value
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

      {stats.length > 0 ? (
        <div className="flex flex-wrap gap-2 text-xs text-ink-3">
          {stats
            .filter((s) => s.answered > 0)
            .map((s) => (
              <Chip key={s.level} tone="neutral">
                HSK {s.level}: {s.correct}/{s.answered} correct
              </Chip>
            ))}
          {stats.every((s) => s.answered === 0) ? (
            <span>No tone practice yet — the lifetime counts appear here as you go.</span>
          ) : null}
        </div>
      ) : null}

      {round !== null && drills.length > 0 && !finished ? (
        <div className="flex flex-col gap-1.5">
          <ProgressBar
            value={drills.length ? index / drills.length : 0}
            tone="brand"
            label={`${index} of ${drills.length} tone questions this round`}
          />
          <p className="text-center text-xs font-bold text-ink-3">
            {index + 1} / {drills.length} · {correct} correct so far
          </p>
        </div>
      ) : null}

      {queue.data.items.length === 0 && round === null ? (
        <EmptyState
          icon={<AudioLines size={22} aria-hidden />}
          title="No tone drills at this level"
          hint="Try another level — level 1 has a tone drill for every word it teaches."
        />
      ) : null}

      {current && !finished ? (
        <Card className="flex flex-col gap-4">
          <div className="flex items-center justify-between gap-2">
            <Chip tone="neutral">HSK {current.level}</Chip>
            <span className="text-xs text-ink-3">
              {current.type === 'tone_mcq'
                ? 'which tone?'
                : 'which word did you hear?'}
            </span>
          </div>

          <ExerciseCard
            exercise={current}
            answer={payloads[current.id]}
            showPinyin={prefs.pinyinVisible}
            onChange={(payload) => submit(current, payload)}
            result={results[current.id]}
          />

          {results[current.id] ? (
            <Button onClick={() => setIndex((i) => i + 1)} className="self-end">
              {index + 1 === drills.length ? 'See how you did' : 'Next'}
            </Button>
          ) : null}
        </Card>
      ) : null}

      {answer.error ? <ErrorNote>{(answer.error as Error).message}</ErrorNote> : null}

      {finished ? (
        <Card className="flex flex-col items-center gap-4 text-center">
          <Trophy size={30} aria-hidden className="text-reward-600" />
          <div>
            <p className="text-2xl font-extrabold text-ink">
              {correct} / {drills.length}
            </p>
            <p className="text-sm text-ink-2">
              {correct === drills.length
                ? 'Every tone in this round — well done.'
                : `${drills.length - correct} to work on. The tones you missed are below.`}
            </p>
          </div>

          {tally.length > 0 ? (
            <div className="w-full rounded-tile bg-surface-2 px-4 py-3 text-left">
              <p className="text-xs font-semibold uppercase tracking-wide text-ink-3">Your tones this round</p>
              <ul className="mt-2 flex flex-col gap-1.5">
                {tally.map((row) => (
                  <li key={row.tone} className="flex items-center justify-between gap-3 text-sm">
                    <span className="text-ink">{row.label}</span>
                    <span className={row.missed > 0 ? 'font-bold text-danger-700' : 'text-ink-3'}>
                      {row.asked - row.missed}/{row.asked}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}

          {weakest ? (
            <p className="flex items-center gap-2 text-sm font-semibold text-ink-2">
              <TrendingDown size={16} aria-hidden className="text-danger-600" />
              Practice this one: {weakest.label}
            </p>
          ) : null}

          <Button onClick={nextRound}>
            <RotateCcw size={16} aria-hidden /> Another round
          </Button>
        </Card>
      ) : null}
    </div>
  )
}

/** Sums two sessions' tallies, so the round's diagnosis covers every answer given. */
function mergeTally(current: ToneTallyRow[], incoming: ToneTallyRow[]): ToneTallyRow[] {
  const merged = new Map(current.map((row) => [row.tone, { ...row }]))
  for (const row of incoming) {
    const existing = merged.get(row.tone)
    if (existing) {
      existing.asked += row.asked
      existing.missed += row.missed
      continue
    }
    merged.set(row.tone, { ...row })
  }
  return [...merged.values()].sort((a, b) => a.tone.localeCompare(b.tone))
}
