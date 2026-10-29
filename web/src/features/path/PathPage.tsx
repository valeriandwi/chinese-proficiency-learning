import { useState } from "react";

import { BookOpen, Check, Gift, Lock, Play, Star } from "lucide-react";
import { Link } from "react-router-dom";

import { useOverview, usePath } from "../../api/hooks";
import type { LessonSummary } from "../../api/types";
import { Select } from "../../components/Select";
import { Chip, ErrorNote, Skeleton } from "../../components/ui";
import { describeLessonState } from "../../lib/labels";

/** One skin for every unit: the app is green, and a blue-then-violet-then-magenta trail made
 *  the path the one screen that was not. Kept as an array so a unit can still be dressed
 *  differently later. Literal class strings because Tailwind scans this file for them; the
 *  eyebrow is white because brand-100 on brand-600 is below 4.5:1. */
const UNIT_SKINS = [
  {
    band: "bg-brand-600",
    eyebrow: "text-white",
    chip: "bg-brand-100 text-brand-700",
    dot: "text-brand-600",
  },
] as const;

function unitSkin(unitNumber: number) {
  return UNIT_SKINS[(unitNumber - 1) % UNIT_SKINS.length];
}

/** A serpentine offset, so the trail reads as a path rather than a list. Written as
 *  literal class names because Tailwind scans this file for them. */

