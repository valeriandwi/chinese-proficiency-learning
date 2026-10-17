import { useAddBookmark, useRemoveBookmark, useVocabulary } from '../../api/hooks'
import { Bookmark, BookmarkCheck } from 'lucide-react'
import { Button } from '../../components/ui'

/**
 * The bookmark control, shared by the study guide and the reader.
 *
 * Bookmarks key on a lexeme id. The guide's rows carry theirs; the reader's
 * tapped words carry only the characters, so this button resolves the word
 * through the vocabulary search — a real lookup against real data, never a
 * guessed id — and states plainly when the word is not in the curriculum.
 * Nothing here fabricates a bookmark target.
 */
export function BookmarkButton({
  lexemeId,
  simplified,
  level = 0,
  bookmarked = false,
}: {
  lexemeId?: string
  simplified: string
  level?: number
  bookmarked?: boolean
}) {
  const add = useAddBookmark()
  const remove = useRemoveBookmark()

  // Only the unresolved path (the reader) pays for the search: a known id skips it.
  const search = lexemeId ? null : useVocabulary(level, simplified, 0)
  const resolved = lexemeId ?? search?.data?.items.find((w) => w.simplified === simplified)?.id
  const isBookmarked = lexemeId ? bookmarked : false

  if (lexemeId) {
    return (
      <Button
        variant={isBookmarked ? 'success' : 'secondary'}
        onClick={() => (isBookmarked ? remove.mutate(lexemeId) : add.mutate(lexemeId))}
        disabled={add.isPending || remove.isPending}
        aria-pressed={isBookmarked}
        className="px-3 py-1.5 text-xs"
      >
        {isBookmarked ? <BookmarkCheck size={14} aria-hidden /> : <Bookmark size={14} aria-hidden />}
        {isBookmarked ? 'Bookmarked' : 'Bookmark'}
      </Button>
    )
  }

  // The reader's path: resolve, then bookmark — or say the word is not in the guide.
  if (search?.isLoading) {
    return (
      <Button variant="secondary" disabled className="px-3 py-1.5 text-xs">
        <Bookmark size={14} aria-hidden /> Looking it up…
      </Button>
    )
  }
  if (!resolved) {
    return (
      <span className="text-xs text-ink-3" title="This word is not in the curriculum's vocabulary list.">
        Not in the study guide's vocabulary
      </span>
    )
  }
  return (
    <Button
      variant="secondary"
      onClick={() => add.mutate(resolved)}
      disabled={add.isPending}
      className="px-3 py-1.5 text-xs"
    >
      <Bookmark size={14} aria-hidden /> Bookmark
    </Button>
  )
}
