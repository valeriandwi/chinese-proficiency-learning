import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Mascot, MascotMark } from './Mascot'

// The mascot is decorative everywhere it appears next to text, and the one place it stands
// alone it must introduce itself. Those two rules, plus "it holds a small square", are what
// is worth pinning: the drawing itself is a matter of taste, not a contract.

describe('Mascot', () => {
  it('is decorative by default, so it adds nothing to a screen reader', () => {
    const { container } = render(<Mascot />)
    const svg = container.querySelector('svg')

    expect(svg).not.toBeNull()
    expect(svg?.getAttribute('aria-hidden')).toBe('true')
    expect(svg?.getAttribute('role')).toBeNull()
    expect(container.querySelector('title')).toBeNull()
  })

  it('introduces itself when it stands alone', () => {
    render(<Mascot title="hanpath, your study buddy" />)

    const img = screen.getByRole('img', { name: 'hanpath, your study buddy' })
    expect(img.tagName.toLowerCase()).toBe('svg')
  })

  it('draws a different face when it is cheering', () => {
    const { container: idle } = render(<Mascot mood="idle" />)
    const { container: cheer } = render(<Mascot mood="cheer" />)

    const shapeCount = (root: HTMLElement) => root.querySelectorAll('svg *').length
    // Reading the attribute instead of a CSS selector: jsdom's selector parser trips over
    // the parentheses in var(--color-ink).
    const inkCircles = (root: HTMLElement) =>
      [...root.querySelectorAll('svg circle')].filter(
        (c) =>
          c.getAttribute('fill') === 'var(--color-ink)' ||
          c.parentElement?.getAttribute('fill') === 'var(--color-ink)',
      ).length

    expect(shapeCount(cheer)).toBeGreaterThan(shapeCount(idle))
    // Cheer has no round pupils left: the eyes become arcs.
    expect(inkCircles(idle)).toBe(2)
    expect(inkCircles(cheer)).toBe(0)
  })

  it('keeps a square box at any size, so it cannot distort in a flex row', () => {
    const { container } = render(<Mascot size={24} />)
    const svg = container.querySelector('svg')

    expect(svg?.getAttribute('width')).toBe('24')
    expect(svg?.getAttribute('height')).toBe('24')
    expect(svg?.getAttribute('viewBox')).toBe('0 0 48 48')
  })
})

describe('MascotMark', () => {
  it('is a square tile that hides the whole mark from assistive tech', () => {
    const { container } = render(<MascotMark size={30} />)
    const tile = container.firstElementChild as HTMLElement

    expect(tile.getAttribute('aria-hidden')).toBe('true')
    expect(tile.style.width).toBe('30px')
    expect(tile.style.height).toBe('30px')
    expect(tile.className).toContain('bg-brand-600')
    expect(container.querySelector('svg')).not.toBeNull()
    // Fifteen white trail dots and one yellow goal.
    expect(container.querySelectorAll('svg circle')).toHaveLength(16)
    expect(container.querySelector('svg circle:last-child')?.getAttribute('fill')).toBe('var(--color-reward-500)')
  })
})
