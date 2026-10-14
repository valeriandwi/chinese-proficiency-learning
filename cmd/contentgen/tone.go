package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/valeriandwipurnomo/hanpath/internal/curriculum"
)

// Tone drills: one per word, for every level.
//
// The rules that make them honest, and the reason they look the way they do:
//
//   - A single-syllable word is asked about by SOUND. One syllable cannot undergo tone
//     sandhi, so the audio and the answer key can never disagree.
//   - A multi-syllable word is asked about as WRITTEN. 你好 is written 3-3 and spoken
//     2-3; a listening question would mark a learner wrong for hearing correctly. The
//     explanation states both forms, which teaches the sandhi rather than hiding it.
//   - Tone drills need no speech recognition, which is what makes a tone test exist for
//     every level even on a deployment with no pronunciation provider.

// toneOrder is the fixed order tones are presented and chosen from, so the option
// lists — and therefore the generated files — are deterministic.
var toneOrder = []string{"1", "2", "3", "4", "5"}

// toneNames are the option labels a learner reads. They double as the explanation for
// what the tone sounds like, because "3rd tone" alone does not tell a beginner much.
var toneNames = map[string]string{
	"1": "1st tone — high level",
	"2": "2nd tone — rising",
	"3": "3rd tone — falling then rising",
	"4": "4th tone — falling",
	"5": "neutral — light and short",
}

// numericSyllable matches one syllable with its tone digit, e.g. "dian4" in "dian4ying3".
var numericSyllable = regexp.MustCompile(`(?i)([a-zü]+[1-5])`)

// tonePattern turns the source list's tone-numbered pinyin into a pattern:
// "dian4ying3" -> "4-3", "ba5" -> "5". It returns "" when the pinyin carries no tone
// information at all, which is a word this generator must not guess about.
func tonePattern(numeric string) string {
	syllables := numericSyllable.FindAllString(strings.TrimSpace(numeric), -1)
	tones := make([]string, 0, len(syllables))
	for _, s := range syllables {
		tones = append(tones, s[len(s)-1:])
	}
	return strings.Join(tones, "-")
}

// vowelTones maps tone-marked vowels to their tone number. Unmarked vowels are neutral
// in pinyin, which is exactly what a dictionary treats them as.
var vowelTones = map[rune]string{
	'ā': "1", 'ē': "1", 'ī': "1", 'ō': "1", 'ū': "1", 'ǖ': "1",
	'á': "2", 'é': "2", 'í': "2", 'ó': "2", 'ú': "2", 'ǘ': "2",
	'ǎ': "3", 'ě': "3", 'ǐ': "3", 'ǒ': "3", 'ǔ': "3", 'ǚ': "3",
	'à': "4", 'è': "4", 'ì': "4", 'ò': "4", 'ù': "4", 'ǜ': "4",
}

// tonePatternFromPinyin derives a tone pattern from tone-marked pinyin, for the
// hand-authored level where the source list does not exist.
//
// A Chinese character is a syllable, and a syllable carries at most one tone mark; so
// the marks are assigned in order and any syllable left without one is neutral, which
// is the pinyin convention (吗 "ma" is neutral, 朋友 "péngyou" ends neutral).
func tonePatternFromPinyin(word, pinyin string) string {
	syllables := []rune(word)
	if len(syllables) == 0 {
		return ""
	}
	marks := make([]string, 0, len(syllables))
	for _, r := range pinyin {
		if tone, ok := vowelTones[r]; ok {
			marks = append(marks, tone)
		}
	}
	if len(marks) == 0 {
		// No mark anywhere: a whole-word neutral pattern (吗, 呢, 的).
		return strings.TrimSuffix(strings.Repeat("5-", len(syllables)), "-")
	}
	tones := make([]string, 0, len(syllables))
	next := 0
	for range syllables {
		if next < len(marks) {
			tones = append(tones, marks[next])
			next++
			continue
		}
		tones = append(tones, "5")
	}
	return strings.Join(tones, "-")
}

// spokenPattern applies the one sandhi rule this generator knows: a third tone followed
// by another third tone is spoken as a second tone. It is reported, never used as the
// answer, because the drill asks about the written tones.
func spokenPattern(pattern string) string {
	tones := strings.Split(pattern, "-")
	changed := false
	for i := 0; i+1 < len(tones); i++ {
		if tones[i] == "3" && tones[i+1] == "3" {
			tones[i] = "2"
			changed = true
		}
	}
	if !changed {
		return pattern
	}
	return strings.Join(tones, "-")
}

