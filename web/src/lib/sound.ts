import { usePrefs } from '../stores/prefs'

// The correct-answer sound.
//
// Synthesized with the Web Audio API rather than shipped as a file: no asset to load, no
// network request, no licence to track, and it cannot fall out of step with the brand.
// Two rising notes (a fifth apart), ~180ms, soft — the visual verdict already says
// "Correct!", so this is emphasis, never information.
//
// Rules it follows:
//   • silent when the learner has turned answer sounds off (the preference is persisted),
//   • silent when the browser has no AudioContext (no crash, no console noise),
//   • created lazily on the first answer, because browsers only allow audio after a
//     gesture and the answer click IS that gesture,
//   • never queued or repeated: a burst of correct answers just overlays short blips.

type AudioContextCtor = new () => AudioContext

let context: AudioContext | null = null

function audioContext(): AudioContext | null {
  if (typeof window === 'undefined') return null
  const ctor =
    (window as unknown as { AudioContext?: AudioContextCtor; webkitAudioContext?: AudioContextCtor }).AudioContext ??
    (window as unknown as { webkitAudioContext?: AudioContextCtor }).webkitAudioContext
  if (!ctor) return null
  if (!context) context = new ctor()
  return context
}

/** The notes: [frequency, start offset, duration] in Hz/seconds. Rising, so it reads as
 *  progress rather than as an alarm. */
const CORRECT_NOTES: readonly [number, number, number][] = [
  [659.25, 0, 0.09], // E5
  [987.77, 0.075, 0.11], // B5
]

/** A wrong answer falls instead of rising. A minor third down (G4 → E♭4) on a triangle
 *  wave: it is unmistakably "not that one" without being a buzzer — the learner is
 *  already being told by the band and the shake, and this sound repeats at the speed they
 *  answer, so it has to stay soft. */
const WRONG_NOTES: readonly [number, number, number][] = [
  [392.0, 0, 0.1], // G4
  [311.13, 0.11, 0.16], // Eb4
]

type Waveform = OscillatorType

function schedule(notes: readonly [number, number, number][], type: Waveform, peak: number): void {
  if (!usePrefs.getState().soundEnabled) return
  const audio = audioContext()
  if (!audio) return
  // A context created before a gesture (or after the tab was backgrounded) starts
  // suspended; resuming on the next answer is enough to make the first one audible.
  if (audio.state === 'suspended') void audio.resume()

  const start = audio.currentTime
  for (const [frequency, offset, duration] of notes) {
    const oscillator = audio.createOscillator()
    const gain = audio.createGain()
    oscillator.type = type
    oscillator.frequency.value = frequency
    // A short attack and a smooth decay: an abrupt stop clicks.
    const t = start + offset
    gain.gain.setValueAtTime(0.0001, t)
    gain.gain.exponentialRampToValueAtTime(peak, t + 0.012)
    gain.gain.exponentialRampToValueAtTime(0.0001, t + duration)
    oscillator.connect(gain)
    gain.connect(audio.destination)
    oscillator.start(t)
    oscillator.stop(t + duration + 0.02)
  }
}

export function playCorrectChime(): void {
  schedule(CORRECT_NOTES, 'sine', 0.12)
}

export function playWrongChime(): void {
  // Slightly quieter than the correct chime: a miss should not feel like a punishment.
  schedule(WRONG_NOTES, 'triangle', 0.1)
}

/** One entry point for "the server graded this answer".
 *
 *  Correct plays the rising chime, a definite miss plays the falling one, and anything
 *  else is silent: `is_correct === null` means the answer is waiting for review (free-form
 *  writing), which is not a verdict and must not be scored by ear. */
export function notifyGraded(result: { is_correct?: boolean | null } | null | undefined): void {
  if (result?.is_correct === true) playCorrectChime()
  else if (result?.is_correct === false) playWrongChime()
}

/** Test seam: forget the cached context (never called by the app). */
export function __resetAudioContext(): void {
  context = null
}
