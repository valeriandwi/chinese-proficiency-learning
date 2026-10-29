import { BackToPractice } from '../../components/BackToPractice'
import { useState } from 'react'
import { BookOpen } from 'lucide-react'
import { useReadingPassages } from '../../api/hooks'
import { Button, Card, Chip, EmptyState, ErrorNote, Spinner } from '../../components/ui'
import { BookmarkButton } from './BookmarkButton'

// The passage reader: real passages from the lessons, read outside the path.
//
// Reading records no attempts — a passage is read, not answered — so this page
// has nothing to exclude and says nothing about XP: it is reading practice,
// honest about being ungraded. Pinyin and translation are on tap rather than
// always-on, because a learner who reads with the translation open is not
// reading; and a word with a translation in the passage's own token data can
// be tapped for its meaning, which is the reader's one discovery.

const LEVEL_CHOICES = [
  { value: 0, label: 'All' },
  { value: 1, label: 'HSK 1' },
  { value: 2, label: 'HSK 2' },
  { value: 3, label: 'HSK 3' },
  { value: 4, label: 'HSK 4' },
  { value: 5, label: 'HSK 5' },
]

type Sentence = {
  zh: string
  pinyin?: string
  en?: string
  tokens?: { zh: string; pinyin?: string; en?: string }[]
}

export function ReadPage() {
  const [level, setLevel] = useState(0)
  const passages = useReadingPassages(level)
  const [revealed, setRevealed] = useState<Record<string, 0 | 1 | 2>>({})
  const [tappedWord, setTappedWord] = useState<{ zh: string; pinyin?: string; en?: string } | null>(null)
  // The sentence last touched is tinted, so a learner never loses their place.
  const [active, setActive] = useState<string | null>(null)

  if (passages.isLoading) return <Spinner label="Loading passages" />
  if (passages.error) return <ErrorNote onRetry={() => void passages.refetch()}>{(passages.error as Error).message}</ErrorNote>

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-5">
      <header className="flex flex-col gap-3">
        <BackToPractice />
        <div>
          <h1 className="flex items-center gap-2 text-3xl font-extrabold tracking-tight text-ink">
            <BookOpen size={26} aria-hidden className="text-brand-600" />
            读书 <span className="text-lg font-bold text-ink-3">reading</span>
          </h1>
          <p className="text-sm text-ink-2">
            Real passages from the lessons. Pinyin and translation are on tap — reading with them always on is not
            reading.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Level">
          {LEVEL_CHOICES.map((choice) => (
            <Button
              key={choice.value}
              variant={level === choice.value ? 'primary' : 'secondary'}
              aria-pressed={level === choice.value}
              onClick={() => setLevel(choice.value)}
              className="px-3 py-1.5 text-xs"
            >
              {choice.label}
            </Button>
          ))}
        </div>
      </header>

      {passages.data?.items.length === 0 ? (
        <EmptyState
          icon={<BookOpen size={22} aria-hidden />}
          title="No reading passages at this level"
          hint="Level 1 has a passage in its final unit — try All or HSK 1. Other levels' passages arrive as their content is authored."
        />
      ) : null}

      {passages.data?.items.map((passage) => {
        const sentences = (passage.sentences ?? []) as Sentence[]
        const help = revealed[passage.id] ?? 0
        return (
          <Card key={passage.id} className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center gap-2">
                <Chip tone="neutral">HSK {passage.level}</Chip>
                <Chip tone="neutral">{passage.word_count} words · {passage.minutes} min</Chip>
              </div>
              {/* Hanzi alone → + pinyin → + English: help is added a step at a time, so a
                  learner can stop at the step that still makes them read. */}
              <div role="group" aria-label="Show" className="inline-flex gap-1 rounded-tile bg-surface-2 p-1">
                {(['Hanzi', '+ Pinyin', '+ English'] as const).map((label, step) => (
                  <button
                    key={label}
                    type="button"
                    aria-pressed={help === step}
                    onClick={() => setRevealed((r) => ({ ...r, [passage.id]: step as 0 | 1 | 2 }))}
                    className={`min-h-9 rounded-tile px-3 text-xs font-extrabold transition ${
                      help === step ? 'bg-surface text-ink shadow-tile' : 'text-ink-2 hover:text-ink'
                    }`}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>

            <h2 className="hanzi text-2xl font-extrabold text-ink">{passage.title}</h2>
            {passage.instruction ? <p className="text-sm text-ink-2">{passage.instruction}</p> : null}

            <div className="flex flex-col gap-2.5">
              {sentences.map((sentence, i) => {
                const key = `${passage.id}:${i}`
                return (
                  <div
                    key={i}
                    onClick={() => setActive(key)}
                    className={`rounded-tile px-4 py-3 ${active === key ? 'bg-reward-50' : 'bg-surface-2'}`}
                  >
                    {sentence.tokens?.length ? (
                      /* Pinyin sits in real <ruby> above each word, so it aligns with the
                         word and a screen reader gets it; each word is a button for its card. */
                      <p className="hanzi flex flex-wrap items-end gap-x-0.5 gap-y-1 text-3xl leading-relaxed text-ink">
                        {sentence.tokens.map((token, j) => (
                          <button
                            key={`${i}-${j}`}
                            type="button"
                            className="rounded-lg px-0.5 transition hover:bg-reward-100"
                            onClick={() => {
                              setActive(key)
                              setTappedWord(token)
                            }}
                          >
                            <ruby>
                              {token.zh}
                              {help >= 1 && token.pinyin ? (
                                <rt className="font-sans text-[0.8rem] font-medium text-ink-3">{token.pinyin}</rt>
                              ) : null}
                            </ruby>
                          </button>
                        ))}
                      </p>
                    ) : (
                      <>
                        <p className="hanzi text-xl leading-relaxed text-ink">{sentence.zh}</p>
                        {help >= 1 && sentence.pinyin ? (
                          <p className="mt-1 text-sm text-ink-3">{sentence.pinyin}</p>
                        ) : null}
                      </>
                    )}
                    {help >= 2 && sentence.en ? <p className="mt-1 text-sm text-ink-2">{sentence.en}</p> : null}
                  </div>
                )
              })}
            </div>
          </Card>
        )
      })}

      {/* The tapped word's meaning, where the passage itself carries it. */}
      {tappedWord ? (
        <Card className="flex items-start justify-between gap-3" data-testid="word-detail">
          <div className="min-w-0">
            <p className="hanzi text-3xl font-bold text-ink">{tappedWord.zh}</p>
            {tappedWord.pinyin ? <p className="mt-1 text-sm text-ink-3">{tappedWord.pinyin}</p> : null}
            {tappedWord.en ? <p className="mt-1 text-sm text-ink-2">{tappedWord.en}</p> : null}
            {!tappedWord.pinyin && !tappedWord.en ? (
              <p className="mt-1 text-sm text-ink-2">This word has no translation in this passage's data.</p>
            ) : null}
          </div>
          <BookmarkButton simplified={tappedWord.zh} />
          <Button variant="ghost" onClick={() => setTappedWord(null)} className="shrink-0 px-2 py-1 text-xs">
            Close
          </Button>
        </Card>
      ) : null}

      <p className="text-center text-xs text-ink-3">
        Reading records nothing — it is practice, not progress. Bookmark a word to find it in the study guide later.
      </p>
    </div>
  )
}
