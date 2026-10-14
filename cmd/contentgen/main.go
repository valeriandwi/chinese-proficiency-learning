// Command contentgen generates vocabulary curriculum levels from the published
// HSK 2.0 word lists.
//
// Why a generator rather than hand-authored YAML: the word lists are data (the
// syllabus authority), and hand-writing several thousand drills would be both
// slower and less consistent than deriving them. What a generator must NOT do is
// invent language, so this tool produces only drills that can be derived from a
// word list without judgement — meaning, listening, dictation and speaking
// practice over the word itself, plus a stroke-order drill for a single-character
// word. Sentences (fill_blank, sentence_order, translation, short_answer) need
// authored Chinese and are deliberately out of scope: a generator asked to invent
// them would produce plausible but wrong grammar.
//
// It builds internal/curriculum's own types and marshals them, so the output
// cannot drift from the schema the importer parses — the agreement is checked by
// the compiler, not by a comment.
//
// Source data: the Complete HSK Vocabulary project (MIT) — see
// content/hsk2.0/VOCABULARY-LICENCE.txt for the notice and the fetch command.
//
// Usage:
//
//	go run ./cmd/contentgen -levels 2,3,4,5 -raw .local/wordlists -out content/hsk2.0
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/valeriandwipurnomo/hanpath/internal/curriculum"
)

// rawWord is one entry of the third-party list. Only the fields we use are
// decoded; the source carries transliterations in five systems and the rest is
// noise for our purposes.
type rawWord struct {
	Simplified string    `json:"simplified"`
	Radical    string    `json:"radical"`
	Frequency  int       `json:"frequency"`
	Pos        []string  `json:"pos"`
	Forms      []rawForm `json:"forms"`
}

// rawForm is named rather than anonymous so tests can build words without
// restating the shape.
type rawForm struct {
	Traditional    string            `json:"traditional"`
	Transcriptions map[string]string `json:"transcriptions"`
	Meanings       []string          `json:"meanings"`
}

// posNames maps the source's part-of-speech codes onto words. Chinese lexicography
// uses the CEDICT family of abbreviations, and the source follows it.
var posNames = map[string]string{
	"n": "noun", "nr": "noun", "ns": "noun", "nt": "noun", "nz": "noun", "an": "noun",
	"v": "verb", "vn": "verb", "vi": "verb", "vt": "verb",
	"a": "adjective", "d": "adverb", "m": "numeral", "q": "classifier", "r": "pronoun",
	"p": "preposition", "c": "conjunction", "u": "particle", "y": "particle",
	"t": "time word", "f": "localizer", "s": "place word", "e": "interjection",
	"o": "onomatopoeia", "x": "other", "z": "other",
}

// stageFor maps an HSK 2.0 level onto the syllabus stage the validator accepts.
func stageFor(level int) string {
	switch {
	case level <= 2:
		return "elementary"
	case level <= 4:
		return "intermediate"
	default:
		return "advanced"
	}
}

// difficultyFor keeps new words below the four hardest rungs: a level's first pass
// is vocabulary recognition, not exam trickery.
func difficultyFor(level int) int {
	if level > 4 {
		return 4
	}
	return level - 1
}

type options struct {
	levels         []int
	rawDir         string
	outDir         string
	hanziData      string
	wordsPerUnit   int
	wordsPerLesson int
	perLevel       int
}

func main() {
	var levelsFlag string
	var opt options
	flag.StringVar(&levelsFlag, "levels", "2,3,4,5", "comma-separated HSK levels to generate")
	flag.StringVar(&opt.rawDir, "raw", ".local/wordlists", "directory holding old-<level>.json from the word-list project")
	flag.StringVar(&opt.outDir, "out", "content/hsk2.0", "directory to write level-<n>.yaml into")
	flag.IntVar(&opt.wordsPerUnit, "unit-words", 24, "words per unit")
	flag.IntVar(&opt.wordsPerLesson, "lesson-words", 6, "words per lesson")
	flag.IntVar(&opt.perLevel, "per-level", 0, "cap the words taken from each level (0 = the whole list)")
	flag.StringVar(&opt.hanziData, "hanzi-data", "web/node_modules/hanzi-writer-data", "stroke-order data package, the source of stroke counts")
	flag.Parse()

	for _, part := range strings.Split(levelsFlag, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 {
			fail("levels must be a comma-separated list of positive numbers, got %q", levelsFlag)
		}
		opt.levels = append(opt.levels, n)
	}
	sort.Ints(opt.levels)

	header, err := readHeader(filepath.Join(opt.outDir, "level-1.yaml"))
	if err != nil {
		fail("reading the level 1 header: %v", err)
	}

	strokes := newStrokeIndex(opt.hanziData)
	for _, level := range opt.levels {
		words, err := readWords(filepath.Join(opt.rawDir, fmt.Sprintf("old-%d.json", level)), level, opt.perLevel)
		if err != nil {
			fail("level %d: %v", level, err)
		}
		doc, err := buildLevel(header, level, words, opt, strokes)
		if err != nil {
			fail("level %d: %v", level, err)
		}
		path := filepath.Join(opt.outDir, fmt.Sprintf("level-%d.yaml", level))
		if err := writeDocument(path, doc); err != nil {
			fail("level %d: %v", level, err)
		}
		exercises := 0
		for _, unit := range doc.Levels[0].Units {
			for _, lesson := range unit.Lessons {
				exercises += len(lesson.Exercises)
			}
		}
		fmt.Printf("level %d: %d words, %d units, %d lessons, %d exercises -> %s\n",
			level, len(doc.Vocabulary), len(doc.Levels[0].Units), countLessons(doc.Levels[0]), exercises, path)
	}
}

