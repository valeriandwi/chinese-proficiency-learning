import { describe, expect, it } from 'vitest'
import css from '../index.css?raw'

// The design system's guarantees, asserted rather than trusted.
//
// Two things make this file worth its length: the previous palette failed WCAG AA
// in five places without anything catching it (`ink/40` measured 2.47:1), and a
// "please use the tokens" convention decays the moment someone is in a hurry. Both
// are build failures now.
//
// The stylesheet and the component sources are read through Vite (`?raw` and
// `import.meta.glob`) rather than node:fs, so this test needs no @types/node and
// cannot disagree with what the bundler actually compiles.

function srgb(c: number): number {
  const v = c / 255
  return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4
}

function luminance(hex: string): number {
  const h = hex.replace('#', '')
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16))
  return 0.2126 * srgb(r) + 0.7152 * srgb(g) + 0.0722 * srgb(b)
}

function contrast(foreground: string, background: string): number {
  const a = luminance(foreground)
  const b = luminance(background)
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
}

/** Read a token out of index.css so the test cannot drift from the stylesheet. */
function token(name: string): string {
  const match = css.match(new RegExp(`--color-${name}:\\s*(#[0-9a-fA-F]{6})`))
  if (!match) throw new Error(`index.css does not define --color-${name}`)
  return match[1]
}

const TOKENS = [
  'brand-50',
  'brand-100',
  'brand-300',
  'brand-500',
  'brand-600',
  'brand-700',
  'success-50',
  'success-100',
  'success-300',
  'success-500',
  'success-600',
  'success-700',
  'reward-50',
  'reward-100',
  'reward-300',
  'reward-500',
  'reward-600',
  'reward-700',
  'danger-50',
  'danger-100',
  'danger-300',
  'danger-500',
  'danger-600',
  'danger-700',
  'ink',
  'ink-2',
  'ink-3',
  'ink-4',
  'paper',
  'surface',
  'surface-2',
  'track',
  'line',
  'line-strong',
] as const

describe('design tokens', () => {
  it('defines every semantic token the components rely on', () => {
    for (const name of TOKENS) expect(() => token(name), `--color-${name}`).not.toThrow()
  })

  it('uses a self-hosted rounded sans for the interface and a serif for Hanzi', () => {
    expect(css).toMatch(/--font-sans:[\s\S]*'Nunito Variable'/)
    expect(css).toMatch(/--font-hanzi:[\s\S]*'Noto Serif SC'/)
    // Nothing may be fetched from a third party at runtime.
    expect(css).not.toMatch(/fonts\.googleapis|fonts\.gstatic|cdn\./)
  })

  it('keeps every text pair at or above WCAG AA (4.5:1)', () => {
    const pairs: [string, string, string][] = [
      ['ink', 'paper', 'body on the page background'],
      ['ink', 'surface', 'body on a card'],
      ['ink-2', 'paper', 'secondary text on the page'],
      ['ink-2', 'surface', 'secondary text on a card'],
      ['ink-3', 'paper', 'labels on the page'],
      ['ink-4', 'surface', 'meta text on a card'],
      ['ink-2', 'surface-2', 'text on a tinted surface'],
      ['brand-700', 'surface', 'link on a card'],
      ['brand-700', 'brand-50', 'active navigation'],
      ['brand-700', 'brand-100', 'brand chip'],
      ['success-700', 'surface', 'success link on a card'],
      ['success-700', 'success-50', 'success note'],
      ['success-700', 'success-100', 'success chip'],
      ['reward-700', 'reward-50', 'reward note'],
      ['reward-700', 'reward-100', 'reward chip'],
      ['danger-700', 'danger-50', 'error note'],
      ['danger-700', 'danger-100', 'danger chip'],
      ['danger-600', 'surface', 'danger text on a card'],
    ]
    const failures = pairs
      .map(([fg, bg, label]) => ({ label, ratio: Number(contrast(token(fg), token(bg)).toFixed(2)) }))
      .filter(({ ratio }) => ratio < 4.5)
    expect(failures, 'text pairs below 4.5:1').toEqual([])
  })

  it('keeps white-on-colour buttons at or above WCAG AA', () => {
    // A raised button's label is text like any other, and the previous primary
    // button (white on jade-600) measured 4.27:1 — below the threshold, in shipped
    // code, with nothing to catch it.
    for (const shade of ['brand-600', 'success-700', 'danger-600'] as const) {
      expect(Number(contrast('#ffffff', token(shade)).toFixed(2)), `white on ${shade}`).toBeGreaterThanOrEqual(4.5)
    }
  })

  it('gives every unit family a legible tint, band and fill', () => {
    // One accent per unit, cycling through five families. Each one needs the same
    // guarantees as the rest of the palette: 700 carries white text, the 100 tint is
    // readable ON that 700 band, the 50/100 tints carry 700 text, and 600 is only ever
    // a fill on the track.
    const failures: string[] = []
    for (const family of [1, 2, 3, 4, 5]) {
      for (const shade of ['50', '100', '300', '600', '700']) {
        if (!css.includes(`--color-unit-${family}-${shade}:`)) {
          failures.push(`unit-${family}-${shade} is not defined`)
        }
      }
      const checks: [string, string, number][] = [
        ['unit-' + family + '-700', 'unit-' + family + '-50', 4.5],
        ['unit-' + family + '-700', 'unit-' + family + '-100', 4.5],
        // The band's eyebrow text sits on the band.
        ['unit-' + family + '-100', 'unit-' + family + '-700', 4.5],
        // A fill is measured against the track it renders on, not against white.
        ['unit-' + family + '-600', 'track', 3],
      ]
      for (const [fg, bg, minimum] of checks) {
        const ratio = Number(contrast(token(fg), token(bg)).toFixed(2))
        if (ratio < minimum) failures.push(`${fg} on ${bg} = ${ratio}:1, needs ${minimum}:1`)
      }
      const whiteOnBand = Number(contrast('#ffffff', token('unit-' + family + '-700')).toFixed(2))
      if (whiteOnBand < 4.5) failures.push(`white on unit-${family}-700 = ${whiteOnBand}:1, needs 4.5:1`)
    }
    expect(failures, 'unit palette failures').toEqual([])
  })

  it('defines the motion it uses, and collapses it when motion is reduced', () => {
    // Motion is decoration, never information: every animation here reinforces words
    // that are already on screen. What has to hold is that the names resolve (a typo'd
    // utility compiles to no animation at all, silently) and that the operating
    // system's setting genuinely stops it.
    for (const name of ['pop', 'rise', 'breathe', 'slide-up', 'shake', 'bounce-in', 'fall']) {
      expect(css, `@keyframes ${name}`).toMatch(new RegExp(`@keyframes ${name}\\b`))
    }
    for (const tokenName of ['pop', 'rise', 'breathe', 'slide-up', 'shake', 'bounce-in', 'fall']) {
      expect(css, `--animate-${tokenName}`).toMatch(new RegExp(`--animate-${tokenName}:`))
    }
    const reduced = css.slice(css.indexOf('prefers-reduced-motion: reduce'))
    expect(reduced).toMatch(/animation-duration:\s*0\.001ms/)
    expect(reduced).toMatch(/animation-iteration-count:\s*1/)
    expect(reduced).toMatch(/transition-duration:\s*0\.001ms/)
  })

  it('keeps every meaning-carrying fill and boundary at or above 3:1', () => {
    // Fills sit on the track, not on white: a 500 shade measures 2.1:1 there.
    const nonText: [string, string, string][] = [
      ['success-600', 'track', 'lesson progress fill'],
      ['reward-700', 'track', 'XP / streak fill'],
      ['brand-600', 'track', 'path progress fill'],
      ['danger-600', 'track', 'error progress fill'],
      ['line-strong', 'surface', 'control boundary'],
      ['brand-500', 'surface', 'focus ring'],
    ]
    const failures = nonText
      .map(([fg, bg, label]) => ({ label, ratio: Number(contrast(token(fg), token(bg)).toFixed(2)) }))
      .filter(({ ratio }) => ratio < 3)
    expect(failures, 'fills/boundaries below 3:1').toEqual([])
  })
})

