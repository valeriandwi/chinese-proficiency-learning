import { Link } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'

/** The studios are reached through Practice and have no tab of their own, so each one says
 *  where it came from and offers the way back. */
export function BackToPractice() {
  return (
    <Link
      to="/skills"
      className="inline-flex min-h-8 items-center gap-1.5 text-xs font-bold text-ink-2 transition hover:text-ink"
    >
      <ArrowLeft size={14} aria-hidden /> Practice
    </Link>
  )
}
