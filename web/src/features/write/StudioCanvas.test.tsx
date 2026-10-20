import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import type { StudioStroke } from './StudioCanvas'

// hanzi-writer APPENDS its own <svg> to the field element and never removes it. That
// makes the field a piece of state the component has to own: a new character must start
// on a clean field, and a parent re-render must not restart the quiz (which would stack a
// second, invisible quiz on top of the first and keep the old drawing on screen).
//
// Both properties are asserted here against a stub writer, because the real one needs a
// browser canvas — the geometry is hanzi-writer's business, the field's lifecycle is ours.

const { createMock } = vi.hoisted(() => ({ createMock: vi.fn() }))

vi.mock('hanzi-writer', () => ({ default: { create: createMock } }))

const { StudioCanvas } = await import('./StudioCanvas')

function stubWriter() {
  createMock.mockImplementation((el: HTMLElement, character: string) => {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
    svg.setAttribute('data-char', character)
    el.appendChild(svg)
    return { quiz: vi.fn(), cancelQuiz: vi.fn() }
  })
}

function field(): HTMLElement {
  return screen.getByTestId('studio-canvas').querySelector('div') as HTMLElement
}

const props = {
  strokeCount: 2,
  mode: 'guided' as const,
  outlineColor: '#c6ccd6',
  onStrokesChange: (_: StudioStroke[]) => {},
  onCompleted: () => {},
}

beforeEach(() => {
  createMock.mockReset()
  stubWriter()
})

describe('StudioCanvas', () => {
  it('starts each character on a clean field, with no drawing left from the last one', () => {
    const { rerender } = render(<StudioCanvas character="人" {...props} />)

    expect(field().querySelectorAll('svg')).toHaveLength(1)
    expect(field().querySelector('svg')?.getAttribute('data-char')).toBe('人')

    rerender(<StudioCanvas character="好" {...props} />)

    // Exactly one drawing, and it is the new character: the previous svg is gone.
    expect(field().querySelectorAll('svg')).toHaveLength(1)
    expect(field().querySelector('svg')?.getAttribute('data-char')).toBe('好')
  })

  it('does not restart the quiz when the parent re-renders with new callbacks', () => {
    const { rerender } = render(<StudioCanvas character="人" {...props} />)
    expect(createMock).toHaveBeenCalledTimes(1)

    // The studio re-renders on every answer, so these are new function identities even
    // though the drawing has not changed at all.
    rerender(
      <StudioCanvas
        character="人"
        {...props}
        onStrokesChange={() => {}}
        onCompleted={() => {}}
      />,
    )

    expect(createMock).toHaveBeenCalledTimes(1)
    expect(field().querySelectorAll('svg')).toHaveLength(1)
  })

  it('reports the strokes it collects, and completion only when finished', () => {
    const strokes: StudioStroke[][] = []
    let completedCalls = 0
    render(
      <StudioCanvas
        character="人"
        strokeCount={2}
        mode="guided"
        outlineColor="#c6ccd6"
        onStrokesChange={(s) => strokes.push(s)}
        onCompleted={() => {
          completedCalls += 1
        }}
      />,
    )

    const quizOptions = createMock.mock.calls[0][2] as unknown as string
    void quizOptions
    const quiz = createMock.mock.results[0].value.quiz as ReturnType<typeof vi.fn>
    const options = quiz.mock.calls[0][0] as {
      onCorrectStroke: (d: { strokeNum: number }) => void
      onMistake: (d: { strokeNum: number }) => void
      onComplete: () => void
    }

    options.onCorrectStroke({ strokeNum: 0 })
    expect(strokes.at(-1)).toEqual([{ index: 0 }])
    expect(completedCalls).toBe(0)

    options.onMistake({ strokeNum: 1 })
    expect(strokes.at(-1)).toEqual([{ index: 0 }, { index: 1, mistaken: true }])
    expect(completedCalls).toBe(0)

    options.onComplete()
    expect(completedCalls).toBe(1)
  })
})
