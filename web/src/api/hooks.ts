import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, isUnauthorized } from './client'
import type {
  AttemptInput,
  Curriculum,
  LessonDetail,
  PathResponse,
  ProgressOverview,
  ReviewAnswerResponse,
  ReviewQueue,
  SessionInfo,
  ToneAnswersResponse,
  ToneQueue,
  SubmitSummary,
  User,
} from './types'

export const queryKeys = {
  session: ['session'] as const,
  overview: ['progress', 'overview'] as const,
  path: (version: string) => ['curriculum', version, 'path'] as const,
  lesson: (id: string) => ['lesson', id] as const,
  reviewQueue: (limit: number) => ['reviews', 'queue', limit] as const,
  toneQueue: (level: number | null) => ['tones', 'queue', level ?? 'mine'] as const,
  curricula: ['curricula'] as const,
}

/** The boot query: who am I, and is there a live session? */
export function useSession() {
  return useQuery({
    queryKey: queryKeys.session,
    queryFn: () => api<SessionInfo>('/api/v1/auth/session'),
    staleTime: 30_000,
    retry: false,
  })
}

export function useCurricula() {
  return useQuery({
    queryKey: queryKeys.curricula,
    queryFn: () => api<{ curricula: Curriculum[] }>('/api/v1/curriculum'),
    staleTime: 5 * 60_000,
  })
}

export function useOverview(enabled = true) {
  return useQuery({
    queryKey: queryKeys.overview,
    queryFn: () => api<ProgressOverview>('/api/v1/progress/overview'),
    enabled,
    retry: (count, error) => !isUnauthorized(error) && count < 2,
  })
}

export function usePath(version: string, enabled = true) {
  return useQuery({
    queryKey: queryKeys.path(version),
    queryFn: () => api<PathResponse>(`/api/v1/curriculum/${version}/path`),
    enabled,
  })
}

export function useLesson(id: string | undefined) {
  return useQuery({
    queryKey: queryKeys.lesson(id ?? ''),
    queryFn: () => api<LessonDetail>(`/api/v1/lessons/${id}`),
    enabled: Boolean(id),
  })
}

export function useReviewQueue(limit = 20) {
  return useQuery({
    queryKey: queryKeys.reviewQueue(limit),
    queryFn: () => api<ReviewQueue>(`/api/v1/reviews/queue?limit=${limit}`),
  })
}

/**
 * Tone drills for a level. `null` lets the server pick the learner's own level, which
 * is what a page should do rather than guessing at a default of its own — the server
 * already knows whether they have said which level they are working on.
 */
export function useToneQueue(level: number | null) {
  return useQuery({
    queryKey: queryKeys.toneQueue(level),
    queryFn: () =>
      api<ToneQueue>(`/api/v1/tones/queue${level === null ? '' : `?level=${level}`}`),
  })
}

/**
 * Grades one tone answer.
 *
 * Deliberately does not invalidate the queue: a round is snapshotted when it starts, so
 * a background refetch cannot reshuffle the questions mid-round, and the page already
 * holds the lifetime stats each answer returns. Refreshing is the page's call, when it
 * begins a new round.
 */
export function useAnswerTone() {
  return useMutation({
    mutationFn: (input: { exercise_id: string; client_ref: string; option_id: string }) =>
      api<ToneAnswersResponse>('/api/v1/tones/answers', { method: 'POST', body: { answers: [input] } }),
  })
}

export function useLogin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { email: string; password: string }) =>
      api<{ user: User; csrf_token: string }>('/api/v1/auth/login', { method: 'POST', body: input }),
    onSuccess: () => qc.invalidateQueries(),
  })
}

export function useRegister() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: {
      email: string
      display_name: string
      password: string
      timezone: string
      daily_goal_minutes?: number
    }) =>
      api<{ user: User; csrf_token: string; message?: string }>('/api/v1/auth/register', {
        method: 'POST',
        body: input,
      }),
    onSuccess: () => qc.invalidateQueries(),
  })
}

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<void>('/api/v1/auth/logout', { method: 'POST' }),
    onSuccess: () => qc.invalidateQueries(),
  })
}

export function useUpdateMe() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: Partial<{
      display_name: string
      timezone: string
      daily_goal_kind: string
      daily_goal_value: number
      pinyin_default: string
      target_curriculum: string
      target_level: number
    }>) => api<User>('/api/v1/me', { method: 'PATCH', body: input }),
    onSuccess: (user) => {
      qc.setQueryData(queryKeys.session, { authenticated: true, user })
      qc.invalidateQueries({ queryKey: queryKeys.overview })
    },
  })
}

/** Recompute every derived value from the attempt log and return the fresh view. */
export function useRefreshProgress() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<ProgressOverview>('/api/v1/progress/refresh', { method: 'POST' }),
    onSuccess: (overview) => {
      qc.setQueryData(queryKeys.overview, overview)
      qc.invalidateQueries({ queryKey: ['curriculum'] })
    },
  })
}

export function useSubmitAttempts(lessonId: string, mode: 'practice' | 'exam' | 'review' | 'placement') {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (attempts: AttemptInput[]) =>
      api<SubmitSummary>(`/api/v1/lessons/${lessonId}/attempts`, {
        method: 'POST',
        body: { mode, attempts },
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.overview })
      qc.invalidateQueries({ queryKey: ['curriculum'] })
      qc.invalidateQueries({ queryKey: ['reviews'] })
    },
  })
}

export function useAnswerReview() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { lexeme_id: string; correct: boolean }) =>
      api<ReviewAnswerResponse>('/api/v1/reviews/answer', { method: 'POST', body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['reviews'] })
      qc.invalidateQueries({ queryKey: queryKeys.overview })
    },
  })
}

export function usePlacementQuestions(version: string, level = 1, limit = 20) {
  return useQuery({
    queryKey: ['placement', version, level, limit],
    queryFn: () =>
      api<{ version: string; level: number; items: { id: string; simplified: string; pinyin: string; senses: { en?: string }[] }[]; self_report: string }>(
        `/api/v1/placement/questions?version=${version}&level=${level}&limit=${limit}`,
      ),
  })
}

export function useSubmitPlacement() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { version: string; known_lexeme_ids: string[]; presented: number }) =>
      api<{ user: User; score: number; recommended: number; note: string }>('/api/v1/me/placement', {
        method: 'POST',
        body: input,
      }),
    onSuccess: (res) => {
      qc.setQueryData(queryKeys.session, { authenticated: true, user: res.user })
      qc.invalidateQueries()
    },
  })
}
