package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/valeriandwipurnomo/hanpath/internal/exercises"
)

// word builds a source entry the way the word-list project shapes it.
func word(simplified, traditional, pinyin, numeric string, meanings ...string) rawWord {
	return rawWord{
		Simplified: simplified,
		Frequency:  1,
		Pos:        []string{"n"},
		Forms: []rawForm{{
			Traditional:    traditional,
			Transcriptions: map[string]string{"pinyin": pinyin, "numeric": numeric},
			Meanings:       meanings,
		}},
	}
}

func sampleWords() []rawWord {
	pairs := [][3]string{
		{"吧", "ba", "ba5"}, {"手机", "shǒujī", "shou3ji1"}, {"电影", "diànyǐng", "dian4ying3"},
		{"考试", "kǎoshì", "kao3shi4"}, {"房间", "fángjiān", "fang2jian1"}, {"帮忙", "bāngmáng", "bang1mang2"},
		{"杯子", "bēizi", "bei1zi5"}, {"时间", "shíjiān", "shi2jian1"}, {"机场", "jīchǎng", "ji1chang3"},
		{"眼镜", "yǎnjìng", "yan3jing4"}, {"或者", "huòzhě", "huo4zhe3"}, {"其实", "qíshí", "qi2shi2"},
	}
	words := make([]rawWord, 0, len(pairs))
	for i, p := range pairs {
		words = append(words, word(p[0], p[0], p[1], p[2], fmt.Sprintf("meaning %d", i+1)))
	}
	return words
}

// fixedStrokes stands in for the stroke-data package in tests.
type fixedStrokes map[string]int

func (f fixedStrokes) count(character string) (int, error) {
	n, ok := f[character]
	if !ok {
		return 0, fmt.Errorf("no stroke data for %q", character)
	}
	return n, nil
}

// asTheImporterSeesIt applies what prepareExercisePrompt does before validation:
// authored content states `audio_text` (what the synthesiser speaks), and the
// importer replaces it with the content-hash `audio_key` it just generated. A
// listening item with no audio is invalid, so the key must be present.
func asTheImporterSeesIt(exType string, prompt map[string]any) map[string]any {
	out := make(map[string]any, len(prompt)+1)
	for k, v := range prompt {
		if k == "audio_text" {
			continue
		}
		out[k] = v
	}
	if exType == "listening_mcq" || exType == "listening_dictation" || exType == "speak_repeat" {
		out["audio_key"] = "pending-audio"
	}
	return out
}

func keysAndGlosses(words []rawWord) ([]string, []string) {
	taken := map[string]int{}
	keys := make([]string, len(words))
	glosses := make([]string, len(words))
	for i, w := range words {
		keys[i] = vocabKey(w, taken)
		glosses[i] = gloss(w)
	}
	return keys, glosses
}

func TestMeaningOptionsAreUniqueAndContainTheAnswer(t *testing.T) {
	words := sampleWords()
	keys, glosses := keysAndGlosses(words)
	for i := range words {
		first := meaningOptions(i, keys, glosses, words)
		second := meaningOptions(i, keys, glosses, words)

		if len(first) != 4 {
			t.Fatalf("word %d: got %d options, want 4", i, len(first))
		}
		seen := map[string]bool{}
		ids := map[string]bool{}
		for _, o := range first {
			if o["text"] == "" || o["id"] == "" {
				t.Fatalf("word %d: option with empty id or text: %v", i, o)
			}
			if seen[o["text"]] {
				t.Fatalf("word %d: duplicate option text %q", i, o["text"])
			}
			seen[o["text"]] = true
			ids[o["id"]] = true
		}
		if !seen[words[i].Simplified] {
			t.Fatalf("word %d: the answer %q is not among its own options", i, words[i].Simplified)
		}
		for _, id := range []string{"a", "b", "c", "d"} {
			if !ids[id] {
				t.Fatalf("word %d: option id %q missing", i, id)
			}
		}
		// Deterministic: regenerating must not reshuffle the choices.
		if jsonEqual(first, second) == false {
			t.Fatalf("word %d: options differ between runs", i)
		}
	}
}

