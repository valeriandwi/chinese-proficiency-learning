import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { RecorderPanel } from './RecorderPanel'

// jsdom has no microphone and no MediaRecorder, so both are stubbed — the states under
// test (a scored take, a deployment with no provider, an unrecognisable take) are
// exactly the ones a learner meets in a real browser, and skipping them would hide the
// whole feature.

class FakeMediaRecorder {
  static behaviour: 'ok' | 'empty' = 'ok'
  state: 'inactive' | 'recording' = 'inactive'
  ondataavailable: ((event: { data: Blob }) => void) | null = null
  onstop: (() => void) | null = null

  constructor(public stream: MediaStream) {}

  start() {
    this.state = 'recording'
  }

  stop() {
    this.state = 'inactive'
    if (FakeMediaRecorder.behaviour === 'ok') {
      this.ondataavailable?.({ data: new Blob(['fake audio bytes'], { type: 'audio/webm' }) })
    }
    this.onstop?.()
  }
}

function fakeStream() {
  return { getTracks: () => [{ stop: vi.fn() }] } as unknown as MediaStream
}

function scorePayload(overrides: Record<string, unknown> = {}) {
  return {
    id: 'rec-scored-1',
    provider: 'test-provider',
    overall: 0.82,
    passing_band: 0.7,
    accuracy: 0.84,
    fluency: 0.79,
    completeness: 0.9,
    prosody: 0.7,
    reference: '你好',
    words: [
      { word: '你', accuracy: 0.95, error_type: 'None' },
      { word: '好', accuracy: 0.3, error_type: 'Mispronunciation' },
    ],
    ...overrides,
  }
}

function mockUpload(status: number, body: unknown) {
  // The parameters are declared so the mock's call arguments are typed: a bare
  // `async () => …` records `[]` per call and reading the URL out of it does not compile.
  const fetchMock = vi.fn(
    async (_url: string, _init?: RequestInit) =>
      new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }),
  )
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

/** Record and stop, which is what a learner does to produce a take. */
async function recordOnce() {
  fireEvent.click(screen.getByRole('button', { name: /^● record$/i }))
  const stop = await screen.findByRole('button', { name: /^■ stop$/i })
  fireEvent.click(stop)
}

function lastPayload(onChange: ReturnType<typeof vi.fn>) {
  const calls = onChange.mock.calls
  return calls.length === 0 ? null : (calls[calls.length - 1][0] as Record<string, unknown>)
}

describe('scoring a spoken take', () => {
  beforeEach(() => {
    FakeMediaRecorder.behaviour = 'ok'
    vi.stubGlobal('MediaRecorder', FakeMediaRecorder)
    Object.defineProperty(globalThis.URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:fake') })
    Object.defineProperty(globalThis.URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn().mockResolvedValue(fakeStream()) },
    })
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('tells the learner their voice will be sent, before they record', () => {
    render(<RecorderPanel exerciseId="ex-1" scoreable reference={{ zh: '你好' }} onChange={vi.fn()} />)
    expect(screen.getByText(/scored takes are uploaded, assessed, and not stored/i)).toBeInTheDocument()
  })

  it('scores the take and names the recording in the answer', async () => {
    const fetchMock = mockUpload(200, scorePayload())
    const onChange = vi.fn()
    render(<RecorderPanel exerciseId="ex-1" scoreable reference={{ zh: '你好' }} onChange={onChange} />)

    await recordOnce()

    // The measured numbers are shown, including the word worth practising again.
    expect(await screen.findByTestId('pronunciation-score')).toBeInTheDocument()
    expect(screen.getByText(/scored by test-provider/)).toBeInTheDocument()
    expect(screen.getByText(/82%/)).toBeInTheDocument()
    expect(screen.getByText(/Worth another go/)).toBeInTheDocument()
    expect(screen.getByText('好')).toBeInTheDocument()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(String(fetchMock.mock.calls[0][0])).toContain('/api/v1/recordings')

    // The answer names the recording — and nothing else about the score. The lesson is
    // graded from what the server stored, so the client has no numbers to forge.
    const payload = lastPayload(onChange)
    expect(payload?.recording_id).toBe('rec-scored-1')
    expect(payload).not.toHaveProperty('score')
    expect(payload).not.toHaveProperty('overall')
  })

  it('falls back to self-assessment when the deployment has no provider', async () => {
    mockUpload(503, { error: { code: 'pronunciation_unavailable', message: 'no provider configured' } })
    const onChange = vi.fn()
    render(<RecorderPanel exerciseId="ex-1" scoreable reference={{ zh: '你好' }} onChange={onChange} />)

    await recordOnce()

    expect(await screen.findByText(/does not score pronunciation/i)).toBeInTheDocument()
    expect(screen.queryByTestId('pronunciation-score')).not.toBeInTheDocument()
    // Crucially, no recording id: the attempt is graded the tier-1 way, and a human is
    // asked to look at it rather than a score being invented.
    expect(lastPayload(onChange)?.recording_id).toBeUndefined()
  })

  it('reports a take that was not recognised instead of inventing a score', async () => {
    mockUpload(422, { error: { code: 'no_speech', message: 'no speech recognised' } })
    const onChange = vi.fn()
    render(<RecorderPanel exerciseId="ex-1" scoreable reference={{ zh: '你好' }} onChange={onChange} />)

    await recordOnce()

    expect(await screen.findByText(/Nothing was recognised in that take/i)).toBeInTheDocument()
    expect(lastPayload(onChange)?.recording_id).toBeUndefined()
  })

  it('never uploads an open speaking prompt', async () => {
    const fetchMock = mockUpload(200, scorePayload())
    render(<RecorderPanel exerciseId="ex-2" reference={{ zh: '介绍一下你自己' }} onChange={vi.fn()} />)

    await recordOnce()

    // Nothing to compare against, so the server would refuse it — the client does not
    // ask, and the copy keeps the promise the tier-1 drill always made.
    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.getByText(/stays in this browser and is never uploaded/i)).toBeInTheDocument()
    expect(screen.queryByTestId('pronunciation-score')).not.toBeInTheDocument()
  })

  it('keeps the take usable when scoring is rate limited', async () => {
    mockUpload(429, { error: { code: 'daily_limit_reached', message: 'cap reached' } })
    const onChange = vi.fn()
    render(<RecorderPanel exerciseId="ex-1" scoreable reference={{ zh: '你好' }} onChange={onChange} />)

    await recordOnce()

    expect(await screen.findByText(/Scoring is paused for now/i)).toBeInTheDocument()
    // The drill still counts: the learner recorded something, and that is what the
    // lesson needs.
    await vi.waitFor(() => expect(lastPayload(onChange)?.duration_ms).toBeGreaterThanOrEqual(0))
    expect(lastPayload(onChange)?.recording_id).toBeUndefined()
  })
})