// readHeader takes the curriculum metadata from the hand-authored level 1 file, so
// every generated file carries an identical header and importing one cannot
// rewrite another's name, standard or notes.
func readHeader(path string) (curriculum.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return curriculum.File{}, err
	}
	defer f.Close()
	parsed, err := curriculum.Parse(f)
	if err != nil {
		return curriculum.File{}, err
	}
	return *parsed, nil
}

func readWords(path string, level, limit int) ([]rawWord, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s (fetch it with the command in content/hsk2.0/VOCABULARY-LICENCE.txt): %w", path, err)
	}
	var words []rawWord
	if err := json.Unmarshal(blob, &words); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	keep := make([]rawWord, 0, len(words))
	for _, w := range words {
		if len(w.Forms) == 0 || w.Simplified == "" {
			continue
		}
		if gloss(w) == "" {
			continue
		}
		keep = append(keep, w)
	}
	if limit > 0 && len(keep) > limit {
		keep = keep[:limit]
	}
	if len(keep) == 0 {
		return nil, fmt.Errorf("no usable entries in %s", path)
	}
	return keep, nil
}

// glossNoise are dictionary conventions, not meanings: a learner asking what 白
// means is not served by "surname Bai", and a choice list of "variant of…" entries
// teaches nothing. Skipped in favour of the first real gloss.
var glossNoise = []string{
	"surname ", "variant of ", "old variant of ", "see ", "abbr. for ", "(onom", "onom.",
	"used in ", "erhua variant of ", "Japanese variant of ", "Taiwan pr. ",
}

func gloss(w rawWord) string {
	if len(w.Forms) == 0 {
		return ""
	}
	// The source lists senses in dictionary order, not by how a learner meets them,
	// so scan every form for the first gloss that is an actual meaning.
	var chosen string
	for _, form := range w.Forms {
		for _, meaning := range form.Meanings {
			text := strings.TrimSpace(meaning)
			if text == "" {
				continue
			}
			noisy := false
			for _, prefix := range glossNoise {
				if strings.HasPrefix(strings.ToLower(text), prefix) {
					noisy = true
					break
				}
			}
			if noisy {
				continue
			}
			chosen = text
			break
		}
		if chosen != "" {
			break
		}
	}
	if chosen == "" {
		chosen = w.Forms[0].Meanings[0]
	}
	text := strings.TrimSpace(strings.Split(chosen, ";")[0])
	text = strings.TrimSpace(strings.TrimSuffix(text, "."))
	// Drop trailing parentheticals ("bar (loanword) (serving drinks...)") while a
	// useful gloss remains, otherwise truncate on a word boundary.
	for len(text) > 40 {
		opened := strings.LastIndex(text, "(")
		if opened < 12 {
			break
		}
		text = strings.TrimSpace(text[:opened])
	}
	if len(text) > 40 {
		if cut := strings.LastIndex(text[:40], " "); cut > 12 {
			text = text[:cut]
		} else {
			text = text[:40]
		}
		text += "…"
	}
	return text
}

func pinyin(w rawWord) string {
	if len(w.Forms) == 0 {
		return ""
	}
	return strings.TrimSpace(w.Forms[0].Transcriptions["pinyin"])
}

func traditional(w rawWord) string {
	if len(w.Forms) == 0 || strings.TrimSpace(w.Forms[0].Traditional) == "" {
		return w.Simplified
	}
	return strings.TrimSpace(w.Forms[0].Traditional)
}

