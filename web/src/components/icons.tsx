import type { ReactElement, SVGProps } from 'react'

/**
 * hanpath's own icon and motif set.
 *
 * These exist because the interface was wearing a library's line icons: correct but
 * anonymous, and thinner than the buttons and path nodes they sit in. A learning app's
 * chrome should feel pressable, so these are filled, chunky, and built from plain
 * geometry — circles, rounded rectangles and strokes — which is what makes them ours
 * rather than a copy of anybody's artwork. Nothing here is derived from another
 * product's illustrations, and no character or mascot is borrowed.
 *
 * Every glyph is drawn on a 24x24 grid and inherits `currentColor`, so a caller picks
 * the role (brand / success / ink) exactly as it does for any other element.
 */

type Glyph = (props: SVGProps<SVGSVGElement>) => ReactElement

/** The path — the home screen, and the thing a learner is walking along. */
export const LearnGlyph: Glyph = (props) => (
  <svg viewBox="0 0 24 24" fill="none" aria-hidden {...props}>
    <path
      d="M6.5 17.5c3.5 0 3.5-4 7-4s3.5-4 4.5-5.5"
      stroke="currentColor"
      strokeWidth={2.4}
      strokeLinecap="round"
      strokeDasharray="0.1 4.4"
    />
    <circle cx="6" cy="17.5" r="3.1" fill="currentColor" />
    <circle cx="13.2" cy="13.4" r="2.5" fill="currentColor" opacity="0.65" />
    <circle cx="18.6" cy="7.6" r="3.6" fill="currentColor" />
  </svg>
)

/** A stack of cards — the spaced-repetition deck. */
export const ReviewsGlyph: Glyph = (props) => (
  <svg viewBox="0 0 24 24" fill="none" aria-hidden {...props}>
    <rect x="3.2" y="7.4" width="14.4" height="11.4" rx="3" fill="currentColor" opacity="0.45" />
    <rect x="6.4" y="4" width="14.4" height="11.4" rx="3" fill="currentColor" />
  </svg>
)

/** Four bars — a pitch contour, which is what a tone is. */
export const TonesGlyph: Glyph = (props) => (
  <svg viewBox="0 0 24 24" fill="none" aria-hidden {...props}>
    <rect x="3" y="10" width="3.4" height="8" rx="1.7" fill="currentColor" opacity="0.45" />
    <rect x="8.2" y="6" width="3.4" height="12" rx="1.7" fill="currentColor" opacity="0.7" />
    <rect x="13.4" y="3" width="3.4" height="15" rx="1.7" fill="currentColor" />
    <rect x="18.6" y="12" width="3.4" height="6" rx="1.7" fill="currentColor" opacity="0.55" />
  </svg>
)

/** Sliders, not a cog: three rails with a knob, legible at 20px. */
export const SettingsGlyph: Glyph = (props) => (
  <svg viewBox="0 0 24 24" fill="none" aria-hidden {...props}>
    <path d="M4 7h16M4 12h16M4 17h16" stroke="currentColor" strokeWidth={2.2} strokeLinecap="round" opacity="0.4" />
    <circle cx="9" cy="7" r="2.6" fill="currentColor" />
    <circle cx="15.5" cy="12" r="2.6" fill="currentColor" />
    <circle cx="7.5" cy="17" r="2.6" fill="currentColor" />
  </svg>
)

/** The verdict glyphs: thick, filled, unmistakable at a glance and in greyscale. */
export const ChunkyCheck: Glyph = (props) => (
  <svg viewBox="0 0 24 24" fill="none" aria-hidden {...props}>
    <path d="M4.5 12.8l5 5L19.5 6.5" stroke="currentColor" strokeWidth={3.4} strokeLinecap="round" strokeLinejoin="round" />
  </svg>
)

export const ChunkyCross: Glyph = (props) => (
  <svg viewBox="0 0 24 24" fill="none" aria-hidden {...props}>
    <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" strokeWidth={3.4} strokeLinecap="round" />
  </svg>
)

/** A checkpoint closes a unit — a diamond, so it never reads as just another lesson. */
export const CheckpointGlyph: Glyph = (props) => (
  <svg viewBox="0 0 24 24" fill="none" aria-hidden {...props}>
    <rect x="12" y="2.6" width="13.3" height="13.3" rx="3.4" transform="rotate(45 12 2.6)" fill="currentColor" />
    <circle cx="12" cy="10.7" r="2.6" fill="#ffffff" opacity="0.85" />
  </svg>
)

/**
 * The ink seal: hanpath's one piece of decoration that is not an icon.
 *
 * A Chinese seal is a red square carrying a character — a stamp of completion, and a
 * form that belongs to this subject rather than to another app. Used at the end of a
 * lesson, tinted with the unit's colour, and it carries the level's own character so
 * the mark says something about the work that earned it.
 */
export function InkSeal({
  character = '汉',
  size = 96,
  className = '',
}: {
  character?: string
  size?: number
  className?: string
}) {
  return (
    <span
      className={`relative inline-grid place-items-center ${className}`}
      style={{ width: size, height: size }}
      aria-hidden
    >
      <svg viewBox="0 0 100 100" className="absolute inset-0 h-full w-full">
        {/* Two offset strokes, so the seal reads as pressed rather than painted. */}
        <rect x="6" y="6" width="88" height="88" rx="20" fill="none" stroke="currentColor" strokeWidth="7" opacity="0.35" />
        <rect x="9" y="9" width="82" height="82" rx="17" fill="currentColor" />
      </svg>
      <span
        className="hanzi relative font-bold text-white"
        style={{ fontSize: size * 0.46, lineHeight: 1 }}
      >
        {character}
      </span>
    </span>
  )
}
