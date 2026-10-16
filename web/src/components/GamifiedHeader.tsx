import { Link } from 'react-router-dom'
import { Flame, Sparkles, Target } from 'lucide-react'
import { useOverview } from '../api/hooks'
import { goalRatio } from '../lib/goal'
import { ProgressRing, Skeleton } from './ui'

/**
 * The one place a learner's numbers live.
 *
 * The gamification principle worth taking from Duolingo: the streak and the day's goal
 * are always visible and never repeated. Before this strip existed, the streak and the
 * goal were rendered twice — a tile row on the home page and again in a desktop rail —
 * plus the goal restated under the greeting, so the same two facts appeared three times
 * while the next lesson competed with eight other cards for the eye.
 *
 * The strip reads the overview the same way every page does, so it costs no extra
 * request and cannot show a different number than the page beside it. It links to
 * /progress: the numbers are the summary, the detail is one tap away.
 */
export function GamifiedHeader() {
  const overview = useOverview()

  if (overview.isLoading) {
    return (
      <div className="flex items-center gap-2" aria-hidden>
        <Skeleton className="h-9 w-24" />
        <Skeleton className="h-9 w-28" />
        <Skeleton className="h-9 w-24" />
      </div>
    )
  }

  // A failed progress fetch is not worth a banner in the chrome; the page itself
  // reports it, and the strip simply steps out of the way.
  if (!overview.data) return null

  const { streak, total_xp, lessons_completed } = overview.data
  const ratio = goalRatio(
    streak.goal_kind,
    streak.goal_value,
    streak.today_minutes,
    streak.today_xp,
    lessons_completed,
  )
  const percent = Math.round(ratio * 100)
  const days = `${streak.current_streak} day${streak.current_streak === 1 ? '' : 's'}`

  return (
    <div className="flex flex-wrap items-center gap-2" data-testid="gamified-header">
      <Link
        to="/progress"
        aria-label={`Streak: ${days}. See your progress.`}
        className="inline-flex items-center gap-2 rounded-full border border-line-strong bg-surface px-3 py-1.5 transition hover:border-reward-300"
      >
        <Flame size={18} aria-hidden className="text-reward-700" />
        <span className="text-lg font-extrabold leading-none text-ink">{streak.current_streak}</span>
        <span className="text-xs font-bold text-ink-2">day{streak.current_streak === 1 ? '' : 's'}</span>
      </Link>

      <Link
        to="/progress"
        aria-label={`Today's goal: ${percent}% ${streak.goal_met_today ? 'met' : 'to go'}. See your progress.`}
        className="inline-flex items-center gap-2 rounded-full border border-line-strong bg-surface px-3 py-1.5 transition hover:border-reward-300"
      >
        <ProgressRing value={ratio} size={26} stroke={5} tone="reward" label={`${percent}% of today's goal`}>
          <Target size={13} aria-hidden className="text-reward-700" />
        </ProgressRing>
        <span className="text-lg font-extrabold leading-none text-ink">{percent}%</span>
        <span className="text-xs font-bold text-ink-2">{streak.goal_met_today ? 'met' : 'to go'}</span>
      </Link>

      <Link
        to="/progress"
        aria-label={`${total_xp} XP earned. See your progress.`}
        className="inline-flex items-center gap-2 rounded-full border border-line-strong bg-surface px-3 py-1.5 transition hover:border-brand-300"
      >
        <Sparkles size={18} aria-hidden className="text-brand-600" />
        <span className="text-lg font-extrabold leading-none text-ink">{total_xp}</span>
        <span className="text-xs font-bold text-ink-2">XP</span>
      </Link>
    </div>
  )
}
