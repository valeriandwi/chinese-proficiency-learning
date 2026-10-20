import { Link } from 'react-router-dom'
import { Flame, Sparkles } from 'lucide-react'
import { useOverview, useSession } from '../api/hooks'
import { goalRatio } from '../lib/goal'
import { ProgressRing, Skeleton } from './ui'

/**
 * The learner header: two flex rows, and each row owns its own line.
 *
 * The single-row version was the crowded one — avatar, name, level, goal meter and two
 * stat pills all competed for one 360px line, so the goal bar collapsed to ~75px and the
 * pills stacked. Splitting it fixes that structurally:
 *
 *   row 1  identity   avatar + level (flex:none) · name (flex:1, truncates) · pills (flex:none)
 *   row 2  the goal   ring (flex:none) · bar (flex:1) · caption (flex:none)
 *
 * Width can therefore only ever come out of the name, which truncates gracefully, and
 * never out of a control. The level capsule sits UNDER the avatar (as the reference
 * does), which buys the name ~44px of line for free.
 *
 * The two right-hand pills carry real facts: the streak, and total XP. The reference's
 * coin and hearts have no server state here, and a fake counter is a lie the learner
 * eventually catches — so the XP pill is Bee Yellow with DARK text (7.1:1), the one
 * pairing yellow can carry.
 */
export function GamifiedHeader() {
  const overview = useOverview()
  const session = useSession()

  if (overview.isLoading) {
    return (
      <div className="flex flex-col gap-2.5 rounded-card border-2 border-line bg-surface p-3" aria-hidden>
        <div className="flex items-center gap-2.5">
          <Skeleton className="h-10 w-10 !rounded-full" />
          <Skeleton className="h-4 w-24" />
          <Skeleton className="ml-auto h-7 w-20" />
        </div>
        <Skeleton className="h-2 w-full !rounded-full" />
      </div>
    )
  }

  // A failed progress fetch is not worth a banner in the chrome; the page itself reports
  // it, and the card simply steps out of the way.
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
  // The learner's target level is a real profile field (placement sets it), so the capsule
  // says what they are working toward rather than inventing a game level.
  const user = session.data?.authenticated ? session.data.user : undefined
  const level = user?.target_level ?? null
  const initial = (user?.display_name ?? 'L').trim().charAt(0).toUpperCase()

  return (
    <div
      data-testid="gamified-header"
      className="flex flex-col gap-2.5 rounded-card border-2 border-line bg-surface p-3 shadow-card"
    >
      <div className="flex min-w-0 items-center gap-2.5">
        <div className="flex flex-none flex-col items-center gap-1">
          <span
            aria-hidden
            className="grid h-10 w-10 place-items-center rounded-full bg-brand-400 text-base font-extrabold text-ink ring-[3px] ring-surface"
          >
            {initial}
          </span>
          {level ? (
            <span className="rounded-pill bg-brand-100 px-2 py-0.5 text-[0.6rem] font-extrabold uppercase tracking-wide text-brand-700">
              HSK {level}
            </span>
          ) : null}
        </div>

        <p className="min-w-0 flex-1 truncate text-sm font-extrabold leading-tight text-ink">
          {user?.display_name ?? 'Learner'}
        </p>

        <div className="flex flex-none items-center gap-1">
          <Link
            to="/progress"
            aria-label={`Streak: ${days}. See your progress.`}
            className="inline-flex items-center gap-1 rounded-pill bg-reward-100 px-2 py-1.5 transition hover:bg-reward-300"
          >
            <Flame size={15} aria-hidden className="text-reward-700" />
            <span className="text-xs font-extrabold leading-none text-reward-700">{streak.current_streak}</span>
          </Link>

          <Link
            to="/progress"
            aria-label={`${total_xp} XP earned. See your progress.`}
            className="inline-flex items-center gap-1 rounded-pill bg-reward-500 px-2 py-1.5 transition hover:bg-reward-300"
          >
            <Sparkles size={15} aria-hidden className="text-ink" />
            <span className="text-xs font-extrabold leading-none text-ink">{total_xp}</span>
          </Link>
        </div>
      </div>

      <Link
        to="/progress"
        aria-label={`Today's goal: ${percent}% ${streak.goal_met_today ? 'met' : 'done'}. See your progress.`}
        className="flex min-w-0 items-center gap-2"
      >
        <ProgressRing value={ratio} size={26} stroke={5} tone="brand" label={`${percent}% of today's goal`}>
          <span className="text-[0.5rem] font-extrabold leading-none text-brand-700">{percent}</span>
        </ProgressRing>
        <span className="track block h-2 min-w-0 flex-1 overflow-hidden rounded-full">
          <span className="block h-full rounded-full bg-brand-500" style={{ width: `${percent}%` }} />
        </span>
        <span className="flex-none text-[0.65rem] font-extrabold tracking-wide text-ink-3">
          {streak.goal_met_today ? 'goal met' : `${percent}% of today`}
        </span>
      </Link>
    </div>
  )
}