export function PathPage() {
  const overview = useOverview();
  const version = overview.data?.curriculum_version ?? "hsk2.0";
  const path = usePath(version, Boolean(overview.data));
  // Which level the trail shows; null means "the one the learner is working on".
  const [pickedLevel, setPickedLevel] = useState<string | null>(null);

  if (overview.isLoading || (overview.data && path.isLoading))
    return <PathSkeleton />;
  if (overview.error)
    return (
      <ErrorNote onRetry={() => void overview.refetch()}>
        {(overview.error as Error).message}
      </ErrorNote>
    );
  if (path.error)
    return (
      <ErrorNote onRetry={() => void path.refetch()}>
        {(path.error as Error).message}
      </ErrorNote>
    );
  if (!path.data) return <PathSkeleton />;

  // The trail marks exactly one "start here" node: the first lesson that is not
  // finished. Everything above it is behind the learner, everything below is ahead.
  const flat = path.data.levels.flatMap((level) =>
    level.units.flatMap((unit) => unit.lessons),
  );
  const nextLessonId = flat.find((lesson) => lesson.state !== "completed")?.id;
  const nextLesson = flat.find((lesson) => lesson.id === nextLessonId);
  const dueReviews = overview.data?.due_reviews ?? 0;
  const currentLevel =
    path.data.levels.find((level) =>
      level.units.some((unit) =>
        unit.lessons.some((l) => l.id === nextLessonId),
      ),
    ) ?? path.data.levels[0];
  const shownId = pickedLevel ?? currentLevel?.id;
  const shownLevels = path.data.levels.filter((level) => level.id === shownId);

  return (
    <div className="flex flex-col gap-8">
      {/* The answer to "what now?" is the largest thing on the screen. Reviews are a nudge
          inside it, not a second button competing with Continue. */}
      <section
        aria-labelledby="up-next-title"
        className="flex flex-wrap items-center gap-4 rounded-lg bg-brand-400 p-6 text-ink shadow-[0_6px_0_var(--color-brand-600)]"
      >
        <div className="flex min-w-0 flex-1 basis-64 flex-col gap-2">
          <p className="text-xs font-extrabold uppercase tracking-widest">
            Up next
          </p>
          {nextLesson ? (
            <>
              <h2
                id="up-next-title"
                className="hanzi text-2xl font-extrabold leading-tight"
              >
                {nextLesson.title}
              </h2>
              <p className="text-sm font-bold">
                {nextLesson.exercise_count} exercises ·{" "}
                {nextLesson.estimated_minutes} min · {nextLesson.xp_reward} XP
              </p>
              <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2">
                <Link
                  to={`/lessons/${nextLesson.id}`}
                  className="inline-flex min-h-12 items-center justify-center rounded-tile border-2 border-white bg-white px-6 text-base font-extrabold text-brand-700 shadow-[0_4px_0_var(--color-brand-100)] transition active:translate-y-px active:shadow-none"
                >
                  {nextLesson.state === "in_progress" ? "Continue" : "Start"}{" "}
                  lesson
                </Link>
                {dueReviews > 0 ? (
                  <Link
                    to="/reviews"
                    className="text-sm font-extrabold underline underline-offset-2"
                  >
                    {dueReviews} review{dueReviews === 1 ? "" : "s"} due — do
                    those first
                  </Link>
                ) : null}
              </div>
            </>
          ) : (
            <h2 id="up-next-title" className="text-xl font-extrabold">
              Every lesson in this curriculum is complete.
            </h2>
          )}
        </div>
      </section>

      {shownLevels.map((level) => {
        return (
          <section key={level.id} className="flex flex-col gap-4">
            <div className="flex flex-wrap items-end justify-between gap-3">
              <div className="min-w-0">
                <h2 className="text-[1.375rem] font-extrabold text-ink">
                  {level.title}
                </h2>
                <p className="text-ink-3">
                  {level.description ? `${level.description} · ` : ""}
                  <span className="tabular-nums">
                    {level.completed_lessons} / {level.total_lessons}
                  </span>{" "}
                  lessons
                </p>
              </div>
              {/* The decision is made on the level, so the switcher and the way through it
                  sit here rather than on a separate page. */}
              <div className="flex shrink-0 items-center gap-2">
                <Select
                  label="Level"
                  value={level.id}
                  onChange={setPickedLevel}
                  align="right"
                  options={path.data.levels.map((option) => ({
                    value: option.id,
                    label: option.title,
                  }))}
                />
                <Link
                  to={`/proficiency/${level.level_number}`}
                  className="inline-flex min-h-11 items-center rounded-card border-2 border-b-4 border-line bg-surface px-4 text-sm font-extrabold text-ink-2 transition hover:border-line-strong hover:text-ink"
                >
                  Test out
                </Link>
              </div>
            </div>

            {level.units.map((unit) => {
              const skin = unitSkin(unit.unit_number);
              const done = unit.lessons.filter(
                (lesson) => lesson.state === "completed",
              ).length;
              const allDone =
                done === unit.lessons.length && unit.lessons.length > 0;
              return (
                <div key={unit.id} className="flex flex-col gap-6">
                  <div
                    className={`flex flex-wrap items-center justify-between gap-2 rounded-card px-5 py-4 text-white shadow-card ${skin.band}`}
                  >
                    <div className="min-w-0">
                      <p
                        className={`text-xs font-extrabold uppercase tracking-widest ${skin.eyebrow}`}
                      >
                        Unit {unit.unit_number}
                      </p>
                      <h3 className="hanzi text-lg font-extrabold text-white">
                        {unit.title}
                      </h3>
                      {unit.description ? (
                        <p className={`mt-0.5 text-sm ${skin.eyebrow}`}>
                          {unit.description}
                        </p>
                      ) : null}
                    </div>
                    <span className="flex items-center gap-2">
                      <span
                        className={`rounded-full px-3 py-1 text-xs font-extrabold ${skin.chip}`}
                      >
                        {done}/{unit.lessons.length} done
                      </span>
                      <Link
                        to="/guide"
                        className="inline-flex min-h-11 items-center gap-1.5 rounded-tile border-2 border-white bg-white px-3 text-sm font-extrabold text-brand-700"
                      >
                        <BookOpen size={16} aria-hidden /> Guide
                      </Link>
                    </span>
                  </div>

                  <ul className="flex flex-col items-center gap-[18px] px-2 pb-2 pt-3">
                    {unit.lessons.map((lesson, index) => (
                      <LessonNode
                        key={lesson.id}
                        lesson={lesson}
                        isNext={lesson.id === nextLessonId}
                        order={index}
                      />
                    ))}
                    {/* The end of a unit, marked as a milestone. It is a marker, not a
                        destination: there is no lesson behind it, so it is not a link. */}
                    <li
                      className="animate-bounce-in flex flex-col items-center gap-2 sm:[translate:-110px_0]"
                      style={{
                        animationDelay: `${unit.lessons.length * 55}ms`,
                      }}
                    >
                      {/* A square chest, not a circle: it is the unit review, not a lesson. */}
                      <span
                        className={`grid h-[70px] w-[76px] place-items-center rounded-[18px] border-b-[7px] ${
                          allDone
                            ? "border-reward-600 bg-reward-500 text-reward-700"
                            : "border-line bg-surface-2 text-ink-3"
                        }`}
                        aria-hidden
                      >
                        <Gift size={24} />
                      </span>
                      <span className="text-sm font-extrabold text-ink">
                        Unit review
                      </span>
                      <span className="text-xs font-bold text-ink-3">
                        {allDone
                          ? `Unit ${unit.unit_number} complete`
                          : `Locked · finish the unit`}
                      </span>
                    </li>
                  </ul>
                </div>
              );
            })}
          </section>
        );
      })}

      {/* On a phone the hero scrolls away on a long path; this keeps the next lesson one
          thumb-tap away, just above the tab bar. */}
      {nextLesson ? (
        <div className="sticky bottom-20 z-10 lg:hidden">
          <Link
            to={`/lessons/${nextLesson.id}`}
            className="flex min-h-12 items-center justify-center gap-1 rounded-card border-2 border-b-4 border-brand-600 border-b-brand-700 bg-brand-600 px-4 font-extrabold text-white"
          >
            {nextLesson.state === "in_progress" ? "Continue" : "Start"} ·{" "}
            <span className="hanzi">{nextLesson.title}</span>
          </Link>
        </div>
      ) : null}
    </div>
  );
}

