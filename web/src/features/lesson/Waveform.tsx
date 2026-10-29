import { useEffect, useState } from 'react'
import { peaks } from '../../lib/waveform'

const BARS = 48

/** The learner's own take, drawn from the real decoded samples: where they started, how long
 *  they spoke, and whether anything was captured at all. A take with no sound says so, since a
 *  muted or wrong microphone is the commonest reason a recording is useless. Where the browser
 *  cannot decode the clip, nothing is drawn — the audio player beside it is unaffected. */
export function Waveform({ url }: { url: string }) {
  const [bars, setBars] = useState<number[] | null>(null)

  useEffect(() => {
    let alive = true
    setBars(null)
    void (async () => {
      try {
        const Context = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
        if (!Context) return
        const context = new Context()
        const audio = await context.decodeAudioData(await (await fetch(url)).arrayBuffer())
        void context.close()
        if (alive) setBars(peaks(audio.getChannelData(0), BARS))
      } catch {
        /* undecodable: leave the player on its own */
      }
    })()
    return () => {
      alive = false
    }
  }, [url])

  if (!bars) return null
  const silent = bars.every((b) => b === 0)
  return (
    <div className="flex flex-col gap-1">
      <div
        role="img"
        aria-label={silent ? 'Your take is silent' : 'Your recording as a waveform'}
        className="flex h-10 items-center gap-[2px]"
      >
        {bars.map((b, i) => (
          <span
            key={i}
            aria-hidden
            className={`min-w-px flex-1 rounded-full ${silent ? 'bg-line-strong' : 'bg-brand-500'}`}
            style={{ height: `${Math.max(6, b * 100)}%` }}
          />
        ))}
      </div>
      {silent ? (
        <p className="text-sm font-semibold text-danger-700">
          Nothing was captured in that take. Check that the right microphone is selected and unmuted, then record again.
        </p>
      ) : null}
    </div>
  )
}
