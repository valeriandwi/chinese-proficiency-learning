import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AudioPanel } from './AudioPanel'
import { RecorderPanel } from './RecorderPanel'

// jsdom has no audio playback and no microphone. Both are stubbed here rather than
// skipped, because the states under test (replay limit, permission refusal, an
// empty take) are exactly the paths a learner hits in a real browser.

class FakeMediaRecorder {
  static instances: FakeMediaRecorder[] = []
  static behaviour: 'ok' | 'empty' = 'ok'

  state: 'inactive' | 'recording' = 'inactive'
  ondataavailable: ((event: { data: Blob }) => void) | null = null
  onstop: (() => void) | null = null

  constructor(public stream: MediaStream) {
    FakeMediaRecorder.instances.push(this)
  }

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
  const track = { stop: vi.fn(), kind: 'audio' }
  return { getTracks: () => [track], track } as unknown as MediaStream & { track: { stop: () => void } }
}

describe('AudioPanel', () => {
  beforeEach(() => {
    vi.stubGlobal('HTMLMediaElement', window.HTMLMediaElement)
    Object.defineProperty(window.HTMLMediaElement.prototype, 'play', {
      configurable: true,
      value: vi.fn().mockResolvedValue(undefined),
    })
  })
  afterEach(() => vi.unstubAllGlobals())

  it('never shows the transcript before the learner has answered', async () => {
    // The whole point of a listening item: the text is the answer, and it only
    // arrives in the payload after grading.
    render(<AudioPanel url="/media/a.mp3" reveal={null} />)
    expect(screen.queryByText('谢谢你。')).not.toBeInTheDocument()
    expect(screen.getByText(/transcript appears after you answer/i)).toBeInTheDocument()
  })

  it('reveals the transcript once a result exists', () => {
    render(<AudioPanel url="/media/a.mp3" reveal={{ zh: '谢谢你。', pinyin: 'Xièxie nǐ.' }} />)
    expect(screen.getByText('谢谢你。')).toBeInTheDocument()
    expect(screen.getByText('Xièxie nǐ.')).toBeInTheDocument()
  })

  it('counts plays and stops at the replay limit', async () => {
    render(<AudioPanel url="/media/a.mp3" replayLimit={2} />)
    const play = screen.getByRole('button', { name: /^▶ play$/i })

    await userEvent.click(play)
    await waitFor(() => expect(screen.getByText(/plays used 1\/2/)).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /^▶ play again$/i }))
    await waitFor(() => expect(screen.getByText(/no replays left \(2\/2\)/i)).toBeInTheDocument())
    expect(screen.getByRole('button', { name: /^▶ play again$/i })).toBeDisabled()
  })

  it('loads the clip lazily rather than downloading every exercise up front', () => {
    render(<AudioPanel url="/media/a.mp3" />)
    expect(screen.getByTestId('reference-audio')).toHaveAttribute('preload', 'none')
  })

  it('says so when the audio cannot be fetched, instead of leaving a dead button', async () => {
    render(<AudioPanel url="/media/missing.mp3" />)
    fireEvent.error(screen.getByTestId('reference-audio'))
    await waitFor(() => expect(screen.getByText(/could not be loaded/i)).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /^▶ play/i })).not.toBeInTheDocument()
  })
})

