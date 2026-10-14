import type { ButtonHTMLAttributes, ReactNode } from 'react'
import type { Tone } from '../lib/labels'

// The component kit. Every colour comes from a semantic token in index.css: a role,
// never a hue. `Tone` is the shared vocabulary between the server's data and the
// screen (see lib/labels.ts), so one status cannot be styled two ways.

const toneClasses: Record<Tone, string> = {
  neutral: 'bg-surface-2 text-ink-2',
  positive: 'bg-success-100 text-success-700',
  warning: 'bg-reward-100 text-reward-700',
  danger: 'bg-danger-100 text-danger-700',
  accent: 'bg-brand-100 text-brand-700',
}

export function Chip({ tone = 'neutral', children }: { tone?: Tone; children: ReactNode }) {
  return (
    <span className={`inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-semibold ${toneClasses[tone]}`}>
      {children}
    </span>
  )
}

export function Card({ children, className = '' }: { children: ReactNode; className?: string }) {
  return (
    <section className={`rounded-card border border-line bg-surface p-5 shadow-card ${className}`}>{children}</section>
  )
}

/** A label above a number: the unit every gamified surface is built from. */
export function StatTile({
  label,
  value,
  hint,
  icon,
  tone = 'neutral',
}: {
  label: string
  value: ReactNode
  hint?: ReactNode
  icon?: ReactNode
  tone?: Tone
}) {
  return (
    <div className="flex items-start gap-3 rounded-card border border-line bg-surface p-4 shadow-tile">
      {icon ? (
        <span className={`grid h-10 w-10 shrink-0 place-items-center rounded-tile ${toneClasses[tone]}`} aria-hidden>
          {icon}
        </span>
      ) : null}
      <div className="min-w-0">
        <p className="text-xs font-semibold uppercase tracking-wide text-ink-3">{label}</p>
        <p className="mt-0.5 text-2xl font-extrabold leading-tight text-ink">{value}</p>
        {hint ? <p className="mt-0.5 text-sm text-ink-2">{hint}</p> : null}
      </div>
    </div>
  )
}

// Every remaining native button attribute is forwarded, so ARIA state reaches the DOM.
// Without the pass-through, `aria-pressed` on a toggle silently vanished: the control
// looked right and announced nothing, and only an assertion on the attribute noticed.
type ButtonProps = {
  children: ReactNode
  onClick?: () => void
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'success'
  disabled?: boolean
  type?: 'button' | 'submit'
  className?: string
  title?: string
} & Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children' | 'onClick' | 'disabled' | 'type' | 'className' | 'title'>

export function Button({
  children,
  onClick,
  variant = 'primary',
  disabled = false,
  type = 'button',
  className = '',
  title,
  ...rest
}: ButtonProps) {
  const base =
    'inline-flex select-none items-center justify-center gap-2 rounded-tile px-4 py-2.5 text-sm font-extrabold tracking-wide transition active:translate-y-px disabled:cursor-not-allowed disabled:opacity-50'
  const variants = {
    // A raised primary: the one obvious action on a screen, with a physical press.
    primary: 'bg-brand-600 text-white shadow-[0_3px_0_var(--color-brand-700)] hover:bg-brand-500 active:shadow-none',
    secondary: 'border-2 border-line-strong bg-surface text-ink hover:bg-surface-2',
    ghost: 'text-ink-2 hover:bg-surface-2',
    danger: 'bg-danger-600 text-white shadow-[0_3px_0_var(--color-danger-700)] hover:bg-danger-500 active:shadow-none',
    // "I knew it" is a positive commitment, so it wears the success colour, not the
    // brand colour reserved for navigation and the primary action of a screen.
    success: 'bg-success-700 text-white hover:bg-success-600',
  } as const
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled}
      title={title}
      className={`${base} ${variants[variant]} ${className}`}
      {...rest}
    >
      {children}
    </button>
  )
}

/** A square icon-only control that still announces itself to a screen reader. */
export function IconButton({
  label,
  onClick,
  children,
  disabled = false,
}: {
  label: string
  onClick?: () => void
  children: ReactNode
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      disabled={disabled}
      className="grid h-10 w-10 place-items-center rounded-tile border border-line bg-surface text-ink-2 transition hover:bg-surface-2 hover:text-ink disabled:opacity-50"
    >
      {children}
    </button>
  )
}

