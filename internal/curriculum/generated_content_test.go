package curriculum_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/valeriandwipurnomo/hanpath/internal/curriculum"
	"github.com/valeriandwipurnomo/hanpath/internal/exercises"
)

// The authored content is a build artifact of cmd/contentgen, and a generated file
// can rot in ways nobody notices until a learner meets a broken exercise: a level
// regenerated after the tool changed, a character added without copying its stroke
// data, a listening item whose audio_text went missing. These checks are the same
// ones the importer applies, run in a second against the files in the repository —
// no database, no audio synthesis — so CI catches the drift.
//
// The integration suite still imports level 1 for real; importing every generated
// level there would mean synthesising ~2,300 audio assets per test run.

const contentDir = "../../content/hsk2.0"

// publishedCounts is how many words each HSK 2.0 level newly introduces, taken from
// the lists in content/hsk2.0/VOCABULARY-LICENCE.txt. Level 1 is hand-authored for
// this application; levels 2-5 are the complete derived lists.
var publishedCounts = map[int]int{1: 46, 2: 147, 3: 298, 4: 598, 5: 1298}

func contentFiles(t *testing.T) []int {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(contentDir, "level-*.yaml"))
	if err != nil {
		t.Fatalf("globbing content: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("no content files under %s", contentDir)
	}
	levels := make([]int, 0, len(paths))
	for _, path := range paths {
		base := strings.TrimSuffix(filepath.Base(path), ".yaml")
		n, err := strconv.Atoi(strings.TrimPrefix(base, "level-"))
		if err != nil {
			t.Fatalf("unexpected content file name %q", base)
		}
		levels = append(levels, n)
	}
	sort.Ints(levels)
	return levels
}

func parseLevel(t *testing.T, level int) *curriculum.File {
	t.Helper()
	path := filepath.Join(contentDir, fmt.Sprintf("level-%d.yaml", level))
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()
	// Parse is strict: an unknown key in a content file is an error, not a field
	// that quietly goes missing.
	parsed, err := curriculum.Parse(f)
	if err != nil {
		t.Fatalf("%s does not parse: %v", path, err)
	}
	return parsed
}

// TestEveryLevelIsWellFormed checks the structural rules the importer enforces, so a
// malformed regeneration fails here instead of at the next import.
func TestEveryLevelIsWellFormed(t *testing.T) {
	for _, level := range contentFiles(t) {
		doc := parseLevel(t, level)
		if doc.Version != "hsk2.0" {
			t.Errorf("level %d: version %q, want hsk2.0", level, doc.Version)
		}
		if len(doc.Levels) != 1 {
			t.Fatalf("level %d: file declares %d levels, want exactly 1 (one file per level)", level, len(doc.Levels))
		}
		lv := doc.Levels[0]
		if lv.Level != level {
			t.Errorf("level %d: declares level %d", level, lv.Level)
		}
		if lv.Title == "" || lv.Description == "" {
			t.Errorf("level %d: title and description are required", level)
		}
		switch lv.Stage {
		case "elementary", "intermediate", "advanced":
		default:
			t.Errorf("level %d: stage %q is not one of the three syllabus stages", level, lv.Stage)
		}
		if len(lv.Units) == 0 {
			t.Errorf("level %d: no units", level)
		}

		defined := map[string]bool{}
		for _, v := range doc.Vocabulary {
			if v.Key == "" || v.Simplified == "" || v.Traditional == "" || v.Pinyin == "" {
				t.Errorf("level %d: vocabulary entry %q is missing a required field: %+v", level, v.Key, v)
			}
			if len(v.Senses) == 0 {
				t.Errorf("level %d: %q has no sense", level, v.Key)
			}
			if v.Levels["hsk2.0"] != level {
				t.Errorf("level %d: %q maps to level %d", level, v.Key, v.Levels["hsk2.0"])
			}
			if defined[v.Key] {
				t.Errorf("level %d: duplicate vocabulary key %q", level, v.Key)
			}
			defined[v.Key] = true
		}

		seenExerciseKeys := map[string]bool{}
		for _, unit := range lv.Units {
			if unit.Title == "" {
				t.Errorf("level %d: unit %d has no title", level, unit.Unit)
			}
			for _, lesson := range unit.Lessons {
				if lesson.Title == "" || len(lesson.Exercises) == 0 {
					t.Errorf("level %d: lesson %q needs a title and at least one exercise", level, lesson.Title)
				}
				for _, key := range lesson.Vocabulary {
					if !defined[key] {
						t.Errorf("level %d: lesson %q references vocabulary %q that the file does not define", level, lesson.Title, key)
					}
				}
				for _, ex := range lesson.Exercises {
					if seenExerciseKeys[ex.Key] {
						t.Errorf("level %d: duplicate exercise key %q", level, ex.Key)
					}
					seenExerciseKeys[ex.Key] = true
					if ex.Difficulty < 1 || ex.Difficulty > 5 {
						t.Errorf("level %d: %s has difficulty %d", level, ex.Key, ex.Difficulty)
					}
				}
			}
		}
	}
}

