package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/valeriandwipurnomo/hanpath/internal/curriculum"
	"github.com/valeriandwipurnomo/hanpath/internal/exercises"
)

func TestTonePatternFromTheSourceList(t *testing.T) {
	cases := map[string]string{
		"dian4ying3": "4-3",
		"ni3":        "3",
		"ba5":        "5",
		"bei1zi5":    "1-5",
		"Bai2":       "2",
		"":           "",
		"notones":    "",
	}
	for in, want := range cases {
		if got := tonePattern(in); got != want {
			t.Errorf("tonePattern(%q) = %q, want %q", in, got, want)
		}
	}
}

// The hand-authored level has no tone numbers, so its tones come from the pinyin. The
// pinyin convention is what makes this decidable: one tone mark per syllable, and a
// syllable without a mark is neutral.
func TestTonePatternFromPinyin(t *testing.T) {
	cases := []struct{ word, pinyin, want string }{
		{"你好", "nǐ hǎo", "3-3"},
		{"吗", "ma", "5"},         // unmarked: neutral
		{"朋友", "péngyou", "2-5"}, // trailing unmarked syllable is neutral
		{"谢谢", "xièxie", "4-5"},
		{"学生", "xuéshēng", "2-1"},
		{"电影", "diànyǐng", "4-3"},
	}
	for _, tc := range cases {
		if got := tonePatternFromPinyin(tc.word, tc.pinyin); got != tc.want {
			t.Errorf("%s (%s) = %q, want %q", tc.word, tc.pinyin, got, tc.want)
		}
	}
	if tonePatternFromPinyin("", "nǐ") != "" {
		t.Error("a word with no characters has no tone pattern")
	}
}

// Sandhi is reported, never used as the answer: the drill asks what is written.
func TestSandhiIsReportedNotAnswered(t *testing.T) {
	if got := spokenPattern("3-3"); got != "2-3" {
		t.Errorf("spokenPattern(3-3) = %q, want 2-3", got)
	}
	if got := spokenPattern("4-3"); got != "4-3" {
		t.Errorf("spokenPattern(4-3) changed a pattern with no sandhi: %q", got)
	}
	if got := spokenPattern("3-3-3"); got != "2-2-3" {
		t.Errorf("spokenPattern(3-3-3) = %q, want 2-2-3", got)
	}

	note := toneNote("你好", "3-3")
	if !strings.Contains(note, "written 3-3, spoken 2-3") {
		t.Errorf("the note must state both forms: %q", note)
	}
	if plain := toneNote("电影", "4-3"); strings.Contains(plain, "written") {
		t.Errorf("a pattern without sandhi must not claim one: %q", plain)
	}
	if explain := toneExplanation("你好", "nǐ hǎo", "3-3"); !strings.Contains(explain, "second tone") {
		t.Errorf("the explanation should teach the sandhi: %q", explain)
	}
}

func TestToneOptionsAlwaysContainTheAnswerExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		single  bool
	}{{"3", true}, {"5", true}, {"1", true}, {"4-3", false}, {"2-5", false}, {"1-1-4", false}} {
		first, firstID := toneOptions(tc.pattern, tc.single)
		second, secondID := toneOptions(tc.pattern, tc.single)

		if len(first) != 4 {
			t.Fatalf("%s: got %d options, want 4", tc.pattern, len(first))
		}
		want := toneOptionLabel(tc.pattern, tc.single)
		appearances := 0
		texts := map[string]bool{}
		ids := map[string]bool{}
		for _, o := range first {
			if o["text"] == want {
				appearances++
			}
			if texts[o["text"]] {
				t.Errorf("%s: duplicate option text %q", tc.pattern, o["text"])
			}
			texts[o["text"]] = true
			ids[o["id"]] = true
		}
		if appearances != 1 {
			t.Errorf("%s: the answer appears %d times", tc.pattern, appearances)
		}
		for _, id := range []string{"a", "b", "c", "d"} {
			if !ids[id] {
				t.Errorf("%s: option id %q missing", tc.pattern, id)
			}
		}
		if firstID != secondID {
			t.Errorf("%s: the correct id changed between runs", tc.pattern)
		}
		if a, b := mustJSON(first), mustJSON(second); a != b {
			t.Errorf("%s: options are not deterministic", tc.pattern)
		}
	}
}