func jsonEqual(a, b []map[string]string) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// The whole point of generating content is that it must be importable. Each
// exercise is validated by the registry that will grade it, exactly as the importer
// does — with the audio key the media pipeline attaches to a listening item.
func TestGeneratedExercisesPassTheRegistryValidator(t *testing.T) {
	words := sampleWords()
	keys, glosses := keysAndGlosses(words)
	strokes := fixedStrokes{"吧": 7}
	lesson, err := buildLesson("hsk2.0", 2, 1, 0, 6, words, keys, glosses, strokes, nil)
	if err != nil {
		t.Fatalf("building the lesson: %v", err)
	}

	if len(lesson.Exercises) < 18 {
		t.Fatalf("lesson has %d exercises; expected a drill for every word plus extras", len(lesson.Exercises))
	}
	seenKeys := map[string]bool{}
	for _, ex := range lesson.Exercises {
		if seenKeys[ex.Key] {
			t.Fatalf("duplicate exercise key %q", ex.Key)
		}
		seenKeys[ex.Key] = true

		prompt := asTheImporterSeesIt(ex.Type, ex.Prompt)
		blob, err := json.Marshal(prompt)
		if err != nil {
			t.Fatalf("%s: prompt not encodable: %v", ex.Key, err)
		}
		answerBlob, err := json.Marshal(ex.AnswerKey)
		if err != nil {
			t.Fatalf("%s: answer_key not encodable: %v", ex.Key, err)
		}
		if err := exercises.Validate(ex.Type, blob, answerBlob); err != nil {
			t.Errorf("%s (%s) rejected by the registry: %v", ex.Key, ex.Type, err)
		}
	}
}

func TestVocabKeysAreUniqueForHomophones(t *testing.T) {
	words := []rawWord{
		word("是", "是", "shì", "shi4", "to be"),
		word("事", "事", "shì", "shi4", "matter"),
		word("市", "市", "shì", "shi4", "city"),
	}
	taken := map[string]int{}
	seen := map[string]bool{}
	for _, w := range words {
		key := vocabKey(w, taken)
		if seen[key] {
			t.Fatalf("key %q issued twice", key)
		}
		seen[key] = true
	}
	if len(seen) != 3 {
		t.Fatalf("expected three distinct keys, got %v", seen)
	}
}

func TestGlossIsTrimmedNotInvented(t *testing.T) {
	long := word("吧", "吧", "bā", "ba1", "bar (loanword) (serving drinks, or providing Internet access etc)")
	trimmed := gloss(long)
	if trimmed == "" {
		t.Fatal("a long gloss must still yield something usable")
	}
	if utf8.RuneCountInString(trimmed) > 41 {
		t.Fatalf("gloss not trimmed: %q (%d runes)", trimmed, utf8.RuneCountInString(trimmed))
	}
	if !utf8.ValidString(trimmed) {
		t.Fatalf("gloss is not valid UTF-8: %q", trimmed)
	}

	short := word("你", "你", "nǐ", "ni3", "you (singular)")
	if got := gloss(short); got != "you (singular)" {
		t.Fatalf("a short gloss must be kept as-is, got %q", got)
	}
}

func TestLevelStageAndDifficultyFollowTheSyllabus(t *testing.T) {
	cases := map[int]string{1: "elementary", 2: "elementary", 3: "intermediate", 4: "intermediate", 5: "advanced", 6: "advanced"}
	for level, want := range cases {
		if got := stageFor(level); got != want {
			t.Errorf("stageFor(%d) = %q, want %q", level, got, want)
		}
	}
	for level, want := range map[int]int{2: 1, 3: 2, 4: 3, 5: 4, 6: 4} {
		if got := difficultyFor(level); got != want {
			t.Errorf("difficultyFor(%d) = %d, want %d", level, got, want)
		}
	}
}
