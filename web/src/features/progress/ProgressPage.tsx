import { Link } from 'react-router-dom'
import { BookOpenCheck, Flame, Sparkles, Target, Wrench } from 'lucide-react'
import { useCurricula, useOverview, useRefreshProgress, useSession } from '../../api/hooks'
import { describeGoal, describeSkill, formatDay, formatPercent } from '../../lib/labels'
import { goalRatio } from '../../lib/goal'
import {
  Button,
  Card,
  Chip,
  EmptyState,
  ErrorNote,
  ProgressBar,
  Spinner,
} from '../../components/ui'

/**
 * Everything that used to crowd the home screen, in one place a learner chooses to open.
 *
 * The home is now the path: a screen whose job is to say what to do next. The mastery
 * table, the 30-day activity and the streak/goal/review tiles all live here, behind the
 * header strip, which is where Duolingo puts its statistics too — off the screen you
 * open to start working.
 *
 * `Recompute progress` moved here as well. It is a database operation, and it was
 * sitting in the primary position of the learner's home screen.
 */
export function ProgressPage() {
  const session = useSession()
  const overview = useOverview()
  const curricula = useCurricula()
  const refresh = useRefreshProgress()

  if (overview.isLoading) return <Spinner label="Loading your progress" />
  if (overview.error) return <ErrorNote onRetry={() => void overview.refetch()}>{(overview.error as Error).message}</ErrorNote>
  if (!overview.data) {
    return <EmptyState title="No progress yet" hint="Complete a lesson to see your streak grow." />
  }

  const { streak, skills, activity } = overview.data
  const user = session.data?.authenticated ? session.data.user : undefined
  // The skill with the lowest mastery among those actually attempted: absence of data is
  // not weakness, so an unattempted skill never qualifies.
  const weakest = skills
    .filter((skill) => skill.attempts > 0)
    .reduce<(typeof skills)[number] | undefined>((low, skill) => (!low || skill.mastery < low.mastery ? skill : low), undefined)
  const version = overview.data.curriculum_version ?? curricula.data?.curricula[0]?.version ?? 'hsk2.0'
  const ratio = goalRatio(
    streak.goal_kind,
    streak.goal_value,
    streak.today_minutes,
    streak.today_xp,
    overview.data.lessons_completed,
  )

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-3xl font-extrabold tracking-tight text-ink">Progress</h1>
          <p className="text-sm text-ink-3">
            {describeGoal(streak.goal_kind, streak.goal_value)}
            {user?.target_level ? ` · target HSK ${user.target_level}` : ''}
          </p>
        </div>
        {/* Recompute is honest but is a database operation, so it is not a primary action. */}
        <details className="relative">
          <summary className="inline-flex min-h-12 cursor-pointer list-none items-center rounded-card px-5 font-extrabold text-ink-2 hover:bg-surface-2">
            More
          </summary>
          <div className="absolute right-0 top-12 z-10 w-60 rounded-card border-2 border-b-4 border-line bg-surface p-2">
            <Button
              variant="ghost"
              onClick={() => refresh.mutate()}
              disabled={refresh.isPending}
              title="Rebuild every derived number from the attempt log"
              className="w-full justify-start"
            >
              <Wrench size={16} aria-hidden />
              {refresh.isPending ? 'Recomputing…' : 'Recompute from attempts'}
            </Button>
          </div>
        </details>
      </header>

      {/* Four equal tiles; the reviews tile is itself the link. */}
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <div className="flex flex-col gap-1 rounded-card border-2 border-b-4 border-line bg-surface p-4">
          <span className="flex items-center gap-1.5 text-xs font-extrabold uppercase tracking-widest text-ink-3">
            <Flame size={14} aria-hidden /> Streak
          </span>
          <span className="text-3xl font-black tabular-nums text-reward-700">
            {streak.current_streak}
            <span className="ml-1 text-base font-bold text-ink-3">day{streak.current_streak === 1 ? '' : 's'}</span>
          </span>
          <span className="text-xs text-ink-3">
            Longest {streak.longest_streak}
            {streak.last_active_day ? ` · last active ${formatDay(streak.last_active_day)}` : ''}
          </span>
        </div>

        <div className="flex flex-col gap-1 rounded-card border-2 border-b-4 border-line bg-surface p-4">
          <span className="flex items-center gap-1.5 text-xs font-extrabold uppercase tracking-widest text-ink-3">
            <Target size={14} aria-hidden /> Today&apos;s goal
          </span>
          <span className="text-3xl font-black tabular-nums text-ink">
            {streak.goal_met_today ? 'Met' : 'Not yet'}
            <span className="ml-1 text-base font-bold text-ink-3">
              {streak.today_minutes} min · {streak.today_xp} XP
            </span>
          </span>
          <ProgressBar
            value={ratio}
            tone="reward"
            className="mt-1"
            label={`${Math.round(ratio * 100)}% of today's goal`}
          />
        </div>

        <div className="flex flex-col gap-1 rounded-card border-2 border-b-4 border-line bg-surface p-4">
          <span className="flex items-center gap-1.5 text-xs font-extrabold uppercase tracking-widest text-ink-3">
            <Sparkles size={14} aria-hidden /> Total XP
          </span>
          <span className="text-3xl font-black tabular-nums text-reward-700">{overview.data.total_xp}</span>
          <span className="text-xs text-ink-3">{overview.data.lessons_completed} lessons done</span>
        </div>

        <Link
          to="/reviews"
          className="flex flex-col gap-1 rounded-card border-2 border-b-4 border-line bg-surface p-4 transition hover:border-line-strong"
        >
          <span className="flex items-center gap-1.5 text-xs font-extrabold uppercase tracking-widest text-ink-3">
            <BookOpenCheck size={14} aria-hidden /> Reviews due
          </span>
          <span className="text-3xl font-black tabular-nums text-danger-700">{overview.data.due_reviews}</span>
          <span className="text-xs font-extrabold text-brand-700">Start reviewing →</span>
        </Link>
      </div>

      <div className="grid gap-3 lg:grid-cols-[1.2fr_1fr]">
        <Card>
          <h2 className="font-extrabold text-ink">Skill mastery</h2>
          {skills.length === 0 ? (
            <p className="mt-2 text-sm text-ink-2">Your first answers will show up here.</p>
          ) : (
            <ul className="mt-3 flex flex-col gap-3">
              {skills.map((skill) => {
                const described = describeSkill(skill.skill)
                // A skill with no attempts is NOT a skill at 0%. Showing a learner
                // "0% · 0/12" against their speaking — which this build does not
                // pronounce at all without a provider — reads as a verdict on their
                // ability rather than an absence of data, so it says what is true.
                const assessed = skill.attempts > 0
                return (
                  <li key={skill.skill} className="flex items-center gap-3">
                    <span className="w-24 shrink-0 text-sm font-bold text-ink">{described.label}</span>
                    <ProgressBar
                      value={assessed ? skill.mastery : 0}
                      tone="brand"
                      className="flex-1"
                      label={
                        assessed
                          ? `${described.label} mastery ${formatPercent(skill.mastery)}`
                          : `${described.label} not assessed yet`
                      }
                    />
                    <span className="w-28 shrink-0 text-right text-xs text-ink-3">
                      {assessed ? `${formatPercent(skill.mastery)} · ${skill.correct}/${skill.attempts}` : 'not yet assessed'}
                    </span>
                  </li>
                )
              })}
            </ul>
          )}
          {weakest ? (
            <Link to="/skills" className="mt-4 inline-block text-sm font-extrabold text-brand-700">
              Weakest: {describeSkill(weakest.skill).label.toLowerCase()} — practise it →
            </Link>
          ) : null}
        </Card>

        <Card>
          <h2 className="font-extrabold text-ink">Last 30 days</h2>
          <div className="mt-3 grid grid-cols-10 gap-1.5">
            {activity.length === 0 ? (
              <p className="text-sm text-ink-2">Nothing recorded yet.</p>
            ) : (
              activity.map((day, i) => (
                <span
                  key={day.day}
                  title={`${day.day}: ${day.minutes} min, ${day.xp} XP, ${day.attempts} answers`}
                  className={`aspect-square rounded ${day.goal_met ? 'bg-success-600' : day.attempts > 0 ? 'bg-success-300' : 'bg-surface-2'} ${
                    i === activity.length - 1 ? 'ring-2 ring-offset-1 ring-ink' : ''
                  }`}
                />
              ))
            )}
          </div>
          {activity.length > 0 ? (
            <ul className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs font-bold text-ink-3">
              <li className="inline-flex items-center gap-1.5"><span className="h-3 w-3 rounded bg-surface-2" />none</li>
              <li className="inline-flex items-center gap-1.5"><span className="h-3 w-3 rounded bg-success-300" />some</li>
              <li className="inline-flex items-center gap-1.5"><span className="h-3 w-3 rounded bg-success-600" />goal met</li>
              <li className="inline-flex items-center gap-1.5"><span className="h-3 w-3 rounded ring-2 ring-ink" />today</li>
            </ul>
          ) : null}
          <div className="mt-4 flex flex-wrap gap-4 text-sm text-ink-2">
            <span className="inline-flex items-center gap-1.5">
              <Sparkles size={16} className="text-reward-700" aria-hidden />
              <strong className="text-ink">{overview.data.total_xp}</strong> XP
            </span>
            <span>
              <strong className="text-ink">{overview.data.lessons_completed}</strong> lessons
            </span>
            <span>
              <strong className="text-ink">{overview.data.total_attempts}</strong> answers
            </span>
          </div>
          <div className="mt-3 flex gap-2">
            <Chip tone="accent">{version}</Chip>
            {curricula.data?.curricula.map((c) => (
              <Chip key={c.version}>
                {c.version} · {c.level_count} levels
              </Chip>
            ))}
          </div>
        </Card>
      </div>
    </div>
  )
}