// vocabKey prefers the source's tone-numbered pinyin ("dian4ying3"), which is
// stable and ASCII; a homophone gets a numeric suffix so keys stay unique.
func vocabKey(w rawWord, taken map[string]int) string {
	base := ""
	if len(w.Forms) > 0 {
		base = strings.TrimSpace(w.Forms[0].Transcriptions["numeric"])
	}
	base = strings.ReplaceAll(base, " ", "")
	if base == "" {
		base = fmt.Sprintf("word%d", len(taken)+1)
	}
	if n, clash := taken[base]; clash {
		taken[base] = n + 1
		return fmt.Sprintf("%s-%d", base, n+1)
	}
	taken[base] = 1
	return base
}

func posOf(w rawWord) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, code := range w.Pos {
		name, ok := posNames[strings.ToLower(strings.TrimSpace(code))]
		if !ok {
			name = "other"
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	// "other" is the source's fallback code; keep it only when nothing else is known.
	if len(out) > 1 {
		pruned := out[:0]
		for _, name := range out {
			if name != "other" {
				pruned = append(pruned, name)
			}
		}
		out = pruned
	}
	if len(out) == 0 {
		out = append(out, "other")
	}
	return out
}

func buildLevel(header curriculum.File, level int, words []rawWord, opt options, strokes strokeSource) (curriculum.File, error) {
	taken := map[string]int{}
	entries := make([]curriculum.VocabEntry, 0, len(words))
	keys := make([]string, 0, len(words))
	glosses := make([]string, 0, len(words))
	for _, w := range words {
		key := vocabKey(w, taken)
		keys = append(keys, key)
		glosses = append(glosses, gloss(w))
		entries = append(entries, curriculum.VocabEntry{
			Key:           key,
			Simplified:    w.Simplified,
			Traditional:   traditional(w),
			Pinyin:        pinyin(w),
			PartsOfSpeech: posOf(w),
			Senses:        []map[string]string{{"en": gloss(w)}},
			FreqRank:      w.Frequency,
			Levels:        map[string]int{header.Version: level},
		})
	}

	lessonIndex, unitIndex := 0, 0
	units := []curriculum.UnitDef{}
	for start := 0; start < len(words); start += opt.wordsPerUnit {
		end := min(start+opt.wordsPerUnit, len(words))
		unitIndex++
		unit := curriculum.UnitDef{
			Unit:        unitIndex,
			Title:       fmt.Sprintf("词汇 %d–%d", start+1, end),
			Description: fmt.Sprintf("Words %d–%d of the published HSK 2.0 level-%d list, in frequency order.", start+1, end, level),
		}
		for lessonStart := start; lessonStart < end; lessonStart += opt.wordsPerLesson {
			lessonEnd := min(lessonStart+opt.wordsPerLesson, end)
			lessonIndex++
			lesson, err := buildLesson(
				header.Version, level, lessonIndex, lessonStart, lessonEnd, words, keys, glosses, strokes,
			)
			if err != nil {
				return curriculum.File{}, fmt.Errorf("lesson %d: %w", lessonIndex, err)
			}
			unit.Lessons = append(unit.Lessons, lesson)
		}
		units = append(units, unit)
	}

	doc := header
	doc.Vocabulary = entries
	doc.Passages = nil
	doc.Levels = []curriculum.LevelDef{{
		Level:       level,
		Stage:       stageFor(level),
		Title:       fmt.Sprintf("Level %d — core vocabulary", level),
		Description: fmt.Sprintf("The published HSK 2.0 level-%d word list in frequency order, practised as meaning, listening and speaking drills. Thematic units are not yet curated; every word is covered by a meaning drill, a listening drill and a speaking drill.", level),
		VocabTarget: len(words),
		Units:       units,
	}}
	return doc, nil
}

func buildLesson(
	version string, level, lessonNumber, start, end int,
	words []rawWord, keys, glosses []string, strokes strokeSource,
) (curriculum.LessonDef, error) {
	lesson := curriculum.LessonDef{
		Lesson:           lessonNumber,
		Title:            fmt.Sprintf("词汇 %d–%d", start+1, end),
		Kind:             "lesson",
		Goal:             fmt.Sprintf("Recognise and say words %d–%d.", start+1, end),
		XPReward:         10 + (level-1)*2,
		EstimatedMinutes: 0,
		Vocabulary:       append([]string{}, keys[start:end]...),
	}
	difficulty := difficultyFor(level)
	prefix := fmt.Sprintf("l%d-w%d", level, lessonNumber)

	for i := start; i < end; i++ {
		n := i - start + 1
		word, meaning := words[i].Simplified, glosses[i]
		py := pinyin(words[i])

		lesson.Exercises = append(lesson.Exercises, exercise(fmt.Sprintf("%s-mcq%d", prefix, n), "mcq", "vocabulary", difficulty,
			fmt.Sprintf("%s (%s) means “%s”.", word, py, meaning),
			map[string]any{
				"question": fmt.Sprintf("Which word means “%s”?", meaning),
				"options":  meaningOptions(i, keys, glosses, words),
			},
			map[string]any{
				"correct_id": "a",
				"why":        fmt.Sprintf("%s is read %s and means “%s”.", word, py, meaning),
			}))

		lesson.Exercises = append(lesson.Exercises, exercise(fmt.Sprintf("%s-listen%d", prefix, n), "listening_mcq", "listening", difficulty,
			fmt.Sprintf("You heard %s (%s), meaning “%s”.", word, py, meaning),
			map[string]any{
				"instruction":  "Listen, then choose what was said.",
				"audio_text":   word,
				"replay_limit": 3,
				"options":      meaningOptions(i, keys, glosses, words),
			},
			map[string]any{
				"correct_id": "a",
				"transcript": word,
				"why":        fmt.Sprintf("The clip says %s (%s): “%s”.", word, py, meaning),
			}))

		lesson.Exercises = append(lesson.Exercises, exercise(fmt.Sprintf("%s-speak%d", prefix, n), "speak_repeat", "speaking", difficulty,
			fmt.Sprintf("Repeat %s (%s) after the model. Nothing here grades your tones.", word, py),
			map[string]any{
				"instruction": "Listen to the model, then record yourself repeating the word. Play both back and compare — nothing here grades your tones.",
				"audio_text":  word,
				"text":        word,
				"pinyin":      py,
				"max_seconds": 8,
			},
			map[string]any{"reference_text": word}))
	}

	// Dictation and stroke order are labour-intensive, so the lesson spreads them
	// over a couple of its words rather than repeating them for every one.
	if end-start >= 2 {
		for _, idx := range []int{start, start + min(3, end-start-1)} {
			n := idx - start + 1
			word, py, meaning := words[idx].Simplified, pinyin(words[idx]), glosses[idx]
			lesson.Exercises = append(lesson.Exercises, exercise(
				fmt.Sprintf("%s-dictation%d", prefix, n), "listening_dictation", "listening", difficulty,
				fmt.Sprintf("Type what you hear: %s (%s).", word, py),
				map[string]any{
					"instruction":  "Listen and type the word you hear, in Chinese characters.",
					"audio_text":   word,
					"replay_limit": 3,
					"hint":         fmt.Sprintf("“%s”", meaning),
				},
				map[string]any{
					"accepted":   []string{word},
					"transcript": word,
					"pinyin":     py,
				}))
		}
	}
	for idx := start; idx < end; idx++ {
		word := words[idx].Simplified
		if utf8.RuneCountInString(word) != 1 {
			continue
		}
		count, err := strokes.count(word)
		if err != nil {
			return curriculum.LessonDef{}, fmt.Errorf("writing drill for %q: %w", word, err)
		}
		n := idx - start + 1
		lesson.Exercises = append(lesson.Exercises, exercise(
			fmt.Sprintf("%s-hanzi%d", prefix, n), "hanzi_trace", "writing", difficulty,
			fmt.Sprintf("Trace %s (%s) in stroke order: %d strokes.", word, pinyin(words[idx]), count),
			map[string]any{
				"character":    word,
				"mode":         "guided",
				"stroke_count": count,
				"instruction":  fmt.Sprintf("Trace %s in the correct stroke order.", word),
			},
			map[string]any{
				"character":    word,
				"stroke_count": count,
			}))
		break // one writing drill per lesson is enough alongside the per-word drills
	}

	// A lesson's length is derived from what it actually contains, not guessed:
	// the estimate the path shows should match the number of questions in it.
	lesson.EstimatedMinutes = max(2, (len(lesson.Exercises)*4+9)/10)
	return lesson, nil
}

// meaningOptions builds the four choices for a word: three other words from the
// same level, chosen by a fixed stride so the output is reproducible, skipping any
// candidate that shares the word's characters or its gloss (two different words can
// carry the same English gloss, and a duplicate is an unanswerable question).
func meaningOptions(index int, keys, glosses []string, words []rawWord) []map[string]string {
	picked := []int{index}
	blocked := map[int]bool{index: true}
	accept := func(candidate int) bool {
		if candidate < 0 || candidate >= len(words) || blocked[candidate] {
			return false
		}
		// Two different words can carry the same English gloss, and offering the
		// learner two correct-looking choices is an unanswerable question.
		if strings.EqualFold(glosses[candidate], glosses[index]) {
			return false
		}
		for _, already := range picked {
			if words[already].Simplified == words[candidate].Simplified ||
				strings.EqualFold(glosses[already], glosses[candidate]) {
				return false
			}
		}
		return true
	}

	// A stride coprime-ish with the list length spreads the choices around the level
	// instead of clustering them on neighbours; the linear pass behind it makes sure
	// a short list still fills four slots. Both are deterministic.
	for _, stride := range []int{7, 11, 13, 17, 19, 23, 29, 31} {
		if len(picked) == 4 {
			break
		}
		for step := 1; step <= len(words); step++ {
			if candidate := (index + step*stride) % len(words); accept(candidate) {
				picked = append(picked, candidate)
				blocked[candidate] = true
				break
			}
		}
	}
	for step := 1; step < len(words) && len(picked) < 4; step++ {
		if candidate := (index + step) % len(words); accept(candidate) {
			picked = append(picked, candidate)
			blocked[candidate] = true
		}
	}
	options := make([]map[string]string, 0, len(picked))
	ids := []string{"a", "b", "c", "d"}
	for i, wordIndex := range picked {
		options = append(options, map[string]string{
			"id":   ids[i],
			"text": words[wordIndex].Simplified,
		})
	}
	return options
}

// strokeSource answers how many strokes a character has. The registry requires a
// positive count and a wrong one is a lie the learner can see, so it is read from
// the same stroke data the client draws the character with — never estimated.
type strokeSource interface {
	count(character string) (int, error)
}

// strokeIndex reads counts from the hanzi-writer-data package, which is the data
// the browser renders from. A character with no data is a hard error: the drill
// could not be drawn either.
type strokeIndex struct {
	dir   string
	cache map[string]int
}

func newStrokeIndex(dir string) *strokeIndex {
	return &strokeIndex{dir: dir, cache: map[string]int{}}
}

func (s *strokeIndex) count(character string) (int, error) {
	if n, ok := s.cache[character]; ok {
		return n, nil
	}
	path := filepath.Join(s.dir, character+".json")
	blob, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("no stroke data for %q at %s (is `pnpm install` run in web/?)", character, path)
	}
	var payload struct {
		Strokes []json.RawMessage `json:"strokes"`
	}
	if err := json.Unmarshal(blob, &payload); err != nil {
		return 0, fmt.Errorf("parsing stroke data for %q: %w", character, err)
	}
	if len(payload.Strokes) == 0 {
		return 0, fmt.Errorf("stroke data for %q lists no strokes", character)
	}
	s.cache[character] = len(payload.Strokes)
	return len(payload.Strokes), nil
}