describe('RecorderPanel', () => {
  let stream: ReturnType<typeof fakeStream>

  beforeEach(() => {
    FakeMediaRecorder.instances = []
    FakeMediaRecorder.behaviour = 'ok'
    stream = fakeStream()
    vi.stubGlobal('MediaRecorder', FakeMediaRecorder)
    Object.defineProperty(globalThis.URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:fake') })
    Object.defineProperty(globalThis.URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    // @ts-expect-error deleting a stubbed property for the unsupported-context case
    delete navigator.mediaDevices
  })

  it('submits nothing until the learner acts, so a drill cannot be skipped silently', () => {
    const onChange = vi.fn()
    render(<RecorderPanel
        exerciseId="ex-speak" reference={{ zh: '你好' }} onChange={onChange} />)
    // Mounting must not report an answer: a 0 ms payload would satisfy isAnswered
    // and the lesson would advance without a word spoken.
    expect(onChange).not.toHaveBeenCalled()
  })

  it('records with the microphone, then reports duration and releases the stream', async () => {
    const onChange = vi.fn()
    render(<RecorderPanel
        exerciseId="ex-speak" reference={{ zh: '你好' }} onChange={onChange} />)

    await userEvent.click(screen.getByRole('button', { name: /^● record$/i }))
    await waitFor(() => expect(screen.getByText(/recording \d+\.\d+s/)).toBeInTheDocument())

    await userEvent.click(screen.getByRole('button', { name: /^■ stop$/i }))
    await waitFor(() => expect(onChange).toHaveBeenCalled())

    const payload = onChange.mock.calls.at(-1)?.[0]
    expect(payload.duration_ms).toBeGreaterThan(0)
    expect(payload).not.toHaveProperty('is_correct')
    expect(screen.getByTestId('learner-recording')).toBeInTheDocument()
    // The microphone must be released as soon as the take is finished.
    expect(stream.track.stop).toHaveBeenCalled()
  })

  it('sends the learner own rating, and only as a rating', async () => {
    const onChange = vi.fn()
    render(<RecorderPanel
        exerciseId="ex-speak" reference={{ zh: '你好' }} onChange={onChange} />)

    await userEvent.click(screen.getByRole('button', { name: /^● record$/i }))
    await userEvent.click(screen.getByRole('button', { name: /^■ stop$/i }))
    await waitFor(() => expect(screen.getByTestId('learner-recording')).toBeInTheDocument())

    await userEvent.click(screen.getByRole('button', { name: /4 · good/i }))
    const payload = onChange.mock.calls.at(-1)?.[0]
    expect(payload.self_rating).toBe(4)
    expect(Object.keys(payload).sort()).toEqual(['duration_ms', 'self_rating'])
  })

  it('treats a refused microphone as a designed state, not a crash', async () => {
    const denied = Object.assign(new Error('denied'), { name: 'NotAllowedError' })
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn().mockRejectedValue(denied) },
    })
    const onChange = vi.fn()
    render(<RecorderPanel
        exerciseId="ex-speak" reference={{ zh: '你好' }} onChange={onChange} />)

    await userEvent.click(screen.getByRole('button', { name: /^● record$/i }))
    await waitFor(() => expect(screen.getByText(/microphone access is blocked/i)).toBeInTheDocument())
    expect(onChange).not.toHaveBeenCalled()
  })

  it('explains an insecure context where getUserMedia does not exist at all', async () => {
    // @ts-expect-error simulating a non-secure origin
    delete navigator.mediaDevices
    render(<RecorderPanel
        exerciseId="ex-speak" reference={{ zh: '你好' }} onChange={vi.fn()} />)

    await userEvent.click(screen.getByRole('button', { name: /^● record$/i }))
    expect(screen.getByText(/secure \(https\) page/i)).toBeInTheDocument()
  })

  it('refuses to call an empty take a recording', async () => {
    FakeMediaRecorder.behaviour = 'empty'
    const onChange = vi.fn()
    render(<RecorderPanel
        exerciseId="ex-speak" reference={{ zh: '你好' }} onChange={onChange} />)

    await userEvent.click(screen.getByRole('button', { name: /^● record$/i }))
    await userEvent.click(screen.getByRole('button', { name: /^■ stop$/i }))

    await waitFor(() => expect(screen.getByText(/came out empty/i)).toBeInTheDocument())
    expect(onChange).not.toHaveBeenCalled()
  })

  it('offers an honest skip that submits an unrecorded attempt', async () => {
    const onChange = vi.fn()
    render(<RecorderPanel
        exerciseId="ex-speak" reference={{ zh: '你好' }} onChange={onChange} />)

    await userEvent.click(screen.getByRole('button', { name: /cannot record/i }))
    expect(onChange).toHaveBeenCalledWith({ duration_ms: 0 })
    // A skip is not a recording, and the panel must not imply one.
    expect(screen.queryByTestId('learner-recording')).not.toBeInTheDocument()
  })

  it('stops an in-flight recording when the learner leaves the exercise', async () => {
    const onChange = vi.fn()
    const { unmount } = render(<RecorderPanel
        exerciseId="ex-speak" reference={{ zh: '你好' }} onChange={onChange} />)
    await userEvent.click(screen.getByRole('button', { name: /^● record$/i }))
    await waitFor(() => expect(screen.getByText(/recording \d+\.\d+s/)).toBeInTheDocument())

    unmount()
    expect(FakeMediaRecorder.instances.at(-1)?.state).toBe('inactive')
    expect(stream.track.stop).toHaveBeenCalled()
  })
})
