import { BackToPractice } from '../../components/BackToPractice'
import { useEffect, useRef, useState } from 'react'
import { PenLine, RotateCcw } from 'lucide-react'
import { useAnswerStudio, useStudioCharacters, useStudioQueue } from '../../api/hooks'
import type { AnswerPayload, AttemptResult, StudyDrill } from '../../api/types'
import { hanziPayload, type StrokeReport } from '../../lib/answers'
import { Button, Card, Chip, EmptyState, ErrorNote, Spinner } from '../../components/ui'
import { usePrefs } from '../../stores/prefs'
import { notifyGraded } from '../../lib/sound'
import { StudioCanvas, type StudioStroke } from './StudioCanvas'

// The writing studio: a large canvas for practising characters outside the path.
//
// Every character here is a real drill from the curriculum (hanzi_trace or
// hanzi_recall), and every attempt is graded by the real server grader with
// mode "studio": recorded, but moving no lesson, no XP, no mastery, no streak —
// which the page states plainly, because the cost of ungated practice is a fact
// the learner should know, not a surprise.

const LEVEL_CHOICES = [
  { value: 0, label: 'All' },
  { value: 1, label: 'HSK 1' },
  { value: 2, label: 'HSK 2' },
  { value: 3, label: 'HSK 3' },
  { value: 4, label: 'HSK 4' },
  { value: 5, label: 'HSK 5' },
]

