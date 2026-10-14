// Wire types for /api/v1.
//
// Two layers, on purpose:
//
//   * `src/api/schema.ts` is GENERATED from openapi/openapi.yaml by
//     `make openapi-types` (openapi-typescript). It is the contract, verbatim.
//   * The hand-written types below are the shapes the SPA actually consumes:
//     narrower, friendlier names, and discriminated unions the components can
//     switch on. CI fails if either side drifts out of step with the spec, because
//     a change to openapi.yaml that breaks a component breaks `tsc -b`.
//
// Regenerate after editing the spec:  make openapi-types

export type User = {
  id: string
  email: string
  display_name: string
  role: 'learner' | 'admin'
  email_verified_at: string | null
  timezone: string
  daily_goal_value: number
  daily_goal_kind: GoalKind
  pinyin_default: 'always' | 'on_tap' | 'never'
  target_curriculum: string | null
  target_level: number | null
  placement_completed_at: string | null
  created_at: string
  updated_at: string
}

export type GoalKind = 'minutes' | 'xp' | 'lessons'

export type SessionInfo =
  | { authenticated: false }
  | { authenticated: true; user: User; csrf_token?: string }

export type LessonState = 'locked' | 'available' | 'in_progress' | 'completed'

export type Lexeme = {
  id: string
  simplified: string
  traditional: string
  pinyin: string
  parts_of_speech: string[]
  senses: { en?: string; [k: string]: string | undefined }[]
  audio_key?: string
  hsk_level?: number
}

export type LessonSummary = {
  id: string
  lesson_number: number
  title: string
  kind: 'lesson' | 'review' | 'assessment' | 'mock_exam'
  goal?: string
  xp_reward: number
  estimated_minutes: number
  exercise_count: number
  state: LessonState
  best_score: number | null
  attempts: number
  completed_at: string | null
}

export type PathUnit = {
  id: string
  unit_number: number
  title: string
  description?: string
  lessons: LessonSummary[]
}

export type PathLevel = {
  id: string
  level_number: number
  stage: string | null
  title: string
  description?: string
  units: PathUnit[]
  completed_lessons: number
  total_lessons: number
}

export type PathResponse = { version: string; levels: PathLevel[] }

export type Curriculum = {
  id: string
  version: string
  name: string
  standard: string
  stage_model: 'six-levels' | 'three-stages-nine-levels'
  status: 'draft' | 'published' | 'archived'
  effective_from: string | null
  notes?: string
  level_count: number
  unit_count: number
  lesson_count: number
  exercise_count: number
  levels: {
    id: string
    level_number: number
    stage: string | null
    title: string
    description?: string
    vocab_target?: number
    unit_count: number
    lesson_count: number
  }[]
}

export type ExerciseType =
  | 'mcq'
  | 'mcq_multi'
  | 'true_false'
  | 'fill_blank'
  | 'sentence_order'
  | 'translation'
  | 'hanzi_trace'
  | 'hanzi_recall'
  | 'short_answer'
  // Listening (mechanically graded) and speaking (never graded for pronunciation).
  | 'listening_mcq'
  | 'listening_dictation'
  | 'speak_repeat'
  | 'read_aloud'
  | 'speak_answer'

export type Skill = 'vocabulary' | 'grammar' | 'reading' | 'listening' | 'writing' | 'speaking'

export type Exercise = {
  id: string
  number: number
  type: ExerciseType
  skill: Skill
  difficulty: number
  prompt: Record<string, unknown>
}

export type Passage = {
  id: string
  kind: 'reading' | 'listening'
  title: string
  instruction?: string
  body: string
  sentences: { zh: string; pinyin?: string; en?: string; tokens?: { zh: string; pinyin?: string; en?: string }[] }[]
  audio_key?: string
  word_count: number
  minutes: number
}

export type LessonDetail = {
  id: string
  title: string
  kind: 'lesson' | 'review' | 'assessment' | 'mock_exam'
  goal?: string
  xp_reward: number
  estimated_minutes: number
  unit_title: string
  level_number: number
  curriculum_version: string
  exercises: Exercise[]
  passages: Passage[]
  vocabulary: Lexeme[]
  state: LessonState
}

export type AnswerPayload = Record<string, unknown>

export type AttemptInput = {
  exercise_id: string
  client_ref: string
  response: AnswerPayload
  duration_ms: number
}

export type AttemptResult = {
  exercise_id: string
  client_ref: string
  is_correct: boolean | null
  score: number
  grader: string
  feedback: Record<string, unknown> | null
  solution?: AnswerPayload
  explanation?: string
  duplicate: boolean
  needs_review: boolean
}

export type LessonOutcome = {
  lesson_id: string
  status: 'in_progress' | 'completed'
  completed_now: boolean
  xp_awarded: number
  correct: number
  total: number
}

export type SubmitSummary = {
  results: AttemptResult[]
  lessons: LessonOutcome[]
}

export type SkillMastery = {
  skill: Skill
  attempts: number
  correct: number
  mastery: number
  last_attempt_at: string | null
}

export type DayActivity = {
  day: string
  xp: number
  minutes: number
  attempts: number
  correct: number
  lessons_completed: number
  goal_met: boolean
}

export type ProgressOverview = {
  streak: {
    current_streak: number
    longest_streak: number
    last_active_day: string | null
    freezes: number
    goal_met_today: boolean
    goal_value: number
    goal_kind: GoalKind
    today_xp: number
    today_minutes: number
  }
  skills: SkillMastery[]
  total_attempts: number
  total_correct: number
  lessons_completed: number
  total_xp: number
  due_reviews: number
  activity: DayActivity[]
  curriculum_version?: string
  next_lesson_id?: string
  next_lesson_title?: string
}

export type ReviewItem = {
  id: string
  lexeme_id: string
  simplified: string
  traditional: string
  pinyin: string
  senses: { en?: string }[]
  audio_key?: string
  interval_index: number
  ease: number
  reps: number
  lapses: number
  due_at: string
}

export type ReviewQueue = { items: ReviewItem[]; due_total: number }

export type ReviewAnswerResponse = {
  lexeme_id: string
  interval_index: number
  ease: number
  due_at: string
  due_total: number
}
