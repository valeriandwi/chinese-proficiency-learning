import { useState } from 'react'
import { BookMarked, Search } from 'lucide-react'
import { useBookmarks, useGrammarNotes, useVocabulary } from '../../api/hooks'
import { Button, Card, Chip, EmptyState, ErrorNote, Skeleton, Spinner } from '../../components/ui'
import { usePrefs } from '../../stores/prefs'
import { BookmarkButton } from '../read/BookmarkButton'

// The study guide: every word in the curriculum, the grammar people wrote, and
// the learner's bookmarks — a reference surface, not a drill.
//
// Its honesty rules: grammar exists only where a person authored it (level 1
// today; levels 2-5 are generated from word lists and carry none — an honest
// empty state, not filler), and a skill with no attempts shows "not yet
// assessed" rather than 0%.

const LEVEL_CHOICES = [
  { value: 0, label: 'All' },
  { value: 1, label: 'HSK 1' },
  { value: 2, label: 'HSK 2' },
  { value: 3, label: 'HSK 3' },
  { value: 4, label: 'HSK 4' },
  { value: 5, label: 'HSK 5' },
]

type Tab = 'vocabulary' | 'grammar' | 'bookmarks'

export function GuidePage() {
  const [tab, setTab] = useState<Tab>('vocabulary')
  const [level, setLevel] = useState(0)
  const [search, setSearch] = useState('')
  const [offset, setOffset] = useState(0)
  const prefs = usePrefs()

  const vocabulary = useVocabulary(level, search, offset)
  const grammar = useGrammarNotes(level)
  const bookmarks = useBookmarks()

  const tabChoices: { value: Tab; label: string }[] = [
    { value: 'vocabulary', label: 'Vocabulary' },
    { value: 'grammar', label: 'Grammar' },
    { value: 'bookmarks', label: 'Bookmarks' },
  ]

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-5">
      <header className="flex flex-col gap-3">
        <div>
          <h1 className="flex items-center gap-2 text-3xl font-extrabold tracking-tight text-ink">
            <BookMarked size={26} aria-hidden className="text-brand-600" />
            学习指南 <span className="text-lg font-bold text-ink-3">study guide</span>
          </h1>
          <p className="text-sm text-ink-2">
            Every word in the curriculum, the grammar people wrote, and your bookmarks.
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Section">
          {tabChoices.map((choice) => (
            <Button
              key={choice.value}
              variant={tab === choice.value ? 'primary' : 'secondary'}
              aria-pressed={tab === choice.value}
              onClick={() => setTab(choice.value)}
              className="px-3 py-1.5 text-xs"
            >
              {choice.label}
            </Button>
          ))}
        </div>
      </header>

      {tab === 'vocabulary' || tab === 'grammar' ? (
        <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Level">
          {LEVEL_CHOICES.map((choice) => (
            <Button
              key={choice.value}
              variant={level === choice.value ? 'primary' : 'secondary'}
              aria-pressed={level === choice.value}
              onClick={() => {
                setLevel(choice.value)
                setOffset(0)
              }}
              className="px-3 py-1.5 text-xs"
            >
              {choice.label}
            </Button>
          ))}
        </div>
      ) : null}

      {tab === 'vocabulary' ? (
        <>
          <label className="flex items-center gap-2 rounded-tile border border-line-strong bg-surface px-3 py-2 shadow-tile">
            <Search size={16} aria-hidden className="shrink-0 text-ink-3" />
            <input
              type="search"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value)
                setOffset(0)
              }}
              placeholder="Search characters, pinyin or meaning…"
              aria-label="Search vocabulary"
              className="w-full bg-transparent text-sm text-ink outline-none placeholder:text-ink-4"
            />
          </label>

          {vocabulary.isLoading ? (
            <div className="flex flex-col gap-2">
              <Skeleton className="h-16" />
              <Skeleton className="h-16" />
              <Skeleton className="h-16" />
            </div>
          ) : vocabulary.error ? (
            <ErrorNote>{(vocabulary.error as Error).message}</ErrorNote>
          ) : (
            <>
              {vocabulary.data?.items.length === 0 ? (
                <EmptyState
                  title={search ? `No words match “${search}”` : 'No words at this level'}
                  hint={search ? 'Try the characters, the pinyin, or an English word.' : 'Choose All to see every level.'}
                />
              ) : (
                <>
                  {vocabulary.data?.items.map((word) => (
                    <VocabularyRow
                      key={`${word.id}-${word.level}`}
                      word={word}
                      showPinyin={prefs.pinyinVisible}
                    />
                  ))}
                  <Pager
                    offset={offset}
                    limit={vocabulary.data?.limit ?? 50}
                    total={vocabulary.data?.total ?? 0}
                    onOffset={setOffset}
                  />
                </>
              )}
            </>
          )}
        </>
      ) : null}

      {tab === 'grammar' ? (
        <>
          {grammar.isLoading ? (
            <Spinner label="Loading grammar notes" />
          ) : grammar.error ? (
            <ErrorNote>{(grammar.error as Error).message}</ErrorNote>
          ) : grammar.data?.items.length === 0 ? (
            <EmptyState
              title="No authored grammar at this level"
              hint="Grammar notes exist where a person wrote them — level 1 today. Levels 2-5 are generated from word lists and carry no grammar yet."
            />
          ) : (
            grammar.data?.items.map((note) => <GrammarCard key={note.id} note={note} />)
          )}
        </>
      ) : null}

      {tab === 'bookmarks' ? (
        <>
          {bookmarks.isLoading ? (
            <Spinner label="Loading bookmarks" />
          ) : bookmarks.error ? (
            <ErrorNote>{(bookmarks.error as Error).message}</ErrorNote>
          ) : bookmarks.data?.items.length === 0 ? (
            <EmptyState
              title="No bookmarks yet"
              hint="Tap Bookmark on a word in the guide or the reader, and it appears here for you to come back to."
            />
          ) : (
            bookmarks.data?.items.map((word) => (
              <VocabularyRow key={`${word.id}-bm`} word={word} showPinyin={prefs.pinyinVisible} />
            ))
          )}
        </>
      ) : null}
    </div>
  )
}

