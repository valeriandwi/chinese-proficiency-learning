package progress

import (
	"testing"
	"time"
)

// Streak arithmetic is the classic place a learning app quietly lies to its user:
// a day boundary computed in the wrong timezone or a grace rule applied one day
// too late resets a streak nobody expected to lose. These cases pin the rules.

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestComputeStreak(t *testing.T) {
	today := day("2026-10-12")

	cases := []struct {
		name        string
		days        []Day
		today       time.Time
		wantCurrent int
		wantLongest int
	}{
		{
			name:        "no activity at all",
			days:        nil,
			wantCurrent: 0, wantLongest: 0,
		},
		{
			name:        "today only",
			days:        []Day{{Date: today, GoalMet: true}},
			wantCurrent: 1, wantLongest: 1,
		},
		{
			name: "yesterday counts until today ends (grace rule)",
			days: []Day{
				{Date: today.AddDate(0, 0, -1), GoalMet: true},
				{Date: today.AddDate(0, 0, -2), GoalMet: true},
				{Date: today.AddDate(0, 0, -3), GoalMet: true},
			},
			wantCurrent: 3, wantLongest: 3,
		},
		{
			name: "a full missed day breaks it",
			days: []Day{
				{Date: today, GoalMet: true},
				{Date: today.AddDate(0, 0, -2), GoalMet: true},
				{Date: today.AddDate(0, 0, -3), GoalMet: true},
			},
			wantCurrent: 1, wantLongest: 2,
		},
		{
			name: "activity without meeting the goal does not count",
			days: []Day{
				{Date: today, GoalMet: true},
				{Date: today.AddDate(0, 0, -1), GoalMet: false},
				{Date: today.AddDate(0, 0, -2), GoalMet: true},
			},
			wantCurrent: 1, wantLongest: 1,
		},
		{
			name: "an older run is remembered as the longest",
			days: []Day{
				{Date: today, GoalMet: true},
				{Date: today.AddDate(0, 0, -5), GoalMet: true},
				{Date: today.AddDate(0, 0, -6), GoalMet: true},
				{Date: today.AddDate(0, 0, -7), GoalMet: true},
				{Date: today.AddDate(0, 0, -8), GoalMet: true},
			},
			wantCurrent: 1, wantLongest: 4,
		},
		{
			name: "a run that ended long ago is not the current streak",
			days: []Day{
				{Date: today.AddDate(0, 0, -10), GoalMet: true},
				{Date: today.AddDate(0, 0, -11), GoalMet: true},
			},
			wantCurrent: 0, wantLongest: 2,
		},
		{
			name: "out-of-order input does not matter",
			days: []Day{
				{Date: today.AddDate(0, 0, -2), GoalMet: true},
				{Date: today, GoalMet: true},
				{Date: today.AddDate(0, 0, -1), GoalMet: true},
			},
			wantCurrent: 3, wantLongest: 3,
		},
		{
			name: "a future day never extends a streak",
			days: []Day{
				{Date: today.AddDate(0, 0, 1), GoalMet: true},
				{Date: today, GoalMet: true},
			},
			wantCurrent: 1, wantLongest: 1,
		},
		{
			name: "a month boundary is just another day",
			days: []Day{
				{Date: day("2026-03-01"), GoalMet: true},
				{Date: day("2026-02-28"), GoalMet: true},
				{Date: day("2026-02-27"), GoalMet: true},
			},
			today:       day("2026-03-01"),
			wantCurrent: 3, wantLongest: 3,
		},
		{
			name: "a leap day does not break a run",
			days: []Day{
				{Date: day("2028-03-01"), GoalMet: true},
				{Date: day("2028-02-29"), GoalMet: true},
				{Date: day("2028-02-28"), GoalMet: true},
			},
			today:       day("2028-03-01"),
			wantCurrent: 3, wantLongest: 3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := tc.today
			if at.IsZero() {
				at = today
			}
			got := ComputeStreak(tc.days, at)
			if got.Current != tc.wantCurrent || got.Longest != tc.wantLongest {
				t.Errorf("ComputeStreak = (current %d, longest %d), want (%d, %d)",
					got.Current, got.Longest, tc.wantCurrent, tc.wantLongest)
			}
		})
	}
}