const barFills = {
  success: 'bg-success-600',
  reward: 'bg-reward-700',
  brand: 'bg-brand-600',
  danger: 'bg-danger-600',
} as const

/** A progress bar. `value` is 0..1 and is clamped, so bad input cannot produce a
 *  bar wider than its track. */
export function ProgressBar({
  value,
  tone = 'success',
  label,
  className = '',
}: {
  value: number
  tone?: keyof typeof barFills
  label?: string
  className?: string
}) {
  const pct = Math.max(0, Math.min(1, Number.isFinite(value) ? value : 0)) * 100
  return (
    <div
      className={`track h-3 w-full overflow-hidden rounded-full ${className}`}
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(pct)}
      aria-label={label}
    >
      <div
        className={`h-full rounded-full transition-[width] duration-500 ${barFills[tone]}`}
        style={{ width: `${pct}%` }}
      />
    </div>
  )
}

/** A ring for the two numbers a learner watches most: today's goal and the streak.
 *  The value is written inside it, so colour is never the only signal. */
export function ProgressRing({
  value,
  size = 68,
  stroke = 7,
  tone = 'reward',
  children,
  label,
}: {
  value: number
  size?: number
  stroke?: number
  tone?: keyof typeof barFills
  children?: ReactNode
  label?: string
}) {
  const pct = Math.max(0, Math.min(1, Number.isFinite(value) ? value : 0))
  const radius = (size - stroke) / 2
  const circumference = 2 * Math.PI * radius
  const strokeColour = {
    success: 'var(--color-success-600)',
    reward: 'var(--color-reward-700)',
    brand: 'var(--color-brand-600)',
    danger: 'var(--color-danger-600)',
  }[tone]
  return (
    <div
      className="relative inline-grid place-items-center"
      style={{ width: size, height: size }}
      role="img"
      aria-label={label}
    >
      <svg width={size} height={size} className="-rotate-90" aria-hidden>
        <circle cx={size / 2} cy={size / 2} r={radius} fill="none" stroke="var(--color-track)" strokeWidth={stroke} />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke={strokeColour}
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={circumference * (1 - pct)}
          className="transition-[stroke-dashoffset] duration-500"
        />
      </svg>
      <span className="absolute grid place-items-center text-center leading-none">{children}</span>
    </div>
  )
}

export function ErrorNote({ children }: { children: ReactNode }) {
  return (
    <p
      role="alert"
      className="rounded-tile border border-danger-300 bg-danger-50 px-3 py-2 text-sm font-semibold text-danger-700"
    >
      {children}
    </p>
  )
}

export function Spinner({ label = 'Loading' }: { label?: string }) {
  return (
    <p className="flex items-center gap-2 text-sm text-ink-2">
      <span className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-brand-500 border-t-transparent" />
      {label}…
    </p>
  )
}

/** A placeholder shaped like the thing that is loading, so a slow screen does not
 *  jump when the data lands. */
export function Skeleton({ className = '' }: { className?: string }) {
  return <span className={`block animate-pulse rounded-tile bg-surface-2 ${className}`} aria-hidden />
}

export function EmptyState({ title, hint, icon }: { title: string; hint?: string; icon?: ReactNode }) {
  return (
    <div className="grid place-items-center gap-2 rounded-card border border-dashed border-line-strong p-8 text-center">
      {icon ? (
        <span className="grid h-12 w-12 place-items-center rounded-full bg-brand-50 text-brand-700" aria-hidden>
          {icon}
        </span>
      ) : null}
      <p className="font-bold text-ink">{title}</p>
      {hint ? <p className="text-sm text-ink-2">{hint}</p> : null}
    </div>
  )
}

/** Hanzi at display size, with pinyin above it when the learner wants it. */
export function Hanzi({ children, pinyin, showPinyin }: { children: ReactNode; pinyin?: string; showPinyin?: boolean }) {
  return (
    <span className="inline-flex flex-col items-center leading-none">
      {showPinyin && pinyin ? <span className="mb-1 text-xs font-normal text-ink-3">{pinyin}</span> : null}
      <span className="hanzi text-3xl">{children}</span>
    </span>
  )
}
