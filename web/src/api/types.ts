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
  | 'tone_mcq'
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

// --- study surfaces -------------------------------------------------------
// The writing studio, the speaking studio and the study guide. Like tone practice,
// they serve content from any level — ungated by the path — and their attempts are
// recorded with mode "studio", excluded from every derived value (migrations/0008).

export type StudyDrill = Exercise & { level: number }

export type StudyLexeme = Lexeme & { level: number; bookmarked: boolean }

export type ReadingPassage = Passage & { level: number }

export type ReadingPassagesResponse = { items: ReadingPassage[]; level: number }

export type GrammarNote = {
  id: string
  level: number
  unit_title: string
  lesson_title: string
  explanation: string
  example: string
  example_pinyin: string
  example_type: string
}

export type StudioQueue = {
  items: StudyDrill[]
  level: number
  stats: ToneLevelStat[]
}

export type StudioAnswerResponse = {
  results: AttemptResult[]
  stats: ToneLevelStat[]
}

export type VocabularyPage = {
  items: StudyLexeme[]
  total: number
  level: number
  limit: number
  offset: number
}

export type BookmarkMutationResponse = {
  lexeme_id: string
  bookmarked: boolean
  created?: boolean
  removed?: boolean
}

// --- proficiency tests -----------------------------------------------------
// A scored paper that can open a level. Placement recommends and says so; this decides.
// Passing records an unlock, never a completion: the level's lessons become available,
// and nothing is marked learned.

export type ProficiencyCard = {
  level: number
  title: string
  /** Gradable questions this level can draw a paper from. */
  questions: number
  /** A proficiency test opened this level. Walking to a level creates no row of its own. */
  unlocked_by_test: boolean
  best_score: number | null
  papers_taken: number
  passed: boolean
}

export type ProficiencyCardsResponse = {
  cards: ProficiencyCard[]
  pass_threshold: number
  paper_size: number
}

export type ProficiencyPaperResponse = {
  paper_id: string
  level: { number: number; title: string }
  questions: Exercise[]
  pass_threshold: number
}

export type ProficiencyPaperResult = {
  id: string
  level: number
  score: number
  percent: number
  passed: boolean
  graded_count: number
  correct_count: number
  pass_mark: number
}

export type ProficiencySubmitResponse = {
  results: AttemptResult[]
  paper: ProficiencyPaperResult
  unlocked_now: boolean
}

// --- tone practice ---------------------------------------------------------
// Drills on tone alone, from any level: not gated by the lesson path, and graded
// without touching it (see migrations/0006). The prompt never carries an answer key,
// and a written tone question carries no pinyin — there the pinyin IS the answer.

export type ToneDrill = Exercise & { level: number }

export type ToneLevelStat = {
  level: number
  /** How many tone drills exist at this level. */
  drills: number
  /** Tone-mode attempts by this learner. */
  answered: number
  correct: number
}

export type ToneTallyRow = {
  tone: string
  label: string
  asked: number
  missed: number
}

export type ToneQueue = {
  items: ToneDrill[]
  /** The level the drills were drawn from; 0 means every level. */
  level: number
  stats: ToneLevelStat[]
}

export type ToneAnswersResponse = {
  results: AttemptResult[]
  tone_tally: ToneTallyRow[]
  stats: ToneLevelStat[]
}

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

export type ReviewForecast = { now: number; day: number; days3: number; week: number; later: number }

export type ReviewQueue = { items: ReviewItem[]; due_total: number; forecast?: ReviewForecast }

export type ReviewAnswerResponse = {
  lexeme_id: string
  interval_index: number
  ease: number
  due_at: string
  due_total: number
}