export function WritePage() {
  const [level, setLevel] = useState<number | null>(null)
  const [character, setCharacter] = useState('')
  const [draft, setDraft] = useState('')
  const queue = useStudioQueue('writing', level, character)
  // The picker lists the level the chips show: "All" until a level is chosen.
  const characters = useStudioCharacters(level ?? 0)
  const answer = useAnswerStudio()
  const prefs = usePrefs()

  const [session, setSession] = useState<{ drills: StudyDrill[]; sequence: number } | null>(null)
  const [index, setIndex] = useState(0)
  const [results, setResults] = useState<Record<string, AttemptResult>>({})
  const sequence = useRef(0)

  // A session starts when the server has drills; a refetch never reshuffles mid-session.
  useEffect(() => {
    if (session !== null || !queue.data) return
    if (queue.data.items.length === 0) return
    sequence.current += 1
    setSession({ drills: queue.data.items, sequence: sequence.current })
    setIndex(0)
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

  // Searching a character deals that character's drills, from any level.
  const chooseCharacter = (value: string) => {
    setCharacter(value)
    setDraft(value)
    setSession(null)
  }

  if (queue.isLoading) return <Spinner label="Loading writing drills" />
  if (queue.error) return <ErrorNote>{(queue.error as Error).message}</ErrorNote>
  if (!queue.data) return null

  const drills = session?.drills ?? []
  const current = drills[index]
  const finished = session !== null && index >= drills.length
  const correct = Object.values(results).filter((r) => r.is_correct === true).length

  const submit = (drill: StudyDrill, strokes: StudioStroke[], completed: boolean) => {
    const reports: StrokeReport[] = strokes.map((s) => ({ index: s.index, mistaken: s.mistaken }))
    const payload: AnswerPayload = hanziPayload(promptCharacter(drill), promptStrokeCount(drill), reports, completed, hintsOf(drill, strokes))
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
            <PenLine size={26} aria-hidden className="text-brand-600" />
            写字 <span className="text-lg font-bold text-ink-3">writing studio</span>
          </h1>
          <p className="text-sm text-ink-2">
            A large canvas for stroke order. Nothing here moves your path and nothing here earns XP — this is practice,
            recorded for you to see.
          </p>
        </div>
        <form
          className="flex items-center gap-2"
          onSubmit={(event) => {
            event.preventDefault()
            chooseCharacter(draft.trim())
          }}
        >
          <label className="flex flex-1 flex-col gap-1 text-sm font-bold text-ink-2">
            Practise a character
            <input
              className="input hanzi"
              value={draft}
              maxLength={4}
              placeholder="好"
              onChange={(event) => setDraft(event.target.value)}
            />
          </label>
          <Button type="submit" variant="secondary" className="self-end">
            Find
          </Button>
          {character ? (
            <Button variant="ghost" onClick={() => chooseCharacter('')} className="self-end">
              Clear
            </Button>
          ) : null}
        </form>
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

      {/* Every character on the level's list, in the order it is taught: pick one to practise it. */}
      {characters.data && characters.data.items.length > 0 ? (
        <section aria-label="Characters" className="flex flex-col gap-2">
          <p className="text-xs font-extrabold uppercase tracking-widest text-ink-3">
            {characters.data.items.length} characters · tap one to practise it
          </p>
          <div className="grid max-h-72 grid-cols-5 gap-2 overflow-y-auto rounded-card border-2 border-line bg-surface p-2 sm:grid-cols-8">
            {characters.data.items.map((item) => {
              const picked = item.character === character
              return (
                <button
                  key={item.character}
                  type="button"
                  aria-pressed={picked}
                  aria-label={`${item.character} ${item.pinyin}`.trim()}
                  onClick={() => chooseCharacter(picked ? '' : item.character)}
                  className={`flex min-h-14 flex-col items-center justify-center rounded-tile border-2 border-b-4 px-1 py-1 transition active:translate-y-px ${
                    picked ? 'border-brand-400 border-b-brand-600 bg-brand-50' : 'border-line bg-surface hover:border-line-strong'
                  }`}
                >
                  <span className="hanzi text-2xl leading-none text-ink">{item.character}</span>
                  {item.pinyin ? <span className="mt-0.5 text-[0.65rem] font-bold text-ink-3">{item.pinyin}</span> : null}
                </button>
              )
            })}
          </div>
        </section>
      ) : null}

      {queue.data.items.length === 0 && session === null ? (
        <EmptyState
          icon={<PenLine size={22} aria-hidden />}
          title={character ? `No writing drill for ${character}` : 'No writing drills at this level'}
          hint={
            character
              ? 'Only characters the curriculum teaches have a drill. Clear the search to deal a mixed round.'
              : 'Try another level — level 1 has a writing drill for every character it teaches.'
          }
        />
      ) : null}

      {current && !finished ? (
        <Card className="flex flex-col gap-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Chip tone="neutral">
                {index + 1} of {drills.length} · HSK {current.level}
              </Chip>
              <Chip tone="accent">{current.type === 'hanzi_recall' ? 'from memory' : 'guided'}</Chip>
            </div>
            {results[current.id] ? (
              <Chip tone={results[current.id].is_correct ? 'positive' : 'danger'}>
                {results[current.id].is_correct ? 'correct' : 'not quite'}
              </Chip>
            ) : null}
          </div>

          {/* The prompt: the character at display size, its pinyin when the learner wants it. */}
          <div className="flex flex-col items-center gap-3">
            <p className="hanzi text-7xl font-bold text-ink">{promptCharacter(current)}</p>
            {prefs.pinyinVisible && promptPinyin(current) ? (
              <p className="text-sm text-ink-3">{promptPinyin(current)}</p>
            ) : null}
          </div>

          <StudioCanvas
            character={promptCharacter(current)}
            strokeCount={promptStrokeCount(current)}
            mode={current.type === 'hanzi_recall' ? 'recall' : 'guided'}
            outlineColor="#c6ccd6"
            onStrokesChange={(strokes) => submitOnComplete(current, strokes)}
            onCompleted={() => {
              /* completion is reported through onStrokesChange with completed=true */
            }}
          />

          {results[current.id] ? (
            <Button onClick={() => setIndex((i) => i + 1)} className="self-end">
              {index + 1 === drills.length ? 'See how you did' : 'Next character'}
            </Button>
          ) : null}
        </Card>
      ) : null}

      {answer.error ? <ErrorNote>{(answer.error as Error).message}</ErrorNote> : null}

      {finished ? (
        <Card className="flex flex-col items-center gap-4 text-center">
          <p className="text-2xl font-extrabold text-ink">
            {correct} / {drills.length}
          </p>
          <p className="text-sm text-ink-2">
            {correct === drills.length
              ? 'Every character in this session — well done.'
              : `${drills.length - correct} to work on. Another round deals new characters.`}
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

  function submitOnComplete(drill: StudyDrill, strokes: StudioStroke[]) {
    // The canvas reports stroke by stroke; the submit happens when the character
    // is completed (the payload's own `completed` flag), which is the same shape
    // a lesson drill submits.
    const completed = strokes.length >= promptStrokeCount(drill) && strokes.length > 0
    submit(drill, strokes, completed)
  }

  function hintsOf(_drill: StudyDrill, strokes: StudioStroke[]): number {
    return strokes.filter((s) => s.mistaken).length >= 3 ? 1 : 0
  }
}

/** The character a writing drill asks for, from its own prompt. */
function promptCharacter(drill: StudyDrill): string {
  return String((drill.prompt as Record<string, unknown>).character ?? '')
}

/** The stroke count the drill's own prompt carries. */
function promptStrokeCount(drill: StudyDrill): number {
  return Number((drill.prompt as Record<string, unknown>).stroke_count ?? 0)
}

/** The pinyin a writing drill's prompt carries, if any. */
function promptPinyin(drill: StudyDrill): string {
  return String((drill.prompt as Record<string, unknown>).pinyin ?? '')
}
