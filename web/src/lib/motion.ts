import { useEffect, useRef, useState } from 'react'

/**
 * Motion, in one place.
 *
 * The stylesheet already collapses every animation and transition when the operating
 * system asks for reduced motion, so nothing here is load-bearing for correctness.
 * What this adds is the two cases CSS cannot express: a component that should not
 * *render* its decoration at all (confetti falling and sticking at its end state is
 * worse than no confetti), and a number that should simply appear rather than count.
 */
export function useReducedMotion(): boolean {
  const [reduced, setReduced] = useState(() => matchesReducedMotion())

  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const query = window.matchMedia('(prefers-reduced-motion: reduce)')
    const update = () => setReduced(query.matches)
    update()
    query.addEventListener('change', update)
    return () => query.removeEventListener('change', update)
  }, [])

  return reduced
}

function matchesReducedMotion(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/**
 * Counts from 0 to `value`, for a reward that lands rather than blinks into place.
 *
 * The first render already shows the final number, so the value is correct before any
 * interval runs — a learner who never sees a frame of the animation still sees the
 * right score, and a reduced-motion learner sees exactly one frame.
 */
export function useCountUp(value: number, durationMs = 700): number {
  const reduced = useReducedMotion()
  const [shown, setShown] = useState(value)
  const previous = useRef(value)

  useEffect(() => {
    if (reduced) {
      previous.current = value
      setShown(value)
      return
    }
    const from = previous.current
    previous.current = value
    if (from === value) {
      setShown(value)
      return
    }
    const started = Date.now()
    const timer = window.setInterval(() => {
      const progress = Math.min(1, (Date.now() - started) / durationMs)
      // Ease out, so the last few points are the slow ones worth watching.
      const eased = 1 - (1 - progress) ** 3
      setShown(Math.round(from + (value - from) * eased))
      if (progress >= 1) window.clearInterval(timer)
    }, 40)
    return () => window.clearInterval(timer)
  }, [value, durationMs, reduced])

  return reduced ? value : shown
}