// toneNote is the instruction a learner reads on a speaking drill: what to aim for, and
// what actually comes out of their mouth when the two differ.
func toneNote(word, pattern string) string {
	names := make([]string, 0, 2)
	for _, t := range strings.Split(pattern, "-") {
		names = append(names, toneNames[t])
	}
	note := "Aim for " + strings.Join(names, ", then ")
	if spoken := spokenPattern(pattern); spoken != pattern {
		note += fmt.Sprintf(" (written %s, spoken %s: a third tone before another third tone becomes a second tone)", pattern, spoken)
	}
	if strings.ContainsAny(word, "一不") {
		note += " (一 and 不 change tone depending on what follows; listen to the model)"
	}
	return note
}

// toneExplanation is the "why" on a tone drill: the pinyin, the pattern in words, and
// the sandhi when the written and spoken forms differ.
func toneExplanation(word, pinyin, pattern string) string {
	names := make([]string, 0, len(strings.Split(pattern, "-")))
	for _, t := range strings.Split(pattern, "-") {
		names = append(names, toneNames[t])
	}
	explanation := fmt.Sprintf("%s (%s) is %s — %s.", word, pinyin, pattern, strings.Join(names, ", then "))
	if spoken := spokenPattern(pattern); spoken != pattern {
		explanation += fmt.Sprintf(" Spoken, the first becomes a second tone: %s.", spoken)
	}
	return explanation
}

// toneOptions builds the choices for a tone drill: the answer plus three distractors,
// in a deterministic order that is not always "a".
//
// A multiple-choice question whose answer is always the first option teaches a learner
// to pick the first option. The rotation is derived from the pattern, so it is stable
// across regenerations and still spread across the options.
func toneOptions(pattern string, single bool) ([]map[string]string, string) {
	correct := toneOptionLabel(pattern, single)
	distractors := make([]string, 0, 4)

	if single {
		for _, t := range toneOrder {
			if label := toneNames[t]; label != correct {
				distractors = append(distractors, label)
			}
		}
	} else {
		// Vary one syllable at a time: the wrong patterns are the ones a learner is
		// actually choosing between, not random noise. 白/百 and 穿/船 are the same
		// decision one syllable at a time.
		tones := strings.Split(pattern, "-")
		for i := range tones {
			for _, t := range toneOrder {
				if t == tones[i] {
					continue
				}
				alt := append([]string{}, tones...)
				alt[i] = t
				label := strings.Join(alt, "-")
				if label != correct && !contains(distractors, label) {
					distractors = append(distractors, label)
				}
			}
		}
	}
	sort.Strings(distractors)
	if len(distractors) > 3 {
		// Deterministic spread rather than the first three sorted.
		span := len(distractors) / 3
		distractors = []string{distractors[0], distractors[span], distractors[min(len(distractors)-1, 2*span)]}
		for len(distractors) < 3 {
			distractors = append(distractors, distractors[len(distractors)-1])
		}
	}
	for len(distractors) < 3 {
		// A one-syllable word has four other labels at most; pad deterministically when
		// the answer is neutral and the pool is short.
		distractors = append(distractors, fmt.Sprintf("%d-3", len(distractors)+1))
	}

	labels := append([]string{correct}, distractors[:3]...)
	// Rotate so the answer is not always first: offset by the pattern's own digits.
	offset := 0
	for _, r := range pattern {
		offset += int(r)
	}
	offset %= len(labels)
	labels = append(labels[offset:], labels[:offset]...)

	ids := []string{"a", "b", "c", "d"}
	options := make([]map[string]string, 0, len(labels))
	correctID := ""
	for i, label := range labels {
		options = append(options, map[string]string{"id": ids[i], "text": label})
		if label == correct {
			correctID = ids[i]
		}
	}
	return options, correctID
}

