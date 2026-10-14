import { useCallback, useEffect, useRef, useState } from 'react'
import { Button } from '../../components/ui'
import type { AnswerPayload } from '../../api/types'

// Records the learner's voice for a speaking exercise.
//
// What this component deliberately does NOT do: grade. There is no speech
// recognition in this build, so the panel collects a recording the learner can play
// back against the reference, plus their own rating, and submits only metadata
// (duration + self-rating). The audio never leaves the browser — it lives in a blob
// URL and is revoked on unmount — so there is no learner-voice storage and no
// privacy surface to reason about.
//
// Every failure path is designed rather than discovered: no microphone, permission
// refused, an insecure context where getUserMedia does not exist, or a take that
// came out empty. Each one tells the learner what happened and offers the honest
// way forward.

type Status = 'idle' | 'requesting' | 'recording' | 'recorded' | 'denied' | 'unavailable' | 'error'

type Props = {
  /** Upper bound from the prompt: recording stops automatically at this length. */
  maxSeconds?: number
  /** Reference text and pinyin, for self-comparison after recording. */
  reference: { zh: string; pinyin?: string }
  disabled?: boolean
  onChange: (payload: AnswerPayload) => void
}

const RATINGS = [
  { value: 1, label: 'Rough' },
  { value: 2, label: 'Shaky' },
  { value: 3, label: 'OK' },
  { value: 4, label: 'Good' },
  { value: 5, label: 'Nailed it' },
]

