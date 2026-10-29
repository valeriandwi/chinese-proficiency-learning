import { Link } from 'react-router-dom'
import { Flame, Sparkles } from 'lucide-react'
import { useOverview, useSession } from '../api/hooks'
import { goalRatio } from '../lib/goal'
import { ProgressRing, Skeleton } from './ui'

/**
 * The learner's numbers, as one compact bar: who, the streak, XP, and today's goal ring.
 *
 * It used to be a two-row card (avatar + level capsule over a goal meter) that pushed every
 * page down by ~110px. One row says the same things: the name truncates before any control
 * does, and every number is a link into /progress.
 *
 * The streak and XP are real server facts. The XP pill is Bee Yellow with DARK text (7.1:1),
 * the one pairing yellow can carry; a fake coin or hearts counter would be a lie the learner
 * eventually catches, so neither exists.
 */
export function GamifiedHeader() {
  const overview = useOverview()
  const session = useSession()

  if (overview.isLoading) {
    return (
      <div className="flex items-center gap-2.5" aria-hidden>
        <Skeleton className="h-4 w-24" />
        <Skeleton className="ml-auto h-9 w-40 !rounded-full" />
      </div>
    )
  }

  // A failed progress fetch is not worth a banner in the chrome; the page itself reports
  // it, and the bar simply steps out of the way.
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
  const user = session.data?.authenticated ? session.data.user : undefined
  const level = user?.target_level ?? null

  return (
    <div data-testid="gamified-header" className="flex min-w-0 items-center gap-2.5 max-sm:justify-end">
      {/* The phone header above already carries identity; here it would only cost a line. */}
      <p className="min-w-0 flex-1 truncate text-sm font-extrabold text-ink max-sm:hidden">
        {user?.display_name ?? 'Learner'}
        {level ? (
          <span className="ml-2 rounded-pill bg-brand-100 px-2 py-0.5 align-middle text-[0.65rem] font-extrabold uppercase tracking-wide text-brand-700">
            HSK {level}
          </span>
        ) : null}
      </p>

      <Link
        to="/progress"
        aria-label={`Streak: ${days}. See your progress.`}
        className="inline-flex min-h-10 flex-none items-center gap-1 rounded-pill bg-reward-100 px-3 transition hover:bg-reward-300"
      >
        <Flame size={16} aria-hidden className="text-reward-700" />
        <span className="text-sm font-extrabold tabular-nums leading-none text-reward-700">{streak.current_streak}</span>
      </Link>

      <Link
        to="/progress"
        aria-label={`${total_xp} XP earned. See your progress.`}
        className="inline-flex min-h-10 flex-none items-center gap-1 rounded-pill bg-reward-500 px-3 transition hover:bg-reward-300"
      >
        <Sparkles size={16} aria-hidden className="text-ink" />
        <span className="text-sm font-extrabold tabular-nums leading-none text-ink">{total_xp}</span>
      </Link>

      <Link
        to="/progress"
        aria-label={`Today's goal: ${percent}% ${streak.goal_met_today ? 'met' : 'done'}. See your progress.`}
        className="flex-none rounded-full"
      >
        <ProgressRing value={ratio} size={40} stroke={5} tone="brand" label={`${percent}% of today's goal`}>
          <span className="text-[0.6rem] font-extrabold leading-none tabular-nums text-brand-700">{percent}</span>
        </ProgressRing>
      </Link>
    </div>
  )
}
