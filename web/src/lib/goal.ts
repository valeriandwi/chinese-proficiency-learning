/** How much of today's goal is done, as a ratio in 0..1.
 *
 * Shared by Today and the right rail so the two can never disagree about whether
 * the goal is met — the streak is built from exactly this comparison server-side.
 */
export function goalRatio(kind: string, goal: number, minutes: number, xp: number, lessons: number): number {
  const done = kind === 'minutes' ? minutes : kind === 'xp' ? xp : lessons
  if (!(goal > 0)) return 0
  return Math.max(0, Math.min(1, done / goal))
}

/** The same ratio as a percentage, for the bar that is styled with a width. */
export function goalProgress(kind: string, goal: number, minutes: number, xp: number, lessons: number): number {
  return goalRatio(kind, goal, minutes, xp, lessons) * 100
}
