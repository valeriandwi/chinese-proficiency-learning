import type { ReactElement, SVGProps } from 'react'

/**
 * Original illustration for the skills hub's doors — the same geometry the icon
 * set is built from (circles, rounded rects, plain strokes), scaled up, so the
 * doors read as part of the same hand that drew the icons and the path nodes.
 * Nothing here is derived from another product's illustrations: these are
 * hanpath's own marks, built from the characters' own forms (a brush stroke, a
 * speech arc, an open book) reduced to plain geometry.
 *
 * Every glyph inherits `currentColor`, so a door tints its art with the role it
 * owns — brand green for the studios, ink for the reference surfaces.
 */

type DoorArt = (props: SVGProps<SVGSVGElement>) => ReactElement

/** 写 — a field grid with one confident stroke crossing it. */
export const WriteDoorArt: DoorArt = (props) => (
  <svg viewBox="0 0 120 72" fill="none" aria-hidden {...props}>
    {/* The practice field: a 2x2 grid like the hanzi canvas. */}
    <rect x="30" y="6" width="60" height="60" rx="10" stroke="currentColor" strokeWidth="3.5" opacity="0.5" />
    <path d="M50 6v60M70 6v60M30 26h60M30 46h60" stroke="currentColor" strokeWidth="1.6" opacity="0.35" strokeDasharray="3 5" />
    {/* The stroke: one confident sweep, hanzi-writer's drawing colour. */}
    <path d="M38 52c10 2 18-8 24-18s14-16 22-18" stroke="currentColor" strokeWidth="5" strokeLinecap="round" />
    <circle cx="84" cy="16" r="4.5" fill="currentColor" />
  </svg>
)

/** 说 — a speech arc meeting a sound wave, the shape of speaking. */
export const SpeakDoorArt: DoorArt = (props) => (
  <svg viewBox="0 0 120 72" fill="none" aria-hidden {...props}>
    {/* The mouth: an open arc, the shape speech makes. */}
    <path d="M36 44c8 10 40 10 48 0" stroke="currentColor" strokeWidth="5" strokeLinecap="round" />
    {/* The sound: three rising bars, the pitch of a spoken syllable. */}
    <rect x="44" y="22" width="6" height="12" rx="3" fill="currentColor" opacity="0.45" />
    <rect x="57" y="16" width="6" height="18" rx="3" fill="currentColor" opacity="0.7" />
    <rect x="70" y="10" width="6" height="24" rx="3" fill="currentColor" />
  </svg>
)

/** 读 — an open book, its lines of text, and one line being read. */
export const ReadDoorArt: DoorArt = (props) => (
  <svg viewBox="0 0 120 72" fill="none" aria-hidden {...props}>
    <path d="M14 20c14-6 32-6 44 2 12-8 30-8 44-2" stroke="currentColor" strokeWidth="5" strokeLinecap="round" />
    <path d="M58 22v40c-8-6-22-8-34-5M62 22v40c8-6 22-8 34-5" stroke="currentColor" strokeWidth="3.4" strokeLinecap="round" opacity="0.5" />
    <path d="M24 34h22M24 44h16M78 34h22M78 44h16" stroke="currentColor" strokeWidth="3" strokeLinecap="round" opacity="0.4" />
    {/* The line being read right now. */}
    <path d="M24 54h22" stroke="currentColor" strokeWidth="4.4" strokeLinecap="round" />
  </svg>
)