func exercise(key, kind, skill string, difficulty int, explanation string, prompt, answerKey map[string]any) curriculum.ExerciseDef {
	return curriculum.ExerciseDef{
		Key:         key,
		Type:        kind,
		Skill:       skill,
		Difficulty:  difficulty,
		Explanation: explanation,
		Prompt:      prompt,
		AnswerKey:   answerKey,
	}
}

// writeDocument serialises, then parses its own output with the importer's strict
// reader. A generator that can write a file the importer cannot read is a trap; this
// makes that impossible to commit.
func writeDocument(path string, doc curriculum.File) error {
	blob, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encoding yaml: %w", err)
	}
	header := fmt.Sprintf(`# Generated by cmd/contentgen — do not edit by hand.
#
# Vocabulary comes from the published HSK 2.0 word lists via the Complete HSK
# Vocabulary project (MIT); see content/hsk2.0/VOCABULARY-LICENCE.txt for the notice
# and the fetch command. Drills are derived from the words themselves: this file
# contains no authored sentences, because a generator cannot be trusted to write
# grammatical Chinese.
%s`, string(blob))

	if _, err := curriculum.Parse(strings.NewReader(header)); err != nil {
		return fmt.Errorf("generated document does not parse: %w", err)
	}
	if err := os.WriteFile(path, []byte(header), 0o644); err != nil {
		return err
	}
	return nil
}

func countLessons(level curriculum.LevelDef) int {
	n := 0
	for _, unit := range level.Units {
		n += len(unit.Lessons)
	}
	return n
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "contentgen: "+format+"\n", args...)
	os.Exit(1)
}
