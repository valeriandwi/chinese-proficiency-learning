import type { CSSProperties } from 'react'

// The hanpath mascot: a moon rabbit (玉兔), the companion of Chang'e in the Mid-Autumn
// story. It is drawn from circles and capsules only, for three reasons:
//
//   • it has to read at 24px in the header and at 72px on the sign-in card,
//   • flat vector means no asset to load, no licence to track, no raster blur,
//   • every colour comes from the design tokens, so it re-skins with the app.
//
// It is deliberately NOT a bird: the reference design language in this project has an owl,
// and the whole point of a mascot is that it belongs to this product.
//
// Decorative everywhere it appears: the wordmark, the heading or the aria-label next to it
// carries the meaning, so the SVG itself is aria-hidden and costs a screen reader nothing.

export type MascotMood = 'idle' | 'cheer'

type MascotProps = {
  size?: number
  mood?: MascotMood
  /** Words the mascot is saying, for the one place it stands alone. */
  title?: string
  className?: string
  style?: CSSProperties
  /** Outline colour that cuts the silhouette out of whatever sits behind it (the logo's moon). */
  rim?: string
}

const FUR = 'var(--color-surface)'
const INK = 'var(--color-ink)'
const EAR = 'var(--color-reward-500)'
const CHEEK = 'var(--color-danger-300)'

export function Mascot({ size = 40, mood = 'idle', title, className = '', style, rim }: MascotProps) {
  const cheering = mood === 'cheer'
  // Gold inner ears vanish against the gold moon, so the rimmed (logo) rabbit goes pink.
  const ear = rim ? CHEEK : EAR
  const outline = rim ? { stroke: rim, strokeWidth: 1.8 } : {}
  // Below 40px the cheeks and the celebration ticks turn into noise at 1x: the silhouette,
  // the eyes and the nose are what survives, so those are the only things drawn.
  const detailed = size >= 40

  return (
    <svg
      viewBox="0 0 48 48"
      width={size}
      height={size}
      className={className}
      style={style}
      role={title ? 'img' : undefined}
      aria-hidden={title ? undefined : true}
      aria-label={title}
      focusable="false"
    >
      {title ? <title>{title}</title> : null}

      {/* Ears: the tilt is the character — one alert, one relaxed. The inner shape is kept
          short and high so its base never reaches the forehead, which at 30px turns the ear
          into a mark on the face. */}
      <g>
        <ellipse cx="18.6" cy="15" rx="3.7" ry="9.4" transform="rotate(-11 18.6 24)" fill={FUR} {...outline} />
        <ellipse cx="18.4" cy="13.4" rx="1.7" ry="5" transform="rotate(-11 18.6 24)" fill={ear} opacity="0.9" />
        <ellipse cx="29.4" cy="14.4" rx="3.7" ry="9.4" transform="rotate(13 29.4 24)" fill={FUR} {...outline} />
        <ellipse cx="29.6" cy="12.8" rx="1.7" ry="5" transform="rotate(13 29.4 24)" fill={ear} opacity="0.9" />
      </g>

      {/* Head */}
      <circle cx="24" cy="30.5" r="12.4" fill={FUR} {...outline} />

      {/* Eyes */}
      {cheering ? (
        <g stroke={INK} strokeWidth="1.5" strokeLinecap="round" fill="none">
          <path d="M18.2 30.4c.7-1.1 2.3-1.1 3 0" />
          <path d="M26.8 30.4c.7-1.1 2.3-1.1 3 0" />
        </g>
      ) : (
        <g fill={INK}>
          <circle cx="19.7" cy="30" r="1.7" />
          <circle cx="28.3" cy="30" r="1.7" />
        </g>
      )}

      {/* Cheeks — warmth, and a face that is not just two dots. */}
      {detailed ? (
        <g fill={CHEEK} opacity="0.65">
          <circle cx="16.6" cy="33.6" r="1.7" />
          <circle cx="31.4" cy="33.6" r="1.7" />
        </g>
      ) : null}

      {/* Nose and mouth */}
      <path d="M22.9 33.7h2.2l-1.1 1.2z" fill={INK} />
      {cheering ? (
        <path d="M21.9 36.4a2.6 2.6 0 0 0 4.2 0z" fill={INK} />
      ) : (
        <path d="M22.3 36.6c.9 1 2.5 1 3.4 0" stroke={INK} strokeWidth={detailed ? 1.2 : 1.7} strokeLinecap="round" fill="none" />
      )}

      {/* Cheer ticks, only when celebrating — and only where there is room for them. */}
      {cheering && detailed ? (
        <g stroke={FUR} strokeWidth="1.6" strokeLinecap="round" opacity="0.9">
          <path d="M6.5 20.5 8.9 21.9" />
          <path d="M41.5 20.5 39.1 21.9" />
        </g>
      ) : null}
    </svg>
  )
}

/** The logo mark: the moon rabbit in front of its full moon, on the brand tile. It has to
 *  hold a 30px square without turning to mush; the rim keeps rabbit and moon apart there.
 *  Keep in step with the favicon in web/index.html. */
export function MascotMark({ size = 30, className = '' }: { size?: number; className?: string }) {
  return (
    <span
      aria-hidden
      className={`grid shrink-0 place-items-center overflow-hidden rounded-tile bg-brand-600 ${className}`}
      style={{ width: size, height: size }}
    >
      <svg viewBox="0 0 48 48" width={size} height={size} className="[grid-area:1/1]" focusable="false">
        <circle cx="24" cy="21.5" r="17" fill="var(--color-reward-500)" />
      </svg>
      <Mascot size={size} rim="var(--color-brand-600)" className="[grid-area:1/1]" />
    </span>
  )
}
