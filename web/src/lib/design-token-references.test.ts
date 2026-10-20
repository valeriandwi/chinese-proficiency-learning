/// <reference types="vite/client" />
import { describe, expect, it } from 'vitest'

// A var() that names a token nobody declared fails *silently*: CSS drops the declaration
// and SVG paints the default black. That is exactly how the mascot's inner ears came out
// charcoal instead of yellow — nothing errored, the drawing was just wrong. This test reads
// every source file and refuses any --color token that index.css does not define.

const sources = import.meta.glob(['/src/**/*.{ts,tsx}', '!/src/**/*.test.*'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

const css = import.meta.glob('/src/index.css', { query: '?raw', import: 'default', eager: true }) as Record<
  string,
  string
>

const declared = new Set(
  [...(Object.values(css)[0] ?? '').matchAll(/--color-([a-z0-9-]+)\s*:/g)].map((match) => match[1]),
)

describe('design token references', () => {
  it('declares a colour token for every var(--color-…) a component uses', () => {
    const unknown: string[] = []
    const dynamic = new Set<string>()

    for (const [path, text] of Object.entries(sources)) {
      for (const match of text.matchAll(/var\(--color-([a-z0-9-]+)/g)) {
        const token = match[1]
        if (token.endsWith('-')) {
          // A name built at runtime (var(--color-unit-${n}-600)): the suffix is not in the
          // source, so the family is checked instead — see the test below.
          dynamic.add(token)
          continue
        }
        if (!declared.has(token)) unknown.push(`${path} uses --color-${token}`)
      }
    }

    expect([...new Set(unknown)]).toEqual([])
  })

  it('resolves every token family that is assembled at runtime', () => {
    const dynamic = new Set<string>()
    for (const text of Object.values(sources)) {
      for (const match of text.matchAll(/var\(--color-([a-z0-9-]+)/g)) {
        if (match[1].endsWith('-')) dynamic.add(match[1])
      }
    }

    // Pinned: a new runtime-assembled family has to be looked at, not slipped in.
    expect([...dynamic].sort()).toEqual(['unit-'])
    for (const family of dynamic) {
      expect([...declared].filter((token) => token.startsWith(family)).length).toBeGreaterThan(0)
    }
  })

  it('finds the tokens and the sources it is meant to be checking', () => {
    // Guards the guard: an empty glob or an unparsed stylesheet would make the test above
    // pass while checking nothing at all.
    expect(declared.size).toBeGreaterThan(40)
    expect(Object.keys(sources).length).toBeGreaterThan(30)
  })
})
