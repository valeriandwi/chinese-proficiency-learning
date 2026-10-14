import { Link } from "react-router-dom";
import { Check, Lock, Play, Star } from "lucide-react";
import { useOverview, usePath } from "../../api/hooks";
import { describeLessonState } from "../../lib/labels";
import type { LessonSummary } from "../../api/types";
import { Chip, ErrorNote, ProgressRing, Spinner } from "../../components/ui";

/** A serpentine offset, so the trail reads as a path rather than a list. Written as
 *  literal class names because Tailwind scans this file for them. */

export function PathPage() {
  const overview = useOverview();
  const version = overview.data?.curriculum_version ?? "hsk2.0";
  const path = usePath(version, Boolean(overview.data));

  if (overview.isLoading || (overview.data && path.isLoading))
    return <Spinner label="Loading the path" />;
  if (overview.error)
    return <ErrorNote>{(overview.error as Error).message}</ErrorNote>;
  if (path.error) return <ErrorNote>{(path.error as Error).message}</ErrorNote>;
  if (!path.data) return <Spinner label="Loading the path" />;

  // The trail marks exactly one "start here" node: the first lesson that is not
  // finished. Everything above it is behind the learner, everything below is ahead.
  const flat = path.data.levels.flatMap((level) =>
    level.units.flatMap((unit) => unit.lessons),
  );
  const nextLessonId = flat.find((lesson) => lesson.state !== "completed")?.id;

  return (
    <div className="flex flex-col gap-8">
      <header>
        <h1 className="text-3xl font-extrabold tracking-tight text-ink">
          你的学习路径
        </h1>
        <p className="mt-1 text-sm text-ink-2">
          {path.data.version} · lessons unlock in order, and the server enforces
          it.
        </p>
      </header>

      {path.data.levels.map((level) => {
        const levelRatio =
          level.total_lessons > 0
            ? level.completed_lessons / level.total_lessons
            : 0;
        return (
          <section key={level.id} className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center gap-3">
              <ProgressRing
                value={levelRatio}
                tone="brand"
                size={56}
                stroke={6}
                label={`${level.completed_lessons} of ${level.total_lessons} lessons complete`}
              >
                <span className="text-xs font-extrabold text-ink">
                  {level.completed_lessons}/{level.total_lessons}
                </span>
              </ProgressRing>
              <div className="min-w-0">
                <h2 className="text-xl font-extrabold text-ink">
                  {level.title}
                </h2>
                <p className="text-sm text-ink-2">
                  {level.completed_lessons}/{level.total_lessons} lessons
                </p>
              </div>
              {level.stage ? <Chip tone="accent">{level.stage}</Chip> : null}
            </div>
            {level.description ? (
              <p className="text-sm text-ink-2">{level.description}</p>
            ) : null}

            {level.units.map((unit) => (
              <div key={unit.id} className="flex flex-col gap-6">
                <div className="flex flex-wrap items-center justify-between gap-2 rounded-card bg-brand-600 px-5 py-4 text-white shadow-card">
                  <div className="min-w-0">
                    <p className="text-xs font-extrabold uppercase tracking-widest text-brand-100">
                      Unit {unit.unit_number}
                    </p>
                    <h3 className="hanzi text-lg font-extrabold text-white">
                      {unit.title}
                    </h3>
                    {unit.description ? (
                      <p className="mt-0.5 text-sm text-brand-100">
                        {unit.description}
                      </p>
                    ) : null}
                  </div>
                  <Chip tone="accent">
                    {
                      unit.lessons.filter(
                        (lesson) => lesson.state === "completed",
                      ).length
                    }
                    /{unit.lessons.length} done
                  </Chip>
                </div>

                <ul className="flex flex-col items-center gap-7 px-2 pb-2 pt-3">
                  {unit.lessons.map((lesson, index) => (
                    <LessonNode
                      key={lesson.id}
                      lesson={lesson}
                      isNext={lesson.id === nextLessonId}
                      showConnector={index > 0}
                    />
                  ))}
                </ul>
              </div>
            ))}
          </section>
        );
      })}
    </div>
  );
}

function LessonNode({
  lesson,
  isNext,
  showConnector,
}: {
  lesson: LessonSummary;
  isNext: boolean;
  showConnector: boolean;
}) {
  const state = describeLessonState(lesson.state);
  const meta = [
    `${lesson.exercise_count} exercises`,
    `${lesson.estimated_minutes} min`,
    `${lesson.xp_reward} XP`,
    lesson.best_score !== null
      ? `best ${Math.round(lesson.best_score * 100)}%`
      : "",
  ]
    .filter(Boolean)
    .join(" · ");

  // Four states, told apart by shape and icon as well as colour — a learner who
  // cannot separate green from indigo still sees a tick, a padlock and a play sign.
  const nodeSkin = {
    completed:
      "bg-success-600 text-white shadow-[0_4px_0_var(--color-success-700)]",
    in_progress:
      "bg-brand-600 text-white shadow-[0_4px_0_var(--color-brand-700)] animate-breathe",
    available: "border-4 border-brand-500 bg-surface text-brand-700",
    locked: "border-2 border-line bg-surface-2 text-ink-3",
  }[lesson.state];

  const icon =
    lesson.state === "completed" ? (
      <Check size={30} aria-hidden />
    ) : lesson.state === "locked" ? (
      <Lock size={24} aria-hidden />
    ) : (
      <Play size={26} aria-hidden className="translate-x-0.5" />
    );

  return (
    <li className={`relative flex w-full flex-col items-center`}>
      {showConnector ? (
        <span
          aria-hidden
          className="absolute -top-7 left-1/2 h-7 w-1.5 -translate-x-1/2 rounded-full bg-line"
        />
      ) : null}

      {/* One target per lesson: the circle and its label are a single link, so the
          tap area is the whole node rather than a 80px puck — and the lesson's name
          is inside the anchor, where a text search (or a screen reader) expects it. */}
      <Link
        to={`/lessons/${lesson.id}`}
        aria-label={`${lesson.title} — ${state.label}`}
        className="group flex flex-col items-center gap-2 rounded-card p-2 transition hover:scale-[1.02]"
      >
        {isNext && lesson.state !== "locked" ? (
          <span className="animate-rise rounded-lg bg-brand-600 px-2.5 py-1 text-[0.65rem] font-extrabold uppercase tracking-widest text-white shadow-tile">
            {lesson.state === "in_progress" ? "Keep going" : "Start"}
          </span>
        ) : null}
        <span
          className={`grid h-20 w-20 place-items-center rounded-full transition group-hover:scale-105 ${nodeSkin}`}
        >
          {icon}
        </span>

        <span className="max-w-xs text-center">
          <span className="block truncate font-extrabold text-ink">
            {lesson.kind !== "lesson" ? `${lesson.kind} · ` : ""}
            {lesson.title}
          </span>
          <span className="mt-0.5 block text-xs text-ink-2">{meta}</span>
          <span className="mt-1.5 flex items-center justify-center gap-1.5">
            <Chip tone={state.tone}>{state.label}</Chip>
            {lesson.kind === "mock_exam" ? (
              <Chip tone="warning">
                <Star size={12} aria-hidden /> exam
              </Chip>
            ) : null}
          </span>
        </span>
      </Link>
    </li>
  );
}
