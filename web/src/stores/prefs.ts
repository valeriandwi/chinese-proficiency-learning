import { create } from 'zustand'
import { persist } from 'zustand/middleware'

// Zustand holds only client-owned UI state. Server state lives in TanStack Query,
// so nothing here duplicates a response body.

type PrefsState = {
  /** Pinyin is shown by default for a beginner and can be toggled per session. */
  pinyinVisible: boolean
  /** Exam mode suppresses per-answer feedback until the paper is submitted. */
  examMode: boolean
  setPinyinVisible: (visible: boolean) => void
  togglePinyin: () => void
  setExamMode: (exam: boolean) => void
}

export const usePrefs = create<PrefsState>()(
  persist(
    (set) => ({
      pinyinVisible: true,
      examMode: false,
      setPinyinVisible: (pinyinVisible) => set({ pinyinVisible }),
      togglePinyin: () => set((s) => ({ pinyinVisible: !s.pinyinVisible })),
      setExamMode: (examMode) => set({ examMode }),
    }),
    { name: 'hanpath-prefs' },
  ),
)

type LessonDraft = {
  lessonId: string | null
  /** Answers keyed by exercise id, and the client_ref issued for each submit. */
  answers: Record<string, Record<string, unknown>>
  clientRefs: Record<string, string>
  index: number
  startedAt: number | null
  begin: (lessonId: string, exerciseIds: string[]) => void
  answer: (exerciseId: string, payload: Record<string, unknown>) => void
  goto: (index: number) => void
  reset: () => void
}

export const useLessonDraft = create<LessonDraft>((set) => ({
  lessonId: null,
  answers: {},
  clientRefs: {},
  index: 0,
  startedAt: null,
  begin: (lessonId, exerciseIds) =>
    set((s) => {
      // Starting a different lesson discards the previous draft; re-entering the
      // same lesson keeps it, so a refresh does not lose progress.
      if (s.lessonId === lessonId && Object.keys(s.clientRefs).length > 0) return { index: 0 }
      const clientRefs: Record<string, string> = {}
      for (const id of exerciseIds) {
        clientRefs[id] = `${lessonId.slice(0, 8)}-${id.slice(0, 8)}-${Math.random().toString(36).slice(2, 8)}`
      }
      return { lessonId, answers: {}, clientRefs, index: 0, startedAt: Date.now() }
    }),
  answer: (exerciseId, payload) =>
    set((s) => ({ answers: { ...s.answers, [exerciseId]: payload } })),
  goto: (index) => set({ index }),
  reset: () => set({ lessonId: null, answers: {}, clientRefs: {}, index: 0, startedAt: null }),
}))