func TestScheduleReview(t *testing.T) {
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)

	t.Run("correct answers climb the ladder one rung at a time", func(t *testing.T) {
		index, ease, due := ScheduleReview(now, 0, 2.5, true)
		if index != 1 {
			t.Errorf("interval index = %d, want 1", index)
		}
		if want := now.Add(24 * time.Hour); !due.Equal(want) {
			t.Errorf("due = %s, want %s", due, want)
		}
		if ease <= 2.5 {
			t.Errorf("ease should rise after a correct answer, got %.2f", ease)
		}
	})

	t.Run("the ladder stops at its last rung", func(t *testing.T) {
		index, _, due := ScheduleReview(now, len(IntervalLadder)-1, 2.5, true)
		if index != len(IntervalLadder)-1 {
			t.Errorf("interval index = %d, want %d", index, len(IntervalLadder)-1)
		}
		if want := now.Add(IntervalLadder[len(IntervalLadder)-1]); !due.Equal(want) {
			t.Errorf("due = %s, want %s", due, want)
		}
	})

	t.Run("a wrong answer steps back and retries in the same session", func(t *testing.T) {
		index, ease, due := ScheduleReview(now, 3, 2.5, false)
		if index != 2 {
			t.Errorf("interval index = %d, want 2", index)
		}
		if want := now.Add(IntervalLadder[0]); !due.Equal(want) {
			t.Errorf("due = %s, want %s (a wrong card returns within the session)", due, want)
		}
		if ease >= 2.5 {
			t.Errorf("ease should fall after a wrong answer, got %.2f", ease)
		}
	})

	t.Run("ease is clamped at both ends", func(t *testing.T) {
		low := 1.35
		for i := 0; i < 20; i++ {
			_, low, _ = ScheduleReview(now, 0, low, false)
		}
		if low < easeMin {
			t.Errorf("ease fell below the floor: %.2f", low)
		}
		high := 2.9
		for i := 0; i < 20; i++ {
			_, high, _ = ScheduleReview(now, 0, high, true)
		}
		if high > easeMax {
			t.Errorf("ease rose above the ceiling: %.2f", high)
		}
	})

	t.Run("a zero ease is replaced rather than trusted", func(t *testing.T) {
		_, ease, _ := ScheduleReview(now, 0, 0, true)
		if ease <= 1 {
			t.Errorf("ease = %.2f, want a sane default", ease)
		}
	})
}

func TestDeriveLessonStates(t *testing.T) {
	cases := []struct {
		name      string
		completed []bool
		started   []bool
		// opened marks lessons whose LEVEL a proficiency test opened. Nil means none.
		opened []bool
		want   []string
	}{
		{
			name:      "a fresh learner sees one available lesson",
			completed: []bool{false, false, false},
			started:   []bool{false, false, false},
			want:      []string{"available", "locked", "locked"},
		},
		{
			name:      "completing a lesson unlocks the next",
			completed: []bool{true, false, false},
			started:   []bool{true, false, false},
			want:      []string{"completed", "available", "locked"},
		},
		{
			name:      "a lesson in progress stays available",
			completed: []bool{true, false, false},
			started:   []bool{true, true, false},
			want:      []string{"completed", "in_progress", "available"},
		},
		{
			name:      "an empty path is not an error",
			completed: []bool{},
			started:   []bool{},
			want:      []string{},
		},
		{
			name:      "everything completed",
			completed: []bool{true, true},
			started:   []bool{true, true},
			want:      []string{"completed", "completed"},
		},
		{
			// The case the feature exists for: a learner who tested out of the level
			// those last two lessons belong to can start there, while the level before
			// it keeps the ordinary one-available rule.
			name:      "a level opened by a test is available in full",
			completed: []bool{false, false, false, false},
			started:   []bool{false, false, false, false},
			opened:    []bool{false, false, true, true},
			want:      []string{"available", "locked", "available", "available"},
		},
		{
			// An unlock opens ONE level. If it leaked forward, a single test would
			// eventually open the whole curriculum.
			name:      "an opened level does not open the level after it",
			completed: []bool{true, true, false, false},
			started:   []bool{true, true, false, false},
			opened:    []bool{false, false, true, false},
			want:      []string{"completed", "completed", "available", "locked"},
		},
		{
			name:      "a lesson already begun inside an opened level stays in progress",
			completed: []bool{false, false, false},
			started:   []bool{false, true, false},
			opened:    []bool{false, true, true},
			want:      []string{"available", "in_progress", "available"},
		},
		{
			name:      "an opened slice shorter than the path is not an error",
			completed: []bool{false, false, false},
			started:   []bool{false, false, false},
			opened:    []bool{true},
			want:      []string{"available", "locked", "locked"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveLessonStates(tc.completed, tc.started, tc.opened)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestPlacementLevel(t *testing.T) {
	cases := []struct {
		score      float64
		levelCount int
		want       int
	}{
		{score: 0.0, levelCount: 6, want: 1},
		{score: 0.1, levelCount: 6, want: 1},
		{score: 0.5, levelCount: 6, want: 4},
		{score: 1.0, levelCount: 6, want: 6},
		{score: 1.5, levelCount: 6, want: 6}, // clamped
		{score: -1, levelCount: 6, want: 1},  // clamped
		{score: 0.4, levelCount: 9, want: 4}, // HSK 3.0's nine levels
		{score: 0.9, levelCount: 0, want: 1}, // a curriculum with no levels
	}
	for _, tc := range cases {
		if got := PlacementLevel(tc.score, tc.levelCount); got != tc.want {
			t.Errorf("PlacementLevel(%.2f, %d) = %d, want %d", tc.score, tc.levelCount, got, tc.want)
		}
	}
}
