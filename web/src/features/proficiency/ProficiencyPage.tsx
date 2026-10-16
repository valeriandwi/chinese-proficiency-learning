import { Link } from 'react-router-dom'
import { ArrowLeft, BadgeCheck, Play } from 'lucide-react'
import { useProficiencyCards } from '../../api/hooks'
import { Button, Card, Chip, EmptyState, ErrorNote, ProgressBar, Spinner } from '../../components/ui'

/**
 * Test out of a level.
 *
 * The counterpart to placement: placement recommends a level and says so, and the server
 * still refuses a locked lesson's submissions — so a recommendation can never let anyone
 * past the gate. This is the scored way through: a paper drawn across the level, and
 * passing it opens the level.
 *
 * What passing does NOT do is claim the level was learned. No lesson is marked complete
 * and no XP is awarded; the level's lessons become available, and the record says a test
 * put the learner there.
 */
export function ProficiencyPage() {
  const cards = useProficiencyCards()

  if (cards.isLoading) return <Spinner label="Loading the test-out options" />
  if (cards.error) return <ErrorNote>{(cards.error as Error).message}</ErrorNote>

  const levels = cards.data?.cards ?? []
  if (levels.length === 0) {
    return <EmptyState title="No levels to test out of" hint="This curriculum has no levels yet." />
  }
  const passPercent = Math.round((cards.data?.pass_threshold ?? 0.8) * 100)

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-5">
      <header>
        <Link
          to="/progress"
          className="inline-flex items-center gap-1.5 text-xs font-bold text-ink-2 transition hover:text-ink"
        >
          <ArrowLeft size={14} aria-hidden /> Back to progress
        </Link>
        <h1 className="mt-1 text-3xl font-extrabold tracking-tight text-ink">Test out</h1>
        <p className="mt-1 text-sm text-ink-2">
          Already know a level? Sit {cards.data?.paper_size ?? 20} questions drawn across it and score {passPercent}% or
          better to open it. Passing opens the level — it does not mark its lessons as learned, and your streak and XP
          only count what you really answer.
        </p>
      </header>

      {levels.map((card) => (
        <Card key={card.level} className="flex flex-wrap items-center justify-between gap-4">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-lg font-extrabold text-ink">HSK {card.level}</h2>
              {card.unlocked_by_test ? (
                <Chip tone="positive">
                  <BadgeCheck size={13} aria-hidden /> opened by a test
                </Chip>
              ) : null}
              {card.papers_taken > 0 ? (
                <Chip tone={card.passed ? 'positive' : 'neutral'}>
                  {card.passed ? 'passed' : 'not passed yet'} · best {Math.round((card.best_score ?? 0) * 100)}%
                </Chip>
              ) : null}
            </div>
            <p className="mt-0.5 truncate text-sm text-ink-2">{card.title}</p>
            <p className="mt-1 text-xs text-ink-3">
              {card.questions} gradable questions to draw from · {card.papers_taken}{' '}
              {card.papers_taken === 1 ? 'attempt' : 'attempts'} so far
            </p>
            {!card.unlocked_by_test && card.papers_taken > 0 ? (
              <div className="mt-2 max-w-xs">
                <ProgressBar
                  value={card.best_score ?? 0}
                  tone="brand"
                  label={`Best paper at this level: ${Math.round((card.best_score ?? 0) * 100)}%`}
                />
              </div>
            ) : null}
          </div>

          <Link to={`/proficiency/${card.level}`} className="shrink-0">
            <Button variant={card.unlocked_by_test ? 'secondary' : 'primary'}>
              <Play size={16} aria-hidden />
              {card.papers_taken > 0 && !card.passed ? `Retry HSK ${card.level}` : `Take the HSK ${card.level} test`}
            </Button>
          </Link>
        </Card>
      ))}
    </div>
  )
}
