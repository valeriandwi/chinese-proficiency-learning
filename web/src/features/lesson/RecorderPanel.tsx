import { useCallback, useEffect, useRef, useState } from 'react'
import { Button } from '../../components/ui'
import { ApiError, apiUpload } from '../../api/client'
import type { AnswerPayload } from '../../api/types'

// Records the learner's voice for a speaking exercise, and — when this deployment has
// a pronunciation provider — has it scored.
//
// What this component deliberately does NOT do: decide the score. The numbers come
// from the server, which talks to the provider and stores the result; the panel only
// carries the recording's id into the answer it submits. A client cannot invent a
// score, and the lesson is graded from what the server measured.
//
// What it does for the learner: tells them, before they record, whether their voice
// will leave the device, and afterwards what was measured and what was not. When no
// provider is configured the drill behaves exactly as before — the recording stays
// local, the learner rates themselves, and a human is asked to look at it — and the
// panel says so rather than pretending.
//
// Every failure path is designed rather than discovered: no microphone, permission
// refused, an insecure context where getUserMedia does not exist, a take that came out
// empty, and each scoring outcome (unavailable, unrecognisable, rate limited).

type Status = 'idle' | 'requesting' | 'recording' | 'recorded' | 'denied' | 'unavailable' | 'error'
type Scoring = 'idle' | 'uploading' | 'scored' | 'unavailable' | 'failed'

/** The score the server returned. Every ratio is 0..1; see openapi RecordingScore. */
type RecordingScore = {
  id: string
  provider: string
  overall: number
  passing_band: number
  accuracy?: number
  fluency?: number
  completeness?: number
  prosody?: number
  reference?: string
  words?: { word: string; accuracy: number; error_type?: string }[]
}

