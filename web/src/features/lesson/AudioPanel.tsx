import { useEffect, useRef, useState } from 'react'
import { Button } from '../../components/ui'

// Plays the reference audio for a listening or speaking exercise.
//
// Two rules this component exists to hold:
//
//   * The Chinese text is NOT rendered before the learner answers. It arrives as
//     `reveal`, which the caller only passes once a result exists — so a listening
//     item cannot be solved by reading the page, and the transcript stays on the
//     server until the attempt is graded.
//   * A replay limit is enforced in the player, so exam conditions mean something.
//     A failed load is shown as an error rather than leaving a dead button: an item
//     whose audio cannot be fetched is unanswerable and must say so.

type Props = {
  url: string
  replayLimit?: number
  disabled?: boolean
  /** Shown only after the learner has answered. */
  reveal?: { zh: string; pinyin?: string } | null
}

export function AudioPanel({ url, replayLimit = 3, disabled, reveal }: Props) {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [plays, setPlays] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [failed, setFailed] = useState(false)
  const exhausted = plays >= replayLimit

  // A new exercise means a new clip: reset the counter and the failure state, or
  // the next question would inherit the previous one's replay budget.
  useEffect(() => {
    setPlays(0)
    setPlaying(false)
    setFailed(false)
  }, [url])

  if (failed) {
    return (
      <div className="rounded-xl border border-danger-300 bg-danger-50 px-4 py-3 text-sm text-danger-700">
        <p className="font-semibold">The audio for this question could not be loaded.</p>
        <p className="mt-1">
          This is a problem on our side, not yours — the reference clip is missing. Report it rather than guessing the
          answer.
        </p>
      </div>
    )
  }

  const play = async () => {
    const el = audioRef.current
    if (!el || exhausted || disabled) return
    try {
      el.currentTime = 0
      await el.play()
      setPlays((n) => n + 1)
    } catch {
      setFailed(true)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-3">
        <Button type="button" onClick={play} disabled={disabled || exhausted || playing}>
          {playing ? 'Playing…' : plays === 0 ? '▶ Play' : '▶ Play again'}
        </Button>
        <span className="text-xs text-ink-3">
          {exhausted
            ? `No replays left (${plays}/${replayLimit}) — answer with what you heard`
            : `plays used ${plays}/${replayLimit}`}
        </span>
        {/* preload="none": a lesson can hold many clips, and fetching them all up
            front would download audio for questions the learner never reaches. */}
        <audio
          ref={audioRef}
          src={url}
          preload="none"
          data-testid="reference-audio"
          onPlay={() => setPlaying(true)}
          onEnded={() => setPlaying(false)}
          onPause={() => setPlaying(false)}
          onError={() => setFailed(true)}
          className="hidden"
        />
      </div>

      {reveal ? (
        <div className="rounded-xl bg-surface-2 px-4 py-3">
          <p className="text-xs uppercase tracking-wide text-ink-3">What you heard</p>
          <p className="hanzi text-2xl">{reveal.zh}</p>
          {reveal.pinyin ? <p className="text-sm text-ink-3">{reveal.pinyin}</p> : null}
        </div>
      ) : (
        <p className="text-xs text-ink-3">The transcript appears after you answer.</p>
      )}
    </div>
  )
}