/** How far each node sits from the trail's centre, in px: a gentle S so the trail reads as a
 *  path rather than a list. Applied through the independent `translate` property, which the
 *  entrance animation's `transform` cannot override, and only from the `sm` breakpoint up. */
const TRAIL_OFFSETS = [-100, -40, 60, -20, -90];

function LessonNode({
  lesson,
  isNext,
  order,
}: {
  lesson: LessonSummary;
  isNext: boolean;
  order: number;
}) {
  const state = describeLessonState(lesson.state);

  // Four states, told apart by shape and icon as well as colour — a learner who cannot
  // separate green from grey still sees a tick, a padlock and a play sign. The solid lip
  // under each node is the package's signature: a node looks pressed out of clay, and the
  // glyph sits on the darkened step (white on #58cc02 would be 2.09:1; on success-600 it
  // is 4.64:1).
  const nodeSkin = {
    completed: "border-b-[7px] border-reward-600 bg-reward-500 text-reward-700",
    in_progress:
      "border-b-[7px] border-brand-600 bg-brand-400 text-on-brand shadow-[0_0_0_8px_var(--color-brand-100)] animate-breathe",
    available: "border-[3px] border-b-[7px] border-line bg-surface text-ink-2",
    locked: "border-b-[7px] border-line bg-surface-2 text-ink-3",
  }[lesson.state];

  const icon =
    lesson.state === "completed" ? (
      <Check size={28} strokeWidth={3} aria-hidden />
    ) : lesson.state === "locked" ? (
      <Lock size={24} aria-hidden />
    ) : (
      <Play
        size={28}
        aria-hidden
        fill="currentColor"
        className="translate-x-0.5"
      />
    );

  return (
    <li
      className="animate-bounce-in relative flex w-full flex-col items-center sm:[translate:var(--trail-x)_0]"
      style={
        {
          animationDelay: `${order * 55}ms`,
          "--trail-x": `${TRAIL_OFFSETS[order % TRAIL_OFFSETS.length]}px`,
        } as React.CSSProperties
      }
    >
      {/* One target per lesson: the circle and its label are a single link, so the
          tap area is the whole node rather than a 80px puck — and the lesson's name
          is inside the anchor, where a text search (or a screen reader) expects it. */}
      <Link
        to={`/lessons/${lesson.id}`}
        aria-label={`${lesson.title} — ${state.label}`}
        className="group flex flex-col items-center gap-2 rounded-card p-2 transition hover:scale-[1.02]"
      >
        {isNext && lesson.state !== "locked" ? (
          <span className="animate-rise relative mb-1.5 rounded-[14px] border-2 border-line bg-surface px-3 py-1.5 text-[0.8rem] font-black uppercase tracking-wider text-brand-700 after:absolute after:-bottom-2 after:left-1/2 after:size-3 after:-translate-x-1/2 after:rotate-45 after:border-b-2 after:border-r-2 after:border-line after:bg-surface after:content-['']">
            Start
          </span>
        ) : null}
        <span
          className={`grid h-[70px] w-[76px] place-items-center rounded-full transition group-hover:scale-105 group-active:translate-y-1 ${nodeSkin}`}
        >
          {icon}
        </span>

        <span className="max-w-xs text-center">
          <span className="block truncate text-sm font-extrabold text-ink">
            {lesson.kind !== "lesson" ? `${lesson.kind} · ` : ""}
            {lesson.title}
          </span>
          <span className="mt-0.5 flex items-center justify-center gap-1.5 text-xs font-bold text-ink-3">
            {state.label}
            {lesson.state === "completed" && lesson.best_score !== null
              ? ` · ${Math.round(lesson.best_score * 100)}%`
              : ""}
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

/** The path's loading state is the shape of the path: a hero, then nodes down the trail. */
function PathSkeleton() {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label="Loading the path"
      className="flex flex-col gap-8"
    >
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-40 w-full !rounded-lg" />
      <div className="flex flex-col items-center gap-7">
        <Skeleton className="h-24 w-24 !rounded-full" />
        <Skeleton className="h-24 w-24 !rounded-full" />
        <Skeleton className="h-24 w-24 !rounded-full" />
      </div>
    </div>
  );
}
