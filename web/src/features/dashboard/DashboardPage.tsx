import { Link } from "react-router-dom";
import { BookOpenCheck, Flame, Play, Sparkles, Target } from "lucide-react";
import {
  useCurricula,
  useOverview,
  useRefreshProgress,
  useSession,
} from "../../api/hooks";
import {
  describeGoal,
  describeSkill,
  formatDay,
  formatPercent,
} from "../../lib/labels";
import { goalRatio, goalProgress } from "../../lib/goal";
import {
  Button,
  Card,
  Chip,
  EmptyState,
  ErrorNote,
  ProgressBar,
  ProgressRing,
  Spinner,
  StatTile,
} from "../../components/ui";

export function DashboardPage() {
  const session = useSession();
  const overview = useOverview();
  const curricula = useCurricula();
  const refresh = useRefreshProgress();

  if (overview.isLoading) return <Spinner label="Loading your progress" />;
  if (overview.error)
    return <ErrorNote>{(overview.error as Error).message}</ErrorNote>;
  if (!overview.data)
    return (
      <EmptyState
        title="No progress yet"
        hint="Complete a lesson to see your streak grow."
      />
    );

  const { streak, skills, activity } = overview.data;
  const user = session.data?.authenticated ? session.data.user : undefined;
  const version =
    overview.data.curriculum_version ??
    curricula.data?.curricula[0]?.version ??
    "hsk2.0";
  const ratio = goalRatio(
    streak.goal_kind,
    streak.goal_value,
    streak.today_minutes,
    streak.today_xp,
    overview.data.lessons_completed,
  );

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-extrabold text-ink">
            {user ? `你好, ${user.display_name}` : "Your progress"}
          </h1>
          <p className="text-sm text-ink-2">
            {describeGoal(streak.goal_kind, streak.goal_value)}
            {user?.target_level ? ` · target HSK ${user.target_level}` : ""}
          </p>
        </div>
        <Button
          variant="secondary"
          onClick={() => refresh.mutate()}
          disabled={refresh.isPending}
        >
          {refresh.isPending ? "Recomputing…" : "Recompute progress"}
        </Button>
      </header>

      {/* One lesson, one button. A learner who reads nothing else on this screen
          still knows what to do next. */}
      {overview.data.next_lesson_id ? (
        <section className="flex flex-wrap items-center justify-between gap-4 rounded-card bg-brand-600 px-5 py-5 text-white shadow-card">
          <div className="min-w-0">
            <p className="text-xs font-extrabold uppercase tracking-widest text-brand-100">
              Up next
            </p>
            <h2 className="mt-1 truncate text-xl font-extrabold text-white">
              {overview.data.next_lesson_title ?? "Path complete"}
            </h2>
            <p className="mt-1 text-sm text-brand-100">
              {streak.goal_met_today
                ? "Goal met today — anything more is a bonus."
                : "A few minutes today keeps the streak alive."}
            </p>
          </div>
          <Link to={`/lessons/${overview.data.next_lesson_id}`}>
            <Button className="!bg-surface !text-brand-700 !shadow-[0_3px_0_var(--color-brand-700)] hover:!bg-brand-50">
              <Play size={16} aria-hidden /> Continue learning
            </Button>
          </Link>
        </section>
      ) : (
        <Card className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <p className="text-xs font-extrabold uppercase tracking-widest text-ink-3">
              Up next
            </p>
            <h2 className="text-lg font-extrabold text-ink">Path complete</h2>
          </div>
          <Link to="/path">
            <Button variant="secondary">See the path</Button>
          </Link>
        </Card>
      )}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        <StatTile
          label="Streak"
          tone="warning"
          icon={<Flame size={20} />}
          value={
            <>
              {streak.current_streak}
              <span className="ml-1 text-base font-bold text-ink-2">
                day{streak.current_streak === 1 ? "" : "s"}
              </span>
            </>
          }
          hint={
            <>
              longest {streak.longest_streak}
              {streak.last_active_day
                ? ` · last active ${formatDay(streak.last_active_day)}`
                : ""}
            </>
          }
        />

        <div className="rounded-card border border-line bg-surface p-4 shadow-tile">
          <div className="flex items-center gap-3">
            <ProgressRing
              value={ratio}
              tone="reward"
              label={`${Math.round(ratio * 100)}% of today's goal`}
            >
              <Target size={20} className="text-reward-700" aria-hidden />
            </ProgressRing>
            <div className="min-w-0">
              <p className="text-xs font-bold uppercase tracking-wide text-ink-3">
                Today&apos;s goal
              </p>
              <p className="text-2xl font-extrabold leading-tight text-ink">
                {streak.goal_met_today ? "Met" : "Not yet"}
                <span className="ml-2 text-sm font-bold text-ink-2">
                  {streak.today_minutes} min · {streak.today_xp} XP
                </span>
              </p>
              <ProgressBar
                value={
                  goalProgress(
                    streak.goal_kind,
                    streak.goal_value,
                    streak.today_minutes,
                    streak.today_xp,
                    overview.data.lessons_completed,
                  ) / 100
                }
                tone="reward"
                className="mt-2"
                label="Today's goal progress"
              />
            </div>
          </div>
          <p className="mt-2 text-xs text-ink-2">
            A streak counts days whose goal you met, not days you opened the
            app.
          </p>
        </div>

        <StatTile
          label="Review queue"
          tone="accent"
          icon={<BookOpenCheck size={20} />}
          value={
            <>
              {overview.data.due_reviews}
              <span className="ml-1 text-sm font-bold text-ink-2">due</span>
            </>
          }
          hint={
            <Link to="/reviews" className="font-bold text-brand-700">
              Start reviewing →
            </Link>
          }
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <h2 className="font-extrabold text-ink">Skill mastery</h2>
          {skills.length === 0 ? (
            <p className="mt-2 text-sm text-ink-2">
              Your first answers will show up here.
            </p>
          ) : (
            <ul className="mt-3 flex flex-col gap-3">
              {skills.map((skill) => {
                const described = describeSkill(skill.skill);
                return (
                  <li key={skill.skill} className="flex items-center gap-3">
                    <span className="w-24 shrink-0 text-sm font-bold text-ink">
                      {described.label}
                    </span>
                    <ProgressBar
                      value={skill.mastery}
                      tone="brand"
                      className="flex-1"
                      label={`${described.label} mastery ${formatPercent(skill.mastery)}`}
                    />
                    <span className="w-24 shrink-0 text-right text-xs text-ink-3">
                      {formatPercent(skill.mastery)} · {skill.correct}/
                      {skill.attempts}
                    </span>
                  </li>
                );
              })}
            </ul>
          )}
        </Card>

        <Card>
          <h2 className="font-extrabold text-ink">Last 30 days</h2>
          <div className="mt-3 flex flex-wrap gap-1">
            {activity.length === 0 ? (
              <p className="text-sm text-ink-2">Nothing recorded yet.</p>
            ) : (
              activity.map((day) => (
                <span
                  key={day.day}
                  title={`${day.day}: ${day.minutes} min, ${day.xp} XP, ${day.attempts} answers`}
                  className={`h-5 w-5 rounded ${day.goal_met ? "bg-success-600" : day.attempts > 0 ? "bg-success-300" : "bg-surface-2"}`}
                />
              ))
            )}
          </div>
          <div className="mt-4 flex flex-wrap gap-4 text-sm text-ink-2">
            <span className="inline-flex items-center gap-1.5">
              <Sparkles size={16} className="text-reward-700" aria-hidden />
              <strong className="text-ink">{overview.data.total_xp}</strong> XP
            </span>
            <span>
              <strong className="text-ink">
                {overview.data.lessons_completed}
              </strong>{" "}
              lessons
            </span>
            <span>
              <strong className="text-ink">
                {overview.data.total_attempts}
              </strong>{" "}
              answers
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
  );
}