export function RecorderPanel({ maxSeconds = 20, reference, disabled, onChange }: Props) {
  const [status, setStatus] = useState<Status>('idle')
  const [durationMs, setDurationMs] = useState(0)
  const [rating, setRating] = useState(0)
  const [blobUrl, setBlobUrl] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  // Nothing is reported to the lesson until the learner actually does something.
  // Emitting on mount would submit {duration_ms: 0} and mark the item answered,
  // letting the lesson advance without a single word spoken.
  const [touched, setTouched] = useState(false)

  const recorderRef = useRef<MediaRecorder | null>(null)
  const chunksRef = useRef<Blob[]>([])
  const streamRef = useRef<MediaStream | null>(null)
  const startedAtRef = useRef(0)
  const timerRef = useRef<number | null>(null)
  const blobUrlRef = useRef<string | null>(null)

  // Emit every time the checkable facts change, so the lesson's "answered" state
  // follows the recording rather than a separate button the learner could skip.
  useEffect(() => {
    if (!touched) return
    onChange(rating ? { duration_ms: durationMs, self_rating: rating } : { duration_ms: durationMs })
    // onChange is a stable callback from the lesson; re-emitting on it would loop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [durationMs, rating, touched])

  const stopTimer = () => {
    if (timerRef.current !== null) {
      window.clearInterval(timerRef.current)
      timerRef.current = null
    }
  }

  const releaseStream = useCallback(() => {
    streamRef.current?.getTracks().forEach((track) => track.stop())
    streamRef.current = null
  }, [])

  // Leaving the exercise must stop the microphone (the browser's recording
  // indicator is a promise to the learner) and release the blob URL.
  useEffect(
    () => () => {
      stopTimer()
      if (recorderRef.current?.state === 'recording') recorderRef.current.stop()
      releaseStream()
      if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current)
    },
    [releaseStream],
  )

  const finish = useCallback(
    (ms: number) => {
      stopTimer()
      const elapsed = ms > 0 ? ms : Date.now() - startedAtRef.current
      const blob = new Blob(chunksRef.current, { type: 'audio/webm' })
      chunksRef.current = []
      releaseStream()

      if (blob.size === 0) {
        // An empty take is not a recording. Saying so beats submitting 0 ms and
        // letting the learner believe it counted.
        setStatus('error')
        setError('The recording came out empty. Check that the right microphone is selected, then try again.')
        return
      }
      if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current)
      const url = URL.createObjectURL(blob)
      blobUrlRef.current = url
      setBlobUrl(url)
      setDurationMs(elapsed)
      setTouched(true)
      setStatus('recorded')
    },
    [releaseStream],
  )

  const start = async () => {
    setError(null)
    if (typeof navigator === 'undefined' || !navigator.mediaDevices?.getUserMedia) {
      // Happens on an insecure origin: getUserMedia is not merely forbidden, it is
      // absent, so this needs its own message rather than a permission prompt.
      setStatus('unavailable')
      return
    }
    setStatus('requesting')
    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true })
    } catch (err) {
      const name = (err as DOMException)?.name
      if (name === 'NotAllowedError' || name === 'SecurityError') {
        setStatus('denied')
      } else if (name === 'NotFoundError' || name === 'OverconstrainedError') {
        setStatus('unavailable')
        setError('No microphone was found on this device.')
      } else {
        setStatus('error')
        setError('The microphone could not be opened. Try again, or continue without recording.')
      }
      return
    }

    streamRef.current = stream
    chunksRef.current = []
    const recorder = new MediaRecorder(stream)
    recorder.ondataavailable = (event) => {
      if (event.data?.size) chunksRef.current.push(event.data)
    }
    recorder.onstop = () => finish(0)
    recorderRef.current = recorder
    startedAtRef.current = Date.now()
    recorder.start()

    timerRef.current = window.setInterval(() => {
      const elapsed = Date.now() - startedAtRef.current
      if (elapsed >= maxSeconds * 1000) {
        // Stop at the authored limit rather than letting the take run on.
        if (recorderRef.current?.state === 'recording') recorderRef.current.stop()
      }
    }, 200)
    setDurationMs(0)
    setStatus('recording')
  }

  const stop = () => {
    if (recorderRef.current?.state === 'recording') recorderRef.current.stop()
  }

  const elapsedSeconds = durationMs / 1000

  return (
    <div className="flex flex-col gap-3 rounded-xl border border-line-strong bg-surface p-4">
      <div className="rounded-xl bg-surface-2 px-4 py-3">
        <p className="text-xs uppercase tracking-wide text-ink-3">Say this</p>
        <p className="hanzi text-2xl">{reference.zh}</p>
        {reference.pinyin ? <p className="text-sm text-ink-3">{reference.pinyin}</p> : null}
      </div>

      <div className="flex flex-wrap items-center gap-3">
        {status === 'recording' ? (
          <>
            <Button type="button" variant="danger" onClick={stop} disabled={disabled}>
              ■ Stop
            </Button>
            <span className="flex items-center gap-2 text-sm text-danger-700">
              <span className="inline-block h-3 w-3 animate-pulse rounded-full bg-danger-500" aria-hidden />
              recording {elapsedSeconds.toFixed(1)}s / {maxSeconds}s
            </span>
          </>
        ) : (
          <Button type="button" onClick={start} disabled={disabled || status === 'requesting'}>
            {status === 'requesting' ? 'Waiting for the microphone…' : blobUrl ? '● Record again' : '● Record'}
          </Button>
        )}
        <span className="text-xs text-ink-3">
          {maxSeconds}s limit · your recording stays in this browser and is not uploaded
        </span>
      </div>

      {status === 'denied' ? (
        <p className="rounded-xl border border-reward-300 bg-reward-50 px-3 py-2 text-sm text-reward-700">
          Microphone access is blocked, so this drill cannot be recorded. Nothing about your Chinese is lost by
          skipping it — the exercise will be marked as not recorded.
        </p>
      ) : null}

      {status === 'unavailable' ? (
        <p className="rounded-xl border border-reward-300 bg-reward-50 px-3 py-2 text-sm text-reward-700">
          {error ?? 'This browser cannot record audio here. A secure (https) page and a microphone are required.'}
        </p>
      ) : null}

      {status === 'error' && error ? (
        <p className="rounded-xl border border-danger-300 bg-danger-50 px-3 py-2 text-sm text-danger-700">{error}</p>
      ) : null}

      {status === 'recorded' && blobUrl ? (
        <div className="flex flex-col gap-2">
          <p className="text-sm text-ink-2">
            Your take ({(durationMs / 1000).toFixed(1)}s). Play it back, then compare it with the reference by playing
            both in turn.
          </p>
          {/* eslint-disable-next-line jsx-a11y/media-has-caption */}
          <audio controls src={blobUrl} data-testid="learner-recording" className="w-full" />
          <div className="flex flex-col gap-2">
            <p className="text-sm text-ink-2">How close was it? (your own judgement — nothing here grades tones)</p>
            <div className="flex flex-wrap gap-2">
              {RATINGS.map((option) => (
                <button
                  key={option.value}
                  type="button"
                  disabled={disabled}
                  onClick={() => {
                    setRating(option.value)
                    setTouched(true)
                  }}
                  className={`rounded-lg border px-3 py-1.5 text-sm transition ${
                    rating === option.value
                      ? 'border-brand-500 bg-brand-50 font-semibold'
                      : 'border-line-strong bg-surface hover:border-brand-300'
                  }`}
                >
                  {option.value} · {option.label}
                </button>
              ))}
            </div>
          </div>
        </div>
      ) : null}

      {status !== 'recording' && status !== 'recorded' ? (
        <button
          type="button"
          disabled={disabled}
          onClick={() => {
            // An explicit, honest skip: the server records a 0 ms attempt and
            // scores it as not recorded, which is what actually happened.
            setDurationMs(0)
            setStatus('idle')
            setTouched(true)
          }}
          className="self-start text-xs text-ink-3 underline hover:text-ink"
        >
          I cannot record right now — continue without a recording
        </button>
      ) : null}
    </div>
  )
}
