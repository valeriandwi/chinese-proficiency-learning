import { Link } from "react-router-dom";
import { Check, Lock, Play, Star } from "lucide-react";
import { useOverview, usePath, useSession } from "../../api/hooks";
import { describeLessonState } from "../../lib/labels";
import type { LessonSummary } from "../../api/types";
import { Chip, ErrorNote, ProgressRing, Spinner } from "../../components/ui";
import { CheckpointGlyph } from "../../components/icons";

/** One accent per unit, cycling through five families — Duolingo's device for telling
 *  one stretch of the path from the next. Written as literal class strings because
 *  Tailwind scans this file for them: a template-built class would compile to nothing.
 *
 *  The unit colour dresses the UNIT only (band, connector, checkpoint, badge). A lesson
 *  node keeps the four semantic state colours, so colour never has to carry two
 *  meanings at once. */
const UNIT_SKINS = [
  { band: "bg-unit-1-700", eyebrow: "text-unit-1-100", line: "bg-unit-1-300", chip: "bg-unit-1-100 text-unit-1-700", dot: "text-unit-1-600" },
  { band: "bg-unit-2-700", eyebrow: "text-unit-2-100", line: "bg-unit-2-300", chip: "bg-unit-2-100 text-unit-2-700", dot: "text-unit-2-600" },
  { band: "bg-unit-3-700", eyebrow: "text-unit-3-100", line: "bg-unit-3-300", chip: "bg-unit-3-100 text-unit-3-700", dot: "text-unit-3-600" },
  { band: "bg-unit-4-700", eyebrow: "text-unit-4-100", line: "bg-unit-4-300", chip: "bg-unit-4-100 text-unit-4-700", dot: "text-unit-4-600" },
  { band: "bg-unit-5-700", eyebrow: "text-unit-5-100", line: "bg-unit-5-300", chip: "bg-unit-5-100 text-unit-5-700", dot: "text-unit-5-600" },
] as const;

function unitSkin(unitNumber: number) {
  return UNIT_SKINS[(unitNumber - 1) % UNIT_SKINS.length];
}

/** A serpentine offset, so the trail reads as a path rather than a list. Written as
 *  literal class names because Tailwind scans this file for them. */

export function PathPage() {
  const session = useSession();
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
  const nextLesson = flat.find((lesson) => lesson.id === nextLessonId);
  const user = session.data?.authenticated ? session.data.user : undefined;

  return (
    <div className="flex flex-col gap-8">
      {/* The home screen. It says hello and then says what to do — the analytics that
          used to live here are on /progress, one tap from the strip above. */}
      <header>
        <h1 className="text-3xl font-extrabold tracking-tight text-ink">
          {user ? `你好, ${user.display_name}` : "你的学习路径"}
        </h1>
        <p className="mt-1 text-sm text-ink-2">
          {nextLesson
            ? `Next up: ${nextLesson.title}`
            : "Every lesson in this curriculum is complete."}
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
              {/* The decision is made on the level, so the way through it is offered here
                  rather than only on a separate page. */}
              <Link
                to={`/proficiency/${level.level_number}`}
                className="ml-auto shrink-0 text-xs font-bold text-brand-700 transition hover:text-brand-500"
              >
                Test out of HSK {level.level_number} →
              </Link>
            </div>
            {level.description ? (
              <p className="text-sm text-ink-2">{level.description}</p>
            ) : null}

            {level.units.map((unit) => {
              const skin = unitSkin(unit.unit_number);
              const done = unit.lessons.filter((lesson) => lesson.state === "completed").length;
              const allDone = done === unit.lessons.length && unit.lessons.length > 0;
              return (
                <div key={unit.id} className="flex flex-col gap-6">
                  <div
                    className={`flex flex-wrap items-center justify-between gap-2 rounded-card px-5 py-4 text-white shadow-card ${skin.band}`}
                  >
                    <div className="min-w-0">
                      <p className={`text-xs font-extrabold uppercase tracking-widest ${skin.eyebrow}`}>
                        Unit {unit.unit_number}
                      </p>
                      <h3 className="hanzi text-lg font-extrabold text-white">{unit.title}</h3>
                      {unit.description ? (
                        <p className={`mt-0.5 text-sm ${skin.eyebrow}`}>{unit.description}</p>
                      ) : null}
                    </div>
                    <span className={`rounded-full px-3 py-1 text-xs font-extrabold ${skin.chip}`}>
                      {done}/{unit.lessons.length} done
                    </span>
                  </div>

                  <ul className="flex flex-col items-center gap-7 px-2 pb-2 pt-3">
                    {unit.lessons.map((lesson, index) => (
                      <LessonNode
                        key={lesson.id}
                        lesson={lesson}
                        isNext={lesson.id === nextLessonId}
                        showConnector={index > 0}
                        connectorClass={skin.line}
                        order={index}
                      />
                    ))}
                    {/* The end of a unit, marked as a milestone. It is a marker, not a
                        destination: there is no lesson behind it, so it is not a link. */}
                    <li className="animate-bounce-in flex flex-col items-center gap-2" style={{ animationDelay: `${unit.lessons.length * 55}ms` }}>
                      <span className={allDone ? skin.dot : "text-ink-4"} aria-hidden>
                        <CheckpointGlyph className="h-14 w-14" />
                      </span>
                      <span className="text-xs font-bold uppercase tracking-widest text-ink-3">
                        {allDone ? `Unit ${unit.unit_number} complete` : `Unit ${unit.unit_number} checkpoint`}
                      </span>
                    </li>
                  </ul>
                </div>
              );
            })}
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
  connectorClass,
  order,
}: {
  lesson: LessonSummary;
  isNext: boolean;
  showConnector: boolean;
  connectorClass: string;
  order: number;
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
  // A deeper lip than before: the trail should look like something you can press, and
  // the four states stay distinguishable by shape and glyph as well as colour.
  const nodeSkin = {
    completed:
      "bg-success-600 text-white shadow-[0_6px_0_var(--color-success-700)]",
    in_progress:
      "bg-brand-600 text-white shadow-[0_6px_0_var(--color-brand-700)] animate-breathe",
    available: "border-4 border-brand-500 bg-surface text-brand-700 shadow-[0_6px_0_var(--color-brand-100)]",
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
    <li
      className="animate-bounce-in relative flex w-full flex-col items-center"
      style={{ animationDelay: `${order * 55}ms` }}
    >
      {showConnector ? (
        <span
          aria-hidden
          className={`absolute -top-7 left-1/2 h-7 w-2 -translate-x-1/2 rounded-full ${connectorClass}`}
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
          className={`grid h-24 w-24 place-items-center rounded-full transition group-hover:scale-105 group-active:translate-y-1 ${nodeSkin}`}
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