// A list where the answer is always "a" teaches the learner to pick the first option.
func TestToneAnswerIsNotAlwaysTheFirstOption(t *testing.T) {
	ids := map[string]bool{}
	for _, pattern := range []string{"1", "2", "3", "4", "5", "1-1", "4-3", "2-5", "1-4-2"} {
		_, id := toneOptions(pattern, !strings.Contains(pattern, "-"))
		ids[id] = true
	}
	if len(ids) < 2 {
		t.Fatalf("the correct option is always %v; the drill is guessable", ids)
	}
}

func TestTonePairsRequireADifferentTone(t *testing.T) {
	words := []rawWord{
		word("穿", "穿", "chuān", "chuan1", "to wear"),
		word("船", "船", "chuán", "chuan2", "boat"),
		word("近", "近", "jìn", "jin4", "near"),
		word("进", "进", "jìn", "jin4", "to enter"), // same syllable AND same tone
		word("水", "水", "shuǐ", "shui3", "water"),  // no partner at all
	}
	pairs := tonePairs(words)
	total := 0
	for _, groups := range pairs {
		for _, members := range groups {
			total++
			if len(members) < 2 {
				t.Fatalf("a pair group with %d members", len(members))
			}
			patterns := map[string]bool{}
			for _, m := range members {
				patterns[m.pattern] = true
			}
			if len(patterns) < 2 {
				t.Fatalf("group %v has no tone difference: it is a spelling question, not a tone one", members)
			}
		}
	}
	if total != 1 {
		t.Fatalf("got %d tone pair groups, want exactly the 穿/船 one (近/进 share a tone)", total)
	}
}

func TestToneDrillShapeFollowsTheSyllableCount(t *testing.T) {
	single := toneExercise("l2-w1", 1, "穿", "chuān", "1", 2, 1)
	if single.Type != "tone_mcq" || single.Skill != "listening" {
		t.Errorf("a one-syllable tone drill should be a listening question: %+v", single)
	}
	if single.Prompt["audio_text"] != "穿" {
		t.Error("a listening tone drill needs the audio to ask by sound")
	}

	multi := toneExercise("l2-w1", 2, "电影", "diànyǐng", "4-3", 2, 1)
	if multi.Skill != "reading" {
		t.Errorf("a multi-syllable tone drill should ask what is written: %+v", multi)
	}
	if _, hasAudio := multi.Prompt["audio_text"]; hasAudio {
		t.Error("a written tone question must not carry audio: the spoken pattern can differ from the written one")
	}

	// Both shapes must satisfy the registry that will grade them, seen as the importer
	// sees them (it is the importer that attaches the audio key).
	for _, ex := range []curriculum.ExerciseDef{single, multi} {
		promptJSON, _ := json.Marshal(asTheImporterSeesIt(ex.Type, ex.Prompt))
		answerJSON, _ := json.Marshal(ex.AnswerKey)
		if err := exercises.Validate(ex.Type, promptJSON, answerJSON); err != nil {
			t.Errorf("%s rejected by the registry: %v", ex.Key, err)
		}
	}
}

// The tone drill set must cover every word of a level, not a sample of it.
func TestEveryWordGetsAToneDrill(t *testing.T) {
	words := sampleWords()
	keys, glosses := keysAndGlosses(words)
	strokes := fixedStrokes{"吧": 7}
	lesson, err := buildLesson("hsk2.0", 2, 1, 0, len(words), words, keys, glosses, strokes, nil)
	if err != nil {
		t.Fatalf("building the lesson: %v", err)
	}
	tones := 0
	for _, ex := range lesson.Exercises {
		if ex.Type == "tone_mcq" {
			tones++
		}
	}
	if tones != len(words) {
		t.Errorf("%d tone drills for %d words", tones, len(words))
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