// Component sources, read through the bundler.
const sources = import.meta.glob('../**/*.{ts,tsx}', { query: '?raw', import: 'default', eager: true }) as Record<
  string,
  string
>

const HUE =
  /(?:^|[\s"'`(:])(?:bg|text|border|ring|fill|stroke|from|to|via|decoration|outline|divide|accent|caret)-(?:red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|slate|gray|grey|zinc|neutral|stone|jade|clay)-\d{2,3}\b/

// Text never carries meaning through opacity: `text-ink/40` measured 2.47:1, which
// is why the ramp has named steps. This pattern would otherwise slip past the hue
// ban above, since an alpha modifier on a token is not a raw hue.
const TEXT_ALPHA = /(?:^|[\s"'`(:])text-(?:ink|black|paper|surface)(?:\/\d{1,3}|(?:\/\d{1,3})?\/\[[^\]]+\])/

describe('the token rule', () => {
  it('has no raw hue class in any component', () => {
    const offenders: string[] = []
    for (const [path, contents] of Object.entries(sources)) {
      if (/\.test\./.test(path) || path.includes('index.css')) continue
      contents.split('\n').forEach((line, index) => {
        if (HUE.test(line)) offenders.push(`${path.replace('../', 'src/')}:${index + 1} ${line.trim().slice(0, 90)}`)
      })
    }
    expect(offenders, 'use a semantic token (brand / success / reward / danger / ink / …) instead').toEqual([])
  })

  it('never sets text colour through an opacity modifier', () => {
    const offenders: string[] = []
    for (const [path, contents] of Object.entries(sources)) {
      if (/\.test\./.test(path)) continue
      contents.split('\n').forEach((line, index) => {
        if (TEXT_ALPHA.test(line)) offenders.push(`${path.replace('../', 'src/')}:${index + 1} ${line.trim().slice(0, 90)}`)
      })
    }
    expect(offenders, 'use ink / ink-2 / ink-3 / ink-4 (the ramp is contrast-verified)').toEqual([])
  })
})