type Props = {
  /** The exercise being answered, so the recording can be scored against it. */
  exerciseId: string
  /** Upper bound from the prompt: recording stops automatically at this length. */
  maxSeconds?: number
  /** Reference text and pinyin, for self-comparison after recording. */
  reference: { zh: string; pinyin?: string }
  /**
   * The tone to aim for, when the drill is about a word's tones. A speaking drill on a
   * word IS a tone drill for that word, so the learner is told what to aim for before
   * they record rather than only after being scored.
   */
  toneNote?: string
  /**
   * Whether this drill has a single reference utterance. An open speaking prompt has
   * nothing to compare against, so it is never uploaded: the server would refuse it,
   * and asking would waste the learner's time.
   */
  scoreable?: boolean
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

export function RecorderPanel({
  exerciseId,
  maxSeconds = 20,
  reference,
  toneNote,
  scoreable = false,
  disabled,
  onChange,
}: Props) {
  const [status, setStatus] = useState<Status>('idle')
  const [durationMs, setDurationMs] = useState(0)
  const [rating, setRating] = useState(0)
  const [blobUrl, setBlobUrl] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  // Nothing is reported to the lesson until the learner actually does something.
  // Emitting on mount would submit {duration_ms: 0} and mark the item answered,
  // letting the lesson advance without a single word spoken.
  const [touched, setTouched] = useState(false)

  const [scoring, setScoring] = useState<Scoring>('idle')
  const [score, setScore] = useState<RecordingScore | null>(null)
  const [scoringNote, setScoringNote] = useState<string | null>(null)

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
    const payload: AnswerPayload = { duration_ms: durationMs }
    if (rating) payload.self_rating = rating
    // Only a score the server produced is named. When there is none, the attempt is
    // graded the tier-1 way, which is the honest fallback.
    if (score) payload.recording_id = score.id
    onChange(payload)
    // onChange is a stable callback from the lesson; re-emitting on it would loop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [durationMs, rating, touched, score])

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

  /**
   * Ask the server to score a take. The audio goes up once, is scored, and is not
   * stored anywhere — the panel says so once it has happened.
   */
  const requestScore = useCallback(
    async (blob: Blob) => {
      setScoring('uploading')
      setScoringNote(null)
      setScore(null)
      try {
        const form = new FormData()
        form.append('exercise_id', exerciseId)
        // The filename is a hint for the converter, not a claim about the container.
        form.append('audio', blob, 'take.webm')
        const result = await apiUpload<RecordingScore>('/api/v1/recordings', form)
        setScore(result)
        setScoring('scored')
      } catch (err) {
        const apiError = err instanceof ApiError ? err : null
        switch (apiError?.code) {
          case 'pronunciation_unavailable':
            setScoring('unavailable')
            setScoringNote(
              'This deployment does not score pronunciation, so this drill is self-assessed. Your recording stays in this browser.',
            )
            break
          case 'no_speech':
            setScoring('failed')
            setScoringNote('Nothing was recognised in that take — it may have been silent. The drill still counts as recorded.')
            break
          case 'invalid_audio':
            setScoring('failed')
            setScoringNote('That take could not be read as audio. Record again, or continue without scoring.')
            break
          case 'rate_limited':
          case 'daily_limit_reached':
            setScoring('failed')
            setScoringNote('Scoring is paused for now (too many today). The drill still counts as recorded.')
            break
          default:
            setScoring('failed')
            setScoringNote('Pronunciation scoring is unavailable right now. The drill still counts as recorded.')
        }
      }
    },
    [exerciseId],
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
      // Scored automatically, once per take: the learner came here to be told how it
      // sounded, not to press another button. A re-record replaces the score.
      if (scoreable) void requestScore(blob)
    },
    [releaseStream, requestScore, scoreable],
  )

  const start = async () => {
    setError(null)
    setScore(null)
    setScoring('idle')
    setScoringNote(null)
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
  const weakWords = (score?.words ?? []).filter((w) => w.accuracy < (score?.passing_band ?? 0.7))

  return (
    <div className="flex flex-col gap-3 rounded-2xl border border-line-strong bg-surface p-4">
      <div className="rounded-xl bg-surface-2 px-4 py-3">
        <p className="text-xs font-semibold uppercase tracking-wide text-ink-3">Say this</p>
        <p className="hanzi text-2xl">{reference.zh}</p>
        {reference.pinyin ? <p className="text-sm text-ink-3">{reference.pinyin}</p> : null}
        {toneNote ? (
          <p className="mt-2 border-t border-line pt-2 text-sm text-ink-2">
            <span className="font-semibold text-ink">Tones:</span> {toneNote}
          </p>
        ) : null}
      </div>

      <div className="flex flex-wrap items-center gap-3">
        {status === 'recording' ? (
          <>
            <Button type="button" variant="danger" onClick={stop} disabled={disabled} className="min-h-14 px-8 text-base">
              ■ Stop
            </Button>
            <span className="flex items-center gap-2 text-sm text-danger-700">
              <span className="inline-block h-3 w-3 animate-pulse rounded-full bg-danger-500" aria-hidden />
              recording {elapsedSeconds.toFixed(1)}s / {maxSeconds}s
            </span>
          </>
        ) : (
          <Button type="button" onClick={start} disabled={disabled || status === 'requesting'} className="min-h-14 px-8 text-base">
            {status === 'requesting' ? 'Waiting for the microphone…' : blobUrl ? '● Record again' : '● Record'}
          </Button>
        )}
        <span className="text-xs text-ink-3">
          {maxSeconds}s limit
          {scoreable
            ? ' · scored takes are uploaded, assessed, and not stored'
            : ' · this recording stays in this browser and is never uploaded'}
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
        <div className="flex flex-col gap-3">
          <p className="text-sm text-ink-2">
            Your take ({(durationMs / 1000).toFixed(1)}s). Play it back, then compare it with the reference by playing
            both in turn.
          </p>
          {/* eslint-disable-next-line jsx-a11y/media-has-caption */}
          <audio controls src={blobUrl} data-testid="learner-recording" className="w-full" />

          {scoring === 'uploading' ? <p className="text-sm text-ink-3">Sending it to be scored…</p> : null}

          {scoring === 'scored' && score ? (
            <div className="rounded-2xl border border-line bg-surface-2 px-4 py-3" data-testid="pronunciation-score">
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <p className="text-sm font-bold text-ink">
                  {Math.round(score.overall * 100)}% · {score.overall >= score.passing_band ? 'clear enough' : 'keep practising'}
                </p>
                <p className="text-xs text-ink-3">scored by {score.provider}</p>
              </div>
              <dl className="mt-2 flex flex-wrap gap-4 text-xs text-ink-2">
                <div>
                  <dt className="font-semibold text-ink-3">Accuracy</dt>
                  <dd>{Math.round((score.accuracy ?? 0) * 100)}%</dd>
                </div>
                <div>
                  <dt className="font-semibold text-ink-3">Fluency</dt>
                  <dd>{Math.round((score.fluency ?? 0) * 100)}%</dd>
                </div>
                <div>
                  <dt className="font-semibold text-ink-3">Completeness</dt>
                  <dd>{Math.round((score.completeness ?? 0) * 100)}%</dd>
                </div>
                <div>
                  <dt className="font-semibold text-ink-3">Prosody</dt>
                  <dd>{Math.round((score.prosody ?? 0) * 100)}%</dd>
                </div>
              </dl>
              {weakWords.length > 0 ? (
                <p className="mt-2 text-sm text-ink-2">
                  Worth another go:{' '}
                  {weakWords.map((w) => (
                    <span key={w.word} className="hanzi mr-2 text-lg">
                      {w.word}
                    </span>
                  ))}
                </p>
              ) : null}
              <p className="mt-2 text-xs text-ink-3">
                This measured your sounds, tones and fluency against the reference. It is not an examiner: a low score
                means “hard to recognise”, not “wrong Chinese”. Your recording was not kept.
              </p>
            </div>
          ) : null}

          {scoringNote ? (
            <p className="rounded-xl border border-reward-300 bg-reward-50 px-3 py-2 text-sm text-reward-700">
              {scoringNote}
            </p>
          ) : null}

          <div className="flex flex-col gap-2">
            <p className="text-sm text-ink-2">How close was it? (your own judgement)</p>
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
