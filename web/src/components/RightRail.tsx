import { Link } from 'react-router-dom'
import { Flame, NotebookPen, Sparkles, Target } from 'lucide-react'
import { useOverview } from '../api/hooks'
import { describeGoal } from '../lib/labels'
import { goalRatio } from '../lib/goal'
import { Button, ProgressRing, Skeleton, StatTile } from './ui'

/**
 * The desktop right-hand rail: the three numbers that make a learner come back —
 * streak, today's goal, and what is waiting in the review queue.
 *
 * Everything here is read from GET /progress/overview through the same query key
 * Today uses, so the rail costs no extra request and cannot show a different
 * streak than the page beside it. Nothing is invented: if the API does not return
 * it, it is not here.
 */
export function RightRail() {
  const overview = useOverview()

  if (overview.isLoading) {
    return (
      <aside className="hidden w-72 shrink-0 flex-col gap-4 xl:flex" aria-label="Your progress">
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-24 w-full" />
      </aside>
    )
  }

  // A failed progress fetch is not worth shouting about in a side panel; the page
  // itself already reports it. The rail simply steps out of the way.
  if (!overview.data) return null

  const { streak, due_reviews, total_xp, lessons_completed } = overview.data
  const ratio = goalRatio(streak.goal_kind, streak.goal_value, streak.today_minutes, streak.today_xp, lessons_completed)

  return (
    <aside className="hidden w-72 shrink-0 flex-col gap-4 xl:flex" aria-label="Your progress">
      <StatTile
        label="Streak"
        tone="warning"
        icon={<Flame size={20} />}
        value={
          <>
            {streak.current_streak}
            <span className="ml-1 text-base font-bold text-ink-2">day{streak.current_streak === 1 ? '' : 's'}</span>
          </>
        }
        hint={`longest ${streak.longest_streak}`}
      />

      <div className="rounded-card border border-line bg-surface p-5 shadow-tile">
        <p className="text-xs font-semibold uppercase tracking-wide text-ink-3">Today&apos;s goal</p>
        <div className="mt-3 flex items-center gap-4">
          <ProgressRing
            value={ratio}
            tone="reward"
            label={`${Math.round(ratio * 100)}% of today's goal`}
          >
            <Target size={22} className="text-reward-700" aria-hidden />
          </ProgressRing>
          <div className="min-w-0">
            <p className="text-2xl font-extrabold leading-tight text-ink">
              {Math.round(ratio * 100)}%
              <span className="ml-2 text-sm font-bold text-ink-2">{streak.goal_met_today ? 'met' : 'to go'}</span>
            </p>
            <p className="mt-0.5 text-sm text-ink-2">
              {streak.today_minutes} min · {streak.today_xp} XP
            </p>
          </div>
        </div>
        <p className="mt-3 text-xs text-ink-2">{describeGoal(streak.goal_kind, streak.goal_value)}</p>
      </div>

      <div className="rounded-card border border-line bg-surface p-5 shadow-tile">
        <p className="text-xs font-semibold uppercase tracking-wide text-ink-3">Review queue</p>
        <p className="mt-1 flex items-center gap-2 text-2xl font-extrabold text-ink">
          <NotebookPen size={20} className="text-brand-700" aria-hidden />
          {due_reviews}
          <span className="text-sm font-bold text-ink-2">due</span>
        </p>
        {due_reviews > 0 ? (
          <Link to="/reviews" className="mt-3 block">
            <Button variant="secondary" className="w-full">
              Review now
            </Button>
          </Link>
        ) : (
          <p className="mt-2 text-sm text-ink-2">Nothing due — your review deck is clear.</p>
        )}
      </div>

      <StatTile
        label="Earned so far"
        tone="accent"
        icon={<Sparkles size={20} />}
        value={`${total_xp} XP`}
        hint={`${lessons_completed} lesson${lessons_completed === 1 ? '' : 's'} completed`}
      />
    </aside>
  )
}