function VocabularyRow({
  word,
  showPinyin,
}: {
  word: { id: string; simplified: string; traditional: string; pinyin: string; senses: { en?: string }[]; level: number; bookmarked: boolean; audio_key?: string }
  showPinyin: boolean
}) {
  return (
    <Card className="flex items-center justify-between gap-4 !p-4">
      <div className="flex min-w-0 items-center gap-4">
        <div className="flex w-24 shrink-0 flex-col items-center">
          <span className="hanzi text-3xl font-bold text-ink">{word.simplified}</span>
          {showPinyin ? <span className="mt-0.5 text-xs text-ink-3">{word.pinyin}</span> : null}
        </div>
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold text-ink">
            {word.senses?.[0]?.en ?? 'No English sense in this data'}
          </p>
          <p className="mt-0.5 text-xs text-ink-3">
            {word.traditional !== word.simplified ? `${word.traditional} · ` : ''}HSK {word.level}
          </p>
        </div>
      </div>
      <BookmarkButton lexemeId={word.id} simplified={word.simplified} bookmarked={word.bookmarked} />
    </Card>
  )
}

function GrammarCard({ note }: { note: import('../../api/types').GrammarNote }) {
  return (
    <Card className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <Chip tone="neutral">HSK {note.level}</Chip>
        <Chip tone="accent">{note.unit_title}</Chip>
        <span className="text-xs text-ink-3">{note.lesson_title}</span>
      </div>
      <p className="text-sm font-semibold text-ink">{note.explanation}</p>
      {note.example ? (
        <div className="rounded-tile bg-surface-2 px-4 py-3">
          <p className="hanzi text-xl text-ink">{note.example}</p>
          {note.example_pinyin ? <p className="mt-1 text-sm text-ink-3">{note.example_pinyin}</p> : null}
        </div>
      ) : null}
    </Card>
  )
}

function Pager({
  offset,
  limit,
  total,
  onOffset,
}: {
  offset: number
  limit: number
  total: number
  onOffset: (offset: number) => void
}) {
  if (total <= limit) return null
  const page = Math.floor(offset / limit) + 1
  const pages = Math.ceil(total / limit)
  return (
    <div className="flex items-center justify-between gap-3" data-testid="vocab-pager">
      <Button variant="secondary" onClick={() => onOffset(Math.max(0, offset - limit))} disabled={offset === 0}>
        ← Previous
      </Button>
      <span className="text-xs font-bold text-ink-3">
        Page {page} of {pages} · {total} words
      </span>
      <Button
        variant="secondary"
        onClick={() => onOffset(Math.min(offset + limit, (pages - 1) * limit))}
        disabled={offset + limit >= total}
      >
        Next →
      </Button>
    </div>
  )
}
