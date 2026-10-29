import { useEffect, useId, useRef, useState } from 'react'
import { Check, ChevronDown } from 'lucide-react'

export type SelectOption<V extends string | number> = { value: V; label: string }

/**
 * A listbox in the app's own clothes, in place of the browser's native <select>, whose menu
 * cannot be styled and looks different on every platform.
 *
 * It follows the WAI-ARIA select-only combobox pattern: focus stays on the button, the open
 * list is a `listbox` whose active option is named by `aria-activedescendant`, and the
 * keyboard does what a native select does — arrows, Home/End, Enter/Space to choose, Escape
 * to close, and typing a letter jumps to the next option that starts with it.
 *
 * Name it with `label` (an aria-label), or wrap it in a <label> that carries visible text.
 */
export function Select<V extends string | number>({
  value,
  options,
  onChange,
  label,
  id,
  align = 'left',
  className = '',
}: {
  value: V
  options: SelectOption<V>[]
  onChange: (value: V) => void
  label?: string
  id?: string
  /** Which edge of the button the open list lines up with. */
  align?: 'left' | 'right'
  className?: string
}) {
  const listId = useId()
  const rootRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLUListElement>(null)
  const [open, setOpen] = useState(false)
  const selectedIndex = Math.max(
    0,
    options.findIndex((option) => option.value === value),
  )
  const [active, setActive] = useState(selectedIndex)
  const selected = options[selectedIndex]

  // Click or tap outside closes the list; the button itself is inside the root.
  useEffect(() => {
    if (!open) return
    const onDown = (event: MouseEvent | TouchEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('touchstart', onDown)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('touchstart', onDown)
    }
  }, [open])

  // Keep the highlighted option visible in a long list (time zones run to hundreds).
  useEffect(() => {
    if (open) listRef.current?.children[active]?.scrollIntoView?.({ block: 'nearest' })
  }, [open, active])

  const show = () => {
    setActive(selectedIndex)
    setOpen(true)
  }
  const choose = (index: number) => {
    const option = options[index]
    if (option) onChange(option.value)
    setOpen(false)
  }

  const onKeyDown = (event: React.KeyboardEvent) => {
    const last = options.length - 1
    switch (event.key) {
      case 'ArrowDown':
      case 'ArrowUp': {
        event.preventDefault()
        if (!open) return show()
        setActive((i) => Math.min(last, Math.max(0, i + (event.key === 'ArrowDown' ? 1 : -1))))
        return
      }
      case 'Home':
      case 'End':
        if (!open) return
        event.preventDefault()
        setActive(event.key === 'Home' ? 0 : last)
        return
      case 'Enter':
      case ' ':
        event.preventDefault()
        if (open) choose(active)
        else show()
        return
      case 'Escape':
        if (open) {
          event.preventDefault()
          setOpen(false)
        }
        return
      case 'Tab':
        setOpen(false)
        return
      default: {
        if (event.key.length !== 1 || event.metaKey || event.ctrlKey || event.altKey) return
        const from = open ? active + 1 : selectedIndex + 1
        const ordered = [...options.slice(from), ...options.slice(0, from)]
        const hit = ordered.find((o) => o.label.toLowerCase().startsWith(event.key.toLowerCase()))
        if (!hit) return
        const index = options.indexOf(hit)
        if (open) setActive(index)
        else onChange(hit.value)
      }
    }
  }

  return (
    <div ref={rootRef} className={`relative inline-block ${className}`}>
      <button
        id={id}
        type="button"
        role="combobox"
        aria-label={label}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listId}
        aria-activedescendant={open ? `${listId}-${active}` : undefined}
        onClick={() => (open ? setOpen(false) : show())}
        onKeyDown={onKeyDown}
        className={`flex min-h-11 w-full items-center justify-between gap-3 rounded-tile border-2 border-b-4 bg-surface px-3.5 text-left font-extrabold text-ink transition ${
          open ? 'border-info-500' : 'border-line hover:border-line-strong'
        }`}
      >
        <span className="truncate">{selected?.label}</span>
        <ChevronDown
          size={18}
          strokeWidth={3}
          aria-hidden
          className={`shrink-0 text-ink-3 transition-transform ${open ? 'rotate-180' : ''}`}
        />
      </button>

      {open ? (
        <ul
          ref={listRef}
          id={listId}
          role="listbox"
          aria-label={label}
          className={`absolute z-30 mt-2 max-h-64 min-w-full overflow-y-auto rounded-card border-2 border-b-4 border-line bg-surface p-1.5 shadow-raised animate-pop ${
            align === 'right' ? 'right-0' : 'left-0'
          }`}
        >
          {options.map((option, index) => {
            const isSelected = index === selectedIndex
            return (
              <li
                key={String(option.value)}
                id={`${listId}-${index}`}
                role="option"
                aria-selected={isSelected}
                onMouseEnter={() => setActive(index)}
                onClick={() => choose(index)}
                className={`flex min-h-11 cursor-pointer items-center justify-between gap-4 whitespace-nowrap rounded-tile px-3 font-extrabold ${
                  isSelected ? 'text-brand-700' : 'text-ink-2'
                } ${index === active ? (isSelected ? 'bg-brand-50' : 'bg-surface-2') : isSelected ? 'bg-brand-50' : ''}`}
              >
                {option.label}
                {isSelected ? <Check size={16} strokeWidth={3} aria-hidden /> : null}
              </li>
            )
          })}
        </ul>
      ) : null}
    </div>
  )
}
