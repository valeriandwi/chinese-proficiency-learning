import { beforeEach, describe, expect, it, vi } from 'vitest'
import { usePrefs } from '../stores/prefs'
import { __resetAudioContext, notifyGraded, playCorrectChime, playWrongChime } from './sound'

// The chime is a small thing with three failure modes worth pinning: it must be silent
// when the learner has muted it, it must not explode in a browser with no audio (jsdom,
// and any WebView), and when it does play it must schedule real notes rather than fail
// quietly. There is no assertion about how it sounds — that is a matter of taste, not a
// contract — only that the notes are created and started.

type FakeContext = {
  currentTime: number
  state: string
  destination: object
  createOscillator: () => {
    type: string
    frequency: { value: number }
    connect: (node: unknown) => void
    start: (t: number) => void
    stop: (t: number) => void
  }
  createGain: () => { gain: Record<string, unknown>; connect: (node: unknown) => void }
  resume: () => Promise<void>
}

function installAudioContext() {
  const started: number[] = []
  const frequencies: number[] = []
  let instances = 0
  const ramp = () => ({ setValueAtTime: vi.fn(), exponentialRampToValueAtTime: vi.fn() })

  class Context implements FakeContext {
    currentTime = 0
    state = 'running'
    destination = {}
    resume = vi.fn(async () => {})
    createOscillator() {
      const osc = {
        type: 'sine',
        frequency: {
          get value() {
            return 0
          },
          set value(v: number) {
            frequencies.push(v)
          },
        },
        connect: vi.fn(),
        start: (t: number) => started.push(t),
        stop: vi.fn(),
      }
      return osc
    }
    createGain() {
      return { gain: ramp(), connect: vi.fn() }
    }
  }

  // Counted through a subclass so the test can prove the context is reused.
  class CountingContext extends Context {
    constructor() {
      super()
      instances += 1
    }
  }

  vi.stubGlobal('AudioContext', CountingContext)
  return { started, frequencies, instances: () => instances }
}

beforeEach(() => {
  __resetAudioContext()
  vi.unstubAllGlobals()
  usePrefs.setState({ soundEnabled: true })
})

describe('playCorrectChime', () => {
  it('plays two rising notes when answer sounds are on', () => {
    const { started, frequencies } = installAudioContext()

    playCorrectChime()

    expect(started).toHaveLength(2)
    expect(frequencies).toHaveLength(2)
    // Rising, and the second note starts after the first.
    expect(frequencies[1]).toBeGreaterThan(frequencies[0])
    expect(started[1]).toBeGreaterThan(started[0])
  })

  it('stays silent when the learner has turned answer sounds off', () => {
    const { started } = installAudioContext()
    usePrefs.setState({ soundEnabled: false })

    playCorrectChime()

    expect(started).toHaveLength(0)
  })

  it('does nothing in a browser without the Web Audio API', () => {
    vi.stubGlobal('AudioContext', undefined)
    // jsdom has no AudioContext either, which is the same shape of browser as a WebView
    // with audio disabled: the call must be a no-op, not a crash.
    expect(() => playCorrectChime()).not.toThrow()
  })

  it('does not stack a context per answer', () => {
    const { instances } = installAudioContext()
    playCorrectChime()
    playCorrectChime()
    // One context for a whole session: created lazily on the first answer, then reused.
    expect(instances()).toBe(1)
  })
})

describe('playWrongChime', () => {
  it('falls instead of rising, so a miss is not mistaken for a hit', () => {
    const { started, frequencies } = installAudioContext()

    playWrongChime()

    expect(started).toHaveLength(2)
    expect(frequencies[1]).toBeLessThan(frequencies[0])
  })

  it('stays silent when the learner has turned answer sounds off', () => {
    const { started } = installAudioContext()
    usePrefs.setState({ soundEnabled: false })

    playWrongChime()

    expect(started).toHaveLength(0)
  })
})

describe('notifyGraded', () => {
  it('plays the rising chime for a correct answer', () => {
    const { frequencies } = installAudioContext()

    notifyGraded({ is_correct: true })

    expect(frequencies).toEqual([659.25, 987.77])
  })

  it('plays the falling chime for a wrong answer', () => {
    const { frequencies } = installAudioContext()

    notifyGraded({ is_correct: false })

    expect(frequencies).toEqual([392, 311.13])
  })

  it('stays silent for an answer awaiting review — that is not a verdict', () => {
    const { started } = installAudioContext()

    notifyGraded({ is_correct: null })

    expect(started).toHaveLength(0)
  })

  it('stays silent when there is no result at all', () => {
    const { started } = installAudioContext()

    notifyGraded(undefined)
    notifyGraded(null)

    expect(started).toHaveLength(0)
  })
})