// TestEveryLevelCoversThePublishedWordList: a regeneration that silently dropped
// words would otherwise look perfectly healthy.
func TestEveryLevelCoversThePublishedWordList(t *testing.T) {
	for _, level := range contentFiles(t) {
		doc := parseLevel(t, level)
		want, known := publishedCounts[level]
		if !known {
			t.Errorf("level %d has no published count recorded; add it to publishedCounts", level)
			continue
		}
		if got := len(doc.Vocabulary); got != want {
			t.Errorf("level %d: %d words, want %d from the published list", level, got, want)
		}
	}
}

// TestEveryExerciseIsGradable runs each generated exercise through the registry that
// will grade it, applying exactly what the importer does to a listening item's
// prompt first. This is the check that caught `audio_text` being an unknown field
// and a hanzi drill with stroke_count 0.
func TestEveryExerciseIsGradable(t *testing.T) {
	listening := map[string]bool{}
	for _, t2 := range exercises.ListeningTypes() {
		listening[t2] = true
	}

	total, byType := 0, map[string]int{}
	for _, level := range contentFiles(t) {
		doc := parseLevel(t, level)
		for _, unit := range doc.Levels[0].Units {
			for _, lesson := range unit.Lessons {
				for _, ex := range lesson.Exercises {
					total++
					byType[ex.Type]++
					prompt := preparePromptAsTheImporterDoes(ex, listening)
					promptJSON, err := json.Marshal(prompt)
					if err != nil {
						t.Fatalf("level %d: %s prompt is not encodable: %v", level, ex.Key, err)
					}
					answerJSON, err := json.Marshal(ex.AnswerKey)
					if err != nil {
						t.Fatalf("level %d: %s answer_key is not encodable: %v", level, ex.Key, err)
					}
					if err := exercises.Validate(ex.Type, promptJSON, answerJSON); err != nil {
						t.Errorf("level %d: %s (%s) is not gradable: %v", level, ex.Key, ex.Type, err)
					}
					if listening[ex.Type] {
						if text, _ := ex.Prompt["audio_text"].(string); strings.TrimSpace(text) == "" {
							t.Errorf("level %d: %s (%s) has no audio_text, so no audio can be produced for it", level, ex.Key, ex.Type)
						}
					}
				}
			}
		}
	}
	if total == 0 {
		t.Fatal("no exercises found; the content files are empty")
	}
	types := make([]string, 0, len(byType))
	for name, n := range byType {
		types = append(types, fmt.Sprintf("%s=%d", name, n))
	}
	sort.Strings(types)
	t.Logf("validated %d exercises across %d levels: %s", total, len(contentFiles(t)), strings.Join(types, " "))
}

// preparePromptAsTheImporterDoes mirrors prepareExercisePrompt: authored content
// states `audio_text` (what the synthesiser speaks) and the importer replaces it
// with the content-hash `audio_key` it generated. A listening item with no audio is
// invalid, so the shape validated must include that key.
func preparePromptAsTheImporterDoes(ex curriculum.ExerciseDef, listening map[string]bool) map[string]any {
	out := make(map[string]any, len(ex.Prompt)+1)
	audioText := ""
	for k, v := range ex.Prompt {
		if k == "audio_text" {
			if s, ok := v.(string); ok {
				audioText = strings.TrimSpace(s)
			}
			continue
		}
		out[k] = v
	}
	if listening[ex.Type] || ex.Type == "speak_repeat" || audioText != "" {
		out["audio_key"] = "pending-audio"
	}
	return out
}

// TestWritingDrillsHaveStrokeData: the client draws these characters from
// web/public/hanzi, copied by web/scripts/copy-hanzi-data.mjs from the characters
// the content mentions. Regenerating content with new characters without running
// that copy step would ship a writing drill that cannot render.
func TestWritingDrillsHaveStrokeData(t *testing.T) {
	dir := "../../web/public/hanzi"
	checked := 0
	for _, level := range contentFiles(t) {
		doc := parseLevel(t, level)
		for _, unit := range doc.Levels[0].Units {
			for _, lesson := range unit.Lessons {
				for _, ex := range lesson.Exercises {
					if ex.Type != "hanzi_trace" {
						continue
					}
					character, _ := ex.Prompt["character"].(string)
					if len([]rune(character)) != 1 {
						t.Errorf("level %d: %s traces %q, which is not a single character", level, ex.Key, character)
						continue
					}
					checked++
					if _, err := os.Stat(filepath.Join(dir, character+".json")); err != nil {
						t.Errorf("level %d: %s traces %q but %s/%s.json is missing — run `make web-hanzi-data`", level, ex.Key, character, dir, character)
					}
					strokes, _ := ex.Prompt["stroke_count"].(int)
					if strokes <= 0 {
						t.Errorf("level %d: %s has stroke_count %v; the registry rejects a non-positive count", level, ex.Key, ex.Prompt["stroke_count"])
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no writing drills found; expected one per generated lesson")
	}
	t.Logf("%d writing drills have stroke data present", checked)
}
