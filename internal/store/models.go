package store

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// User mirrors the users table.
type User struct {
	ID                   uuid.UUID  `json:"id"`
	Email                string     `json:"email"`
	DisplayName          string     `json:"display_name"`
	PasswordHash         string     `json:"-"`
	Role                 string     `json:"role"`
	EmailVerifiedAt      *time.Time `json:"email_verified_at"`
	Timezone             string     `json:"timezone"`
	DailyGoalValue       int        `json:"daily_goal_value"`
	DailyGoalKind        string     `json:"daily_goal_kind"`
	PinyinDefault        string     `json:"pinyin_default"`
	TargetCurriculum     *string    `json:"target_curriculum"`
	TargetLevel          *int       `json:"target_level"`
	PlacementCompletedAt *time.Time `json:"placement_completed_at"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// Session is a server-side session record. The token itself is never stored.
type Session struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	CSRFToken  string     `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// Curriculum is one versioned HSK syllabus.
type Curriculum struct {
	ID            uuid.UUID `json:"id"`
	Version       string    `json:"version"`
	Name          string    `json:"name"`
	Standard      string    `json:"standard"`
	StageModel    string    `json:"stage_model"`
	Status        string    `json:"status"`
	EffectiveFrom *string   `json:"effective_from"`
	Notes         string    `json:"notes,omitempty"`
	LevelCount    int       `json:"level_count"`
	UnitCount     int       `json:"unit_count"`
	LessonCount   int       `json:"lesson_count"`
	ExerciseCount int       `json:"exercise_count"`
}

// Level is a level within a curriculum.
type Level struct {
	ID          uuid.UUID `json:"id"`
	LevelNumber int       `json:"level_number"`
	Stage       *string   `json:"stage"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	VocabTarget int       `json:"vocab_target,omitempty"`
	UnitCount   int       `json:"unit_count"`
	LessonCount int       `json:"lesson_count"`
}

// LessonRef is a lesson's position in the linear path, used for lock state.
type LessonRef struct {
	LessonID     uuid.UUID
	UnitID       uuid.UUID
	UnitNumber   int
	LessonNumber int
	Kind         string
	Title        string
	// LevelNumber is carried so a caller can tell whether this lesson's level has been
	// opened by a proficiency test (see migrations/0007).
	LevelNumber int
}

// PathLesson is a lesson as returned to the learner, with derived state.
type PathLesson struct {
	ID               uuid.UUID `json:"id"`
	LessonNumber     int       `json:"lesson_number"`
	Title            string    `json:"title"`
	Kind             string    `json:"kind"`
	Goal             string    `json:"goal,omitempty"`
	XPReward         int       `json:"xp_reward"`
	EstimatedMinutes int       `json:"estimated_minutes"`
	ExerciseCount    int       `json:"exercise_count"`
	// State is one of locked | available | in_progress | completed.
	State       string     `json:"state"`
	BestScore   *float64   `json:"best_score"`
	Attempts    int        `json:"attempts"`
	CompletedAt *time.Time `json:"completed_at"`
}

// PathUnit groups lessons inside a level.
type PathUnit struct {
	ID          uuid.UUID    `json:"id"`
	UnitNumber  int          `json:"unit_number"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Lessons     []PathLesson `json:"lessons"`
}

// PathLevel is a level with its units, scoped to one learner.
type PathLevel struct {
	ID          uuid.UUID  `json:"id"`
	LevelNumber int        `json:"level_number"`
	Stage       *string    `json:"stage"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Units       []PathUnit `json:"units"`
	Completed   int        `json:"completed_lessons"`
	Total       int        `json:"total_lessons"`
	Unlocked    bool       `json:"unlocked"`
}

// Lexeme is a vocabulary item.
type Lexeme struct {
	ID          uuid.UUID       `json:"id"`
	Simplified  string          `json:"simplified"`
	Traditional string          `json:"traditional"`
	Pinyin      string          `json:"pinyin"`
	POS         []string        `json:"parts_of_speech"`
	Senses      json.RawMessage `json:"senses"`
	AudioKey    *string         `json:"audio_key,omitempty"`
	HSKLevel    *int            `json:"hsk_level,omitempty"`
}

// ExerciseForLearner is an exercise as sent to the client: the answer key is
// structurally absent from this type, so it cannot be leaked by accident.
type ExerciseForLearner struct {
	ID         uuid.UUID       `json:"id"`
	Number     int             `json:"number"`
	Type       string          `json:"type"`
	Skill      string          `json:"skill"`
	Difficulty int             `json:"difficulty"`
	Prompt     json.RawMessage `json:"prompt"`
}

// ExerciseForGrading carries the answer key and never leaves the server.
type ExerciseForGrading struct {
	ID          uuid.UUID
	LessonID    uuid.UUID
	Type        string
	Skill       string
	Prompt      json.RawMessage
	AnswerKey   json.RawMessage
	Explanation *string
}

// Passage is a reading or listening text.
type Passage struct {
	ID          uuid.UUID       `json:"id"`
	Kind        string          `json:"kind"`
	Title       string          `json:"title"`
	Instruction string          `json:"instruction,omitempty"`
	Body        string          `json:"body"`
	Sentences   json.RawMessage `json:"sentences"`
	AudioKey    *string         `json:"audio_key,omitempty"`
	WordCount   int             `json:"word_count"`
	Minutes     int             `json:"minutes"`
}

// LessonDetail is the full lesson payload for the player.
type LessonDetail struct {
	ID                uuid.UUID            `json:"id"`
	Title             string               `json:"title"`
	Kind              string               `json:"kind"`
	Goal              string               `json:"goal,omitempty"`
	XPReward          int                  `json:"xp_reward"`
	EstimatedMinutes  int                  `json:"estimated_minutes"`
	UnitTitle         string               `json:"unit_title"`
	LevelNumber       int                  `json:"level_number"`
	CurriculumVersion string               `json:"curriculum_version"`
	Exercises         []ExerciseForLearner `json:"exercises"`
	Passages          []Passage            `json:"passages"`
	Vocabulary        []Lexeme             `json:"vocabulary"`
	State             string               `json:"state"`
}

// AttemptInput is one learner answer to be graded and recorded.
type AttemptInput struct {
	ExerciseID uuid.UUID       `json:"exercise_id"`
	ClientRef  string          `json:"client_ref"`
	Response   json.RawMessage `json:"response"`
	DurationMs int             `json:"duration_ms"`
}

// AttemptRow is a stored attempt.
type AttemptRow struct {
	ID         uuid.UUID
	ExerciseID uuid.UUID
	Mode       string
	ClientRef  *string
	Response   json.RawMessage
	IsCorrect  *bool
	Score      *float64
	DurationMs *int
	Grader     string
	Feedback   json.RawMessage
	CreatedAt  time.Time
}

// ReviewItem is a due vocabulary card.
type ReviewItem struct {
	ID            uuid.UUID       `json:"id"`
	LexemeID      uuid.UUID       `json:"lexeme_id"`
	Simplified    string          `json:"simplified"`
	Traditional   string          `json:"traditional"`
	Pinyin        string          `json:"pinyin"`
	Senses        json.RawMessage `json:"senses"`
	AudioKey      *string         `json:"audio_key,omitempty"`
	IntervalIndex int             `json:"interval_index"`
	Ease          float64         `json:"ease"`
	Reps          int             `json:"reps"`
	Lapses        int             `json:"lapses"`
	DueAt         time.Time       `json:"due_at"`
}

// DayActivity is one day of rolled-up activity in the learner's timezone.
type DayActivity struct {
	Day      time.Time `json:"day"`
	XP       int       `json:"xp"`
	Minutes  int       `json:"minutes"`
	Attempts int       `json:"attempts"`
	Correct  int       `json:"correct"`
	Lessons  int       `json:"lessons_completed"`
	GoalMet  bool      `json:"goal_met"`
}

// StreakState is the persisted streak summary.
type StreakState struct {
	CurrentStreak int        `json:"current_streak"`
	LongestStreak int        `json:"longest_streak"`
	LastActiveDay *time.Time `json:"last_active_day"`
	Freezes       int        `json:"freezes"`
	// GoalMetToday is derived at read time from daily_activity.
	GoalMetToday bool   `json:"goal_met_today"`
	GoalValue    int    `json:"goal_value"`
	GoalKind     string `json:"goal_kind"`
	TodayXP      int    `json:"today_xp"`
	TodayMinutes int    `json:"today_minutes"`
}

// SkillMastery is a per-skill rollup derived from attempts.
type SkillMastery struct {
	Skill         string     `json:"skill"`
	Attempts      int        `json:"attempts"`
	Correct       int        `json:"correct"`
	Mastery       float64    `json:"mastery"`
	LastAttemptAt *time.Time `json:"last_attempt_at"`
}

// ProgressOverview is the dashboard payload.
type ProgressOverview struct {
	Streak            StreakState    `json:"streak"`
	Skills            []SkillMastery `json:"skills"`
	TotalAttempts     int            `json:"total_attempts"`
	TotalCorrect      int            `json:"total_correct"`
	LessonsDone       int            `json:"lessons_completed"`
	TotalXP           int            `json:"total_xp"`
	DueReviews        int            `json:"due_reviews"`
	Activity          []DayActivity  `json:"activity"`
	CurriculumVersion string         `json:"curriculum_version,omitempty"`
	NextLessonID      *uuid.UUID     `json:"next_lesson_id,omitempty"`
	NextLessonTitle   string         `json:"next_lesson_title,omitempty"`
}

// AuthToken is a single-use email verification / password reset token.
type AuthToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Kind      string
	ExpiresAt time.Time
	UsedAt    *time.Time
}
