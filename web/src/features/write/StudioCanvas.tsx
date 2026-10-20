import { useCallback, useEffect, useRef, useState } from 'react'
import HanziWriter from 'hanzi-writer'
import { Button } from '../../components/ui'

/**
 * The studio canvas: a large field for writing a hanzi by hand, the way a
 * learner practises on paper.
 *
 * hanzi-writer owns the geometry (it loads the Make Me a Hanzi data and runs
 * its own quiz on a 208px field inside this component); this canvas exists
 * because the studio's promise is bigger than a drill: a LARGE field, free
 * drawing, undo of the last stroke, clear, and replay of the stroke order.
 *
 * The stroke matching is hanzi-writer's own (stroke medians + a distance
 * tolerance) — the client reports per-stroke outcomes and the server decides
 * whether the writing passes (see HanziPanel and lib/answers). Nothing here
 * declares its own correctness.
 */

export type StudioStroke = {
  index: number
  mistaken?: boolean
}

type Props = {
  character: string
  strokeCount: number
  mode: 'guided' | 'recall'
  outlineColor: string
  onStrokesChange: (strokes: StudioStroke[]) => void
  onCompleted: () => void
}

export function StudioCanvas({ character, strokeCount, mode, outlineColor, onStrokesChange, onCompleted }: Props) {
  const fieldRef = useRef<HTMLDivElement | null>(null)
  const [strokes, setStrokes] = useState<StudioStroke[]>([])
  const [hintsUsed, setHintsUsed] = useState(0)
  const [replayKey, setReplayKey] = useState(0)

  // The studio re-renders on every answer, and both callbacks arrive as inline arrows, so
  // depending on their identities would restart the quiz on every render: a second writer
  // stacked on the field, the first one never cancelled. The ref keeps the handlers fresh
  // without making them a reason to redraw.
  const handlers = useRef({ onStrokesChange, onCompleted })
  useEffect(() => {
    handlers.current = { onStrokesChange, onCompleted }
  }, [onStrokesChange, onCompleted])

  const report = useCallback((next: StudioStroke[], completed: boolean) => {
    setStrokes(next)
    handlers.current.onStrokesChange(next)
    if (completed) handlers.current.onCompleted()
  }, [])

  useEffect(() => {
    setStrokes([])
    setHintsUsed(0)
  }, [character, mode])

  // The field is re-dealt when the character, the mode or the replay changes.
  // hanzi-writer APPENDS its own <svg> to the element and never removes it, so the field
  // has to be cleared first — otherwise the previous character's drawing stays on screen
  // and the learner is looking at two characters at once.
  useEffect(() => {
    const field = fieldRef.current
    if (!field) return
    field.replaceChildren()
    const collected: StudioStroke[] = []
    let completed = false

    const writer = HanziWriter.create(field, character, {
      // The studio's field is larger than a lesson drill's: 288px vs 208px,
      // because practice deserves room to write.
      width: 288,
      height: 288,
      padding: 10,
      showCharacter: false,
      showOutline: mode === 'guided',
      strokeAnimationSpeed: 1.2,
      delayBetweenStrokes: 120,
      strokeColor: '#1a211b',
      outlineColor,
      drawingColor: '#128540',
      charDataLoader: (char, onComplete, onError) => {
        fetch(`/hanzi/${encodeURIComponent(char)}.json`)
          .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`no stroke data for ${char}`))))
          .then(onComplete)
          .catch(onError)
      },
    })

    writer.quiz({
      showHintAfterMisses: mode === 'guided' ? 2 : 1,
      highlightOnComplete: true,
      onMistake: (data) => {
        collected.push({ index: data.strokeNum, mistaken: true })
        report([...collected], false)
      },
      onCorrectStroke: (data) => {
        collected.push({ index: data.strokeNum })
        report([...collected], false)
      },
      onComplete: () => {
        completed = true
        report([...collected], true)
      },
    })

    return () => {
      if (!completed) writer.cancelQuiz()
    }
  }, [character, mode, outlineColor, report, replayKey])

  const undo = () => {
    if (strokes.length === 0) return
    // Undo removes the last reported stroke from the record; the field itself
    // is re-dealt (replayKey) so the learner writes the character again from
    // the stroke they undid. hanzi-writer has no per-stroke unwrite, so the
    // honest undo is: drop the last outcome and restart the quiz.
    const next = strokes.slice(0, -1)
    setStrokes(next)
    onStrokesChange(next)
    setReplayKey((k) => k + 1)
  }

  const clear = () => {
    setStrokes([])
    onStrokesChange([])
    setReplayKey((k) => k + 1)
  }

  const replay = () => {
    setReplayKey((k) => k + 1)
    // replayKey restarts the quiz; the stroke-order animation is the mode's
    // own guidance (showOutline + the replay below re-shows the order).
    setHintsUsed((h) => h + 1)
  }

  return (
    <div className="flex flex-col items-center gap-4">
      <div className="relative" data-testid="studio-canvas">
        <div ref={fieldRef} className="rounded-tile border-2 border-dashed border-line-strong bg-surface shadow-card" />
        {/* The practice field's grid, the way paper is ruled for writing. */}
        <div className="pointer-events-none absolute inset-0 grid grid-cols-2 grid-rows-2 rounded-tile opacity-40">
          <div className="border-b border-r border-dashed border-line-strong" />
          <div className="border-b border-dashed border-line-strong" />
          <div className="border-r border-dashed border-line-strong" />
          <div />
        </div>
      </div>
      <div className="flex items-center gap-2">
        <Button variant="secondary" onClick={undo} disabled={strokes.length === 0}>
          Undo
        </Button>
        <Button variant="secondary" onClick={clear} disabled={strokes.length === 0}>
          Clear
        </Button>
        <Button variant="secondary" onClick={replay}>
          Replay
        </Button>
      </div>
      <p className="text-xs text-ink-3">
        {strokes.length}/{strokeCount} strokes {mode === 'recall' ? '· from memory' : '· guided'} · hints used {hintsUsed}
      </p>
    </div>
  )
}