func toneOptionLabel(pattern string, single bool) string {
	if single {
		return toneNames[pattern]
	}
	return pattern
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// toneExercise builds the drill for one word.
func toneExercise(prefix string, n int, word, pinyin, pattern string, level int, difficulty int) curriculum.ExerciseDef {
	single := !strings.Contains(pattern, "-")
	options, correctID := toneOptions(pattern, single)

	if single {
		// Asked by sound: one syllable cannot sandhi, so the audio IS the answer.
		return curriculum.ExerciseDef{
			Key:         fmt.Sprintf("%s-tone%d", prefix, n),
			Type:        "tone_mcq",
			Skill:       "listening",
			Difficulty:  difficulty,
			Explanation: toneExplanation(word, pinyin, pattern),
			Prompt: map[string]any{
				"instruction":  "Listen, then choose the tone you heard.",
				"text":         word,
				"audio_text":   word,
				"replay_limit": 3,
				"options":      options,
			},
			AnswerKey: map[string]any{
				"correct_id": correctID,
				"tones":      pattern,
				"why":        toneExplanation(word, pinyin, pattern),
			},
		}
	}
	// Asked as written, because the spoken form of a multi-syllable word can differ from
	// the tones it is written with.
	return curriculum.ExerciseDef{
		Key:         fmt.Sprintf("%s-tone%d", prefix, n),
		Type:        "tone_mcq",
		Skill:       "reading",
		Difficulty:  difficulty,
		Explanation: toneExplanation(word, pinyin, pattern),
		Prompt: map[string]any{
			"instruction": "Choose the tones written for this word.",
			"text":        word,
			"options":     options,
		},
		AnswerKey: map[string]any{
			"correct_id": correctID,
			"tones":      pattern,
			"why":        toneExplanation(word, pinyin, pattern),
		},
	}
}

// tonePairExercise builds a discrimination drill for words that differ only in tone.
//
// This is the ear-training drill tone teaching is actually made of: 白 and 百 differ by
// nothing but tone, so being able to tell them apart is the skill. Two words sharing a
// syllable AND a tone (近/进) are not a pair and are never used as one.
func tonePairExercise(prefix string, n int, members []tonePairMember, difficulty int) (curriculum.ExerciseDef, bool) {
	if len(members) < 2 {
		return curriculum.ExerciseDef{}, false
	}
	// Deterministic: the first two members of the group, in list order.
	a, b := members[0], members[1]
	options := []map[string]string{
		{"id": "a", "text": a.word},
		{"id": "b", "text": b.word},
	}
	// Two options are enough to be unambiguous, and harder than four: the learner has
	// no elimination to lean on.
	if len(members) >= 3 {
		options = append(options, map[string]string{"id": "c", "text": members[2].word})
	}

	why := fmt.Sprintf("%s is %s (%s); %s is %s (%s). They differ only in tone.",
		a.word, a.tone, a.pattern, b.word, b.tone, b.pattern)
	return curriculum.ExerciseDef{
		Key:         fmt.Sprintf("%s-pair%d", prefix, n),
		Type:        "listening_mcq",
		Skill:       "listening",
		Difficulty:  difficulty,
		Explanation: why,
		Prompt: map[string]any{
			"instruction":  "These words differ only in tone. Listen, then choose the one you heard.",
			"audio_text":   a.word,
			"replay_limit": 3,
			"options":      options,
		},
		AnswerKey: map[string]any{
			"correct_id": "a",
			"transcript": a.word,
			"why":        why,
		},
	}, true
}

type tonePairMember struct {
	word    string
	pattern string
	tone    string
}

// tonePairs finds, for each base syllable in a word list, the words that share it and
// differ in tone. Homophones (same syllable, same tone) are excluded: they are a
// spelling question, not a tone question.
func tonePairs(words []rawWord) map[int][][]tonePairMember {
	bases := map[string][]int{}
	for i, w := range words {
		pattern := tonePattern(numericOf(w))
		if pattern == "" {
			continue
		}
		base := strings.ReplaceAll(strings.ReplaceAll(numericOf(w), "-", ""), " ", "")
		base = regexp.MustCompile(`[1-5]`).ReplaceAllString(base, "")
		base = strings.ToLower(base)
		bases[base] = append(bases[base], i)
	}

	pairsByIndex := map[int][][]tonePairMember{}
	for _, indexes := range bases {
		if len(indexes) < 2 {
			continue
		}
		members := make([]tonePairMember, 0, len(indexes))
		seenPatterns := map[string]bool{}
		for _, i := range indexes {
			pattern := tonePattern(numericOf(words[i]))
			if seenPatterns[pattern] {
				continue // same syllable and same tone: not a tone pair
			}
			seenPatterns[pattern] = true
			members = append(members, tonePairMember{
				word:    words[i].Simplified,
				pattern: pattern,
				tone:    toneNames[pattern],
			})
		}
		if len(members) < 2 {
			continue
		}
		// The drill is attached to the first member's lesson.
		pairsByIndex[indexes[0]] = append(pairsByIndex[indexes[0]], members)
	}
	return pairsByIndex
}

func numericOf(w rawWord) string {
	if len(w.Forms) == 0 {
		return ""
	}
	return strings.TrimSpace(w.Forms[0].Transcriptions["numeric"])
}

// buildToneLevelFromAuthored derives a tone unit for the hand-authored level.
//
// Level 1's words were written by hand rather than fetched, so their tones come from
// their pinyin (see tonePatternFromPinyin). The unit is appended to the level by number
// — a second file for the same level adds a unit rather than replacing one — and the
// level's own metadata is copied from the authored file instead of restated, because
// importing this file must not change the level's title, stage or vocabulary target.
func buildToneLevelFromAuthored(header curriculum.File, authored *curriculum.File, opt options) (curriculum.File, error) {
	if len(authored.Levels) == 0 {
		return curriculum.File{}, fmt.Errorf("the authored level declares no level")
	}
	authoredLevel := authored.Levels[0]

	type wordTone struct{ key, word, pinyin, pattern string }
	words := make([]wordTone, 0, len(authored.Vocabulary))
	for _, v := range authored.Vocabulary {
		pattern := tonePatternFromPinyin(v.Simplified, v.Pinyin)
		if pattern == "" {
			return curriculum.File{}, fmt.Errorf("cannot derive a tone pattern for %q (%q)", v.Simplified, v.Pinyin)
		}
		words = append(words, wordTone{v.Key, v.Simplified, v.Pinyin, pattern})
	}
	if len(words) == 0 {
		return curriculum.File{}, fmt.Errorf("the authored level has no vocabulary to drill")
	}
	if opt.perLevel > 0 && len(words) > opt.perLevel {
		words = words[:opt.perLevel]
	}

	// The new unit takes the next number after the ones already in the level.
	unitNumber := 0
	for _, u := range authoredLevel.Units {
		if u.Unit > unitNumber {
			unitNumber = u.Unit
		}
	}
	unitNumber++
	unit := curriculum.UnitDef{
		Unit:  unitNumber,
		Title: "声调 — tones",
		Description: "One tone drill per word of this level: which tone you hear, and what is " +
			"written. Tones decide whether spoken Chinese is understood, and these need no " +
			"pronunciation service to be checked.",
	}

	difficulty := difficultyFor(authoredLevel.Level)
	lessonNumber := 0
	for start := 0; start < len(words); start += opt.wordsPerLesson {
		end := min(start+opt.wordsPerLesson, len(words))
		lessonNumber++
		prefix := fmt.Sprintf("l%d-tone-u%d-l%d", authoredLevel.Level, unitNumber, lessonNumber)
		lesson := curriculum.LessonDef{
			Lesson:           lessonNumber,
			Title:            fmt.Sprintf("声调 %d–%d", start+1, end),
			Kind:             "lesson",
			Goal:             "Name the tone of every word in this set.",
			XPReward:         10,
			EstimatedMinutes: 0,
		}
		for i := start; i < end; i++ {
			lesson.Vocabulary = append(lesson.Vocabulary, words[i].key)
			lesson.Exercises = append(lesson.Exercises, toneExercise(
				prefix, i-start+1, words[i].word, words[i].pinyin, words[i].pattern, authoredLevel.Level, difficulty))
		}
		lesson.EstimatedMinutes = max(2, (len(lesson.Exercises)*4+9)/10)
		unit.Lessons = append(unit.Lessons, lesson)
	}

	doc := header
	doc.Vocabulary = authored.Vocabulary
	doc.Passages = nil
	doc.Levels = []curriculum.LevelDef{{
		Level:       authoredLevel.Level,
		Stage:       authoredLevel.Stage,
		Title:       authoredLevel.Title,
		Description: authoredLevel.Description,
		VocabTarget: authoredLevel.VocabTarget,
		Units:       []curriculum.UnitDef{unit},
	}}
	return doc, nil
}
