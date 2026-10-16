import { useReducedMotion } from '../lib/motion'

/**
 * A celebration burst, with no dependency and no third-party artwork.
 *
 * Twenty-two rectangles are positioned around the top of the frame and given their own
 * drift and spin through CSS custom properties, so one keyframe animates all of them.
 * Colours come from the unit palette, which is why the confetti belongs to the app
 * rather than to a library's default rainbow.
 *
 * It renders nothing at all when the operating system asks for reduced motion: pieces
 * that fall and stop mid-air are worse than no pieces, and the celebration screen
 * already states the result in words and numbers.
 */

const PIECES = 22

/** Deterministic: the same celebration looks the same twice, so screenshots and tests
 *  do not depend on a random seed. */
function drift(index: number) {
  const spread = ((index * 37) % 100) / 100 - 0.5
  return {
    left: 8 + ((index * 61) % 84),
    dx: Math.round(spread * 90),
    dy: 130 + ((index * 29) % 70),
    rot: 160 + ((index * 53) % 220),
    delay: ((index * 17) % 24) / 100,
    width: index % 3 === 0 ? 8 : 6,
    height: index % 4 === 0 ? 10 : 14,
    tone: `var(--color-unit-${(index % 5) + 1}-600)`,
  }
}

export function Confetti({ className = '' }: { className?: string }) {
  const reduced = useReducedMotion()
  if (reduced) return null

  return (
    <div className={`pointer-events-none absolute inset-x-0 top-0 h-40 overflow-hidden ${className}`} aria-hidden>
      {Array.from({ length: PIECES }, (_, index) => {
        const piece = drift(index)
        return (
          <span
            key={index}
            className="animate-fall absolute top-0 rounded-[2px]"
            style={{
              left: `${piece.left}%`,
              width: piece.width,
              height: piece.height,
              backgroundColor: piece.tone,
              animationDelay: `${piece.delay}s`,
              ['--dx' as string]: `${piece.dx}px`,
              ['--dy' as string]: `${piece.dy}px`,
              ['--rot' as string]: `${piece.rot}deg`,
            }}
          />
        )
      })}
    </div>
  )
}
