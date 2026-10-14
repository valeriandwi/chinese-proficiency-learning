import { useEffect, useRef, useState } from 'react'
import HanziWriter from 'hanzi-writer'
import type { AnswerPayload } from '../../api/types'
import { hanziPayload, type StrokeReport } from '../../lib/answers'
import { Button } from '../../components/ui'

// Hanzi writing practice.
//
// hanzi-writer draws the character, animates stroke order and runs the quiz. The
// stroke geometry it uses comes from the Make Me a Hanzi dataset, which is
// distributed under the Arphic Public License; the files are served from
// /hanzi/<character>.json and the licence text ships alongside them (see
// scripts/copy-hanzi-data.mjs and web/public/hanzi/LICENSE.txt).
//
// The client reports per-stroke outcomes; the server decides whether the writing
// passes. Nothing here is allowed to declare its own correctness.

export function HanziPanel({
  character,
  strokeCount,
  mode,
  disabled,
  onAnswer,
}: {
  character: string
  strokeCount: number
  mode: 'guided' | 'recall' | 'sentence'
  disabled?: boolean
  onAnswer: (payload: AnswerPayload) => void
}) {
  const targetRef = useRef<HTMLDivElement | null>(null)
  const answersRef = useRef<AnswerPayload>({})
  const hintsRef = useRef(0)
  const [strokeDataMissing, setStrokeDataMissing] = useState(false)
  const [hintsUsed, setHintsUsed] = useState(0)

  const report = (strokes: StrokeReport[], completed: boolean) => {
    const payload = hanziPayload(character, strokeCount, strokes, completed, hintsRef.current)
    answersRef.current = payload
    onAnswer(payload)
  }

  useEffect(() => {
    if (!targetRef.current || strokeDataMissing) return
    const strokes: StrokeReport[] = []
    hintsRef.current = 0
    setHintsUsed(0)

    const writer = HanziWriter.create(targetRef.current, character, {
      width: 208,
      height: 208,
      padding: 6,
      showCharacter: false,
      // Guided tracing keeps a faint outline; recall hides it.
      showOutline: mode === 'guided',
      strokeAnimationSpeed: 1,
      delayBetweenStrokes: 180,
      strokeColor: '#14181f',
      outlineColor: '#c6ccd6',
      drawingColor: '#4338ca',
      charDataLoader: (char, onComplete, onError) => {
        fetch(`/hanzi/${encodeURIComponent(char)}.json`)
          .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`no stroke data for ${char}`))))
          .then(onComplete)
          .catch(() => {
            setStrokeDataMissing(true)
            onError(new Error(`no stroke data for ${char}`))
          })
      },
    })

    writer.quiz({
      showHintAfterMisses: mode === 'guided' ? 2 : 1,
      highlightOnComplete: true,
      onMistake: (data) => {
        strokes.push({ index: data.strokeNum, mistaken: true })
        report(strokes, false)
      },
      onCorrectStroke: (data) => {
        strokes.push({ index: data.strokeNum })
        report(strokes, false)
      },
      onComplete: () => {
        report(strokes, true)
      },
    })

    return () => {
      writer.cancelQuiz()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [character, mode, strokeDataMissing])

  const showMe = () => {
    hintsRef.current += 1
    setHintsUsed(hintsRef.current)
    const writer = HanziWriter.create(targetRef.current!, character, {
      width: 208,
      height: 208,
      padding: 6,
      showCharacter: true,
      showOutline: mode === 'guided',
      strokeColor: '#14181f',
      charDataLoader: (char, onComplete, onError) => {
        fetch(`/hanzi/${encodeURIComponent(char)}.json`)
          .then((r) => (r.ok ? r.json() : Promise.reject(new Error('missing'))))
          .then(onComplete)
          .catch(onError)
      },
    })
    writer.animateCharacter({
      onComplete: () => {
        // Rebuild the quiz grid so the learner can write immediately afterwards.
        setStrokeDataMissing(false)
      },
    })
    report((answersRef.current.strokes as StrokeReport[]) ?? [], false)
  }

  if (strokeDataMissing) {
    return (
      <p className="rounded-xl border border-reward-300 bg-reward-50 px-3 py-3 text-sm text-reward-700">
        Stroke data for {character} is not in this build, so writing cannot be practised here. Run
        <code className="mx-1 rounded bg-surface-2 px-1">make web-hanzi-data</code>
        to copy it from the hanzi-writer-data package.
      </p>
    )
  }

  return (
    <div className="flex flex-col items-start gap-3">
      <div className="relative">
        <div ref={targetRef} className="rounded-xl border border-dashed border-line-strong bg-surface" />
        <div className="pointer-events-none absolute inset-0 grid grid-cols-2 grid-rows-2 opacity-40">
          <div className="border-b border-r border-dashed border-line-strong" />
          <div className="border-b border-dashed border-line-strong" />
          <div className="border-r border-dashed border-line-strong" />
          <div />
        </div>
      </div>
      <div className="flex items-center gap-2">
        <Button variant="secondary" onClick={showMe} disabled={disabled || hintsUsed >= 3}>
          Show me ({3 - hintsUsed} left)
        </Button>
        <span className="text-sm text-ink-3">
          {strokeCount} strokes{mode === 'recall' ? ' · from memory' : ''}
        </span>
      </div>
      {hintsUsed > 0 ? (
        <p className="text-xs text-ink-3">
          Hints used: {hintsUsed}. In “write from memory” drills a hint halves the credit.
        </p>
      ) : null}
    </div>
  )
}
