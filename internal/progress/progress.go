// Package progress holds the derived-value maths that must not live in SQL:
// streak computation over local calendar days and the spaced-repetition ladder.
// Both are pure functions with unit tests, because both are easy to get subtly
// wrong in ways that only show up as "my streak reset for no reason".
package progress

import (
	"math"
	"sort"
	"time"
)

// LessonPassThreshold is the best-score an exercise needs for the lesson to count
// as passed. It is exported so the SQL recompute and the API agree on one number.
const LessonPassThreshold = 0.7

// Day is one day of rolled-up activity in the learner's timezone.
type Day struct {
	Date    time.Time
	GoalMet bool
}

// Streak is the streak summary derived from activity days.
type Streak struct {
	Current    int
	Longest    int
	LastActive *time.Time
}

// ComputeStreak walks days (any order) and returns the current and longest streak.
//
// Rules, stated explicitly because they are the kind of thing users notice:
//   - Only days where the daily goal was met extend a streak.
//   - Days must be *calendar-adjacent*: a gap of a full missed day breaks the run,
//     even if week-ends are missing from the input entirely.
//   - The current streak survives an unfinished *today*: a learner who last met
//     their goal yesterday is still on a streak until today ends.
func ComputeStreak(days []Day, today time.Time) Streak {
	today = dateOnly(today)

	met := make(map[time.Time]bool, len(days))
	for _, d := range days {
		day := dateOnly(d.Date)
		if day.After(today) {
			continue // defensive: a future day must never extend a streak
		}
		if d.GoalMet {
			met[day] = true
		}
	}
	if len(met) == 0 {
		return Streak{}
	}

	sorted := make([]time.Time, 0, len(met))
	for d := range met {
		sorted = append(sorted, d)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })

	out := Streak{}
	// Longest: the longest run of consecutive calendar days.
	run := 1
	out.Longest = 1
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Sub(sorted[i-1]) == 24*time.Hour {
			run++
		} else {
			run = 1
		}
		if run > out.Longest {
			out.Longest = run
		}
	}
	last := sorted[len(sorted)-1]
	out.LastActive = &last

	// Current: walk backwards day by day from today (or from yesterday, if today
	// is not finished yet) and stop at the first day the goal was not met.
	start := today
	if !met[today] {
		start = today.AddDate(0, 0, -1)
		if !met[start] {
			return out
		}
	}
	for d := start; met[d]; d = d.AddDate(0, 0, -1) {
		out.Current++
	}
	return out
}

// IntervalLadder is the spaced-repetition interval ladder, indexed by
// interval_index. index 0 means "just learned / relearning".
var IntervalLadder = []time.Duration{
	10 * time.Minute, // 0 learning
	24 * time.Hour,   // 1
	3 * 24 * time.Hour,
	7 * 24 * time.Hour,
	14 * 24 * time.Hour,
	30 * 24 * time.Hour,
	90 * 24 * time.Hour,
}

const (
	easeMin = 1.3
	easeMax = 3.0
)

// ScheduleReview applies the interval ladder to one review answer.
// A wrong answer steps back one rung and is retried in ten minutes in the same
// session; a correct answer steps forward one rung.
func ScheduleReview(now time.Time, intervalIndex int, ease float64, correct bool) (nextIndex int, nextEase float64, due time.Time) {
	if intervalIndex < 0 {
		intervalIndex = 0
	}
	if intervalIndex >= len(IntervalLadder) {
		intervalIndex = len(IntervalLadder) - 1
	}
	if ease <= 0 {
		ease = 2.5
	}
	if correct {
		if intervalIndex < len(IntervalLadder)-1 {
			intervalIndex++
		}
		ease = math.Min(easeMax, ease+0.05)
	} else {
		if intervalIndex > 0 {
			intervalIndex--
		}
		ease = math.Max(easeMin, ease-0.20)
	}
	interval := IntervalLadder[intervalIndex]
	if !correct {
		interval = IntervalLadder[0]
	}
	return intervalIndex, round2(ease), now.Add(interval)
}

// DeriveLessonStates assigns locked | available | in_progress | completed to a
// lesson path in order. The path is linear across the whole level, so exactly one
// unfinished lesson is available at a time and everything after it is locked.
//
// Inputs are parallel slices in path order. It returns states keyed by index so
// callers with their own id types do not need to import uuid.
func DeriveLessonStates(orderedCompleted []bool, orderedStarted []bool) []string {
	states := make([]string, len(orderedCompleted))
	unlocked := true
	for i := range orderedCompleted {
		switch {
		case orderedCompleted[i]:
			states[i] = "completed"
			// Completing a lesson unlocks the next one.
			unlocked = true
		case orderedStarted[i]:
			states[i] = "in_progress"
			// A lesson already begun stays available even if it is not first.
			unlocked = true
		case unlocked:
			states[i] = "available"
			unlocked = false
		default:
			states[i] = "locked"
		}
	}
	return states
}

// PlacementLevel recommends a starting level from a placement score in [0,1],
// mapped against the level's vocabulary expectation.
func PlacementLevel(score float64, levelCount int) int {
	if levelCount < 1 {
		levelCount = 1
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	level := int(math.Floor(score*float64(levelCount))) + 1
	if level > levelCount {
		level = levelCount
	}
	return level
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
