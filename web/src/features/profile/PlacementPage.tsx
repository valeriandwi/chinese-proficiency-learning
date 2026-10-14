import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useSession, useSubmitPlacement, useUpdateMe } from '../../api/hooks'
import { usePlacementQuestions } from '../../api/hooks'
import { Button, Card, Chip, ErrorNote, Spinner } from '../../components/ui'
import { usePrefs } from '../../stores/prefs'

// Placement is a recognition self-check, not a scored test. The API says so in its
// response and the UI repeats it, because a learner who over-claims gets dumped
// into material they cannot read and blames the app.

export function PlacementPage() {
  const session = useSession()
  const prefs = usePrefs()
  const questions = usePlacementQuestions('hsk2.0', 1, 20)
  const submit = useSubmitPlacement()
  const updateMe = useUpdateMe()
  const [known, setKnown] = useState<Record<string, boolean>>({})

  if (questions.isLoading) return <Spinner label="Loading the placement check" />
  if (questions.error) return <ErrorNote>{(questions.error as Error).message}</ErrorNote>

  const items = questions.data?.items ?? []
  const knownIds = Object.entries(known)
    .filter(([, v]) => v)
    .map(([k]) => k)

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold">Where should you start?</h1>
        <p className="mt-1 text-sm text-ink-2">{questions.data?.self_report}</p>
      </div>

      <div className="flex items-center justify-between">
        <span className="text-sm text-ink-2">
          {knownIds.length} of {items.length} marked as already known
        </span>
        <button
          type="button"
          onClick={prefs.togglePinyin}
          className="rounded-lg border border-line-strong bg-surface px-3 py-1.5 text-xs font-semibold"
        >
          Pinyin {prefs.pinyinVisible ? 'on' : 'off'}
        </button>
      </div>

      <div className="grid gap-2 sm:grid-cols-2">
        {items.map((item) => {
          const isKnown = Boolean(known[item.id])
          return (
            <button
              key={item.id}
              type="button"
              onClick={() => setKnown((k) => ({ ...k, [item.id]: !isKnown }))}
              className={`flex items-center justify-between gap-3 rounded-xl border px-4 py-3 text-left transition ${
                isKnown ? 'border-brand-500 bg-brand-50' : 'border-line-strong bg-surface hover:border-brand-300'
              }`}
            >
              <span>
                {prefs.pinyinVisible ? <span className="block text-xs text-ink-3">{item.pinyin}</span> : null}
                <span className="hanzi block text-2xl">{item.simplified}</span>
              </span>
              <span className="max-w-40 text-right text-xs text-ink-2">{item.senses?.[0]?.en}</span>
            </button>
          )
        })}
      </div>

      {submit.error ? <ErrorNote>{(submit.error as Error).message}</ErrorNote> : null}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <Chip tone="warning">
          {session.data?.authenticated && session.data.user.placement_completed_at
            ? 'You have taken this check before; retaking it replaces your placement.'
            : 'This is a self-report, not a scored test.'}
        </Chip>
        <div className="flex gap-2">
          <Link to="/path" className="self-center text-sm text-ink-2">
            Skip for now
          </Link>
          <Button
            disabled={submit.isPending || items.length === 0}
            onClick={async () => {
              const result = await submit.mutateAsync({
                version: 'hsk2.0',
                known_lexeme_ids: knownIds,
                presented: items.length,
              })
              await updateMe.mutateAsync({ target_level: result.recommended })
            }}
          >
            {submit.isPending ? 'Placing you…' : 'Finish placement'}
          </Button>
        </div>
      </div>

      {submit.data ? (
        <Card>
          <h2 className="font-semibold">
            You are placed at HSK {submit.data.recommended} (score {Math.round(submit.data.score * 100)}%)
          </h2>
          <p className="mt-1 text-sm text-ink-2">{submit.data.note}</p>
          <Link to="/path" className="mt-3 inline-block text-sm font-semibold text-brand-700">
            Go to the path →
          </Link>
        </Card>
      ) : null}
    </div>
  )
}
