/** The loudest sample below which a take counts as silent: a muted or wrong microphone
 *  records near-zero samples, not literal zeros. */
export const SILENCE_FLOOR = 0.01

/** Reduces audio samples to `bars` heights in 0..1, each the loudest sample in its slice,
 *  scaled so the loudest bar is 1. An all-silent take stays flat rather than being amplified
 *  into noise. */
export function peaks(samples: Float32Array, bars: number): number[] {
  const size = Math.max(1, Math.floor(samples.length / bars))
  const raw: number[] = []
  for (let i = 0; i < bars; i++) {
    let max = 0
    for (let j = i * size; j < Math.min(samples.length, (i + 1) * size); j++) max = Math.max(max, Math.abs(samples[j]))
    raw.push(max)
  }
  const loudest = Math.max(...raw)
  return loudest < SILENCE_FLOOR ? raw.map(() => 0) : raw.map((v) => v / loudest)
}
