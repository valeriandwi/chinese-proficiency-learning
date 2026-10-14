// One module maps every server enum to its label and tone, so a status is never
// styled inconsistently in two places and an unrecognised value degrades to a
// neutral chip that still shows the raw value.

export type Tone = 'neutral' | 'positive' | 'warning' | 'danger' | 'accent'

export type Described = { label: string; tone: Tone; raw: string }

const lessonStates: Record<string, { label: string; tone: Tone }> = {
  locked: { label: 'Locked', tone: 'neutral' },
  available: { label: 'Start', tone: 'accent' },
  in_progress: { label: 'Continue', tone: 'warning' },
  completed: { label: 'Completed', tone: 'positive' },
}

export function describeLessonState(value: string): Described {
  const known = lessonStates[value]
  return known ? { ...known, raw: value } : { label: value, tone: 'neutral', raw: value }
}

const skills: Record<string, { label: string; tone: Tone }> = {
  vocabulary: { label: 'Vocabulary', tone: 'accent' },
  grammar: { label: 'Grammar', tone: 'accent' },
  reading: { label: 'Reading', tone: 'neutral' },
  listening: { label: 'Listening', tone: 'neutral' },
  writing: { label: 'Writing', tone: 'warning' },
  speaking: { label: 'Speaking', tone: 'warning' },
}

export function describeSkill(value: string): Described {
  const known = skills[value]
  return known ? { ...known, raw: value } : { label: value, tone: 'neutral', raw: value }
}

const exerciseTypes: Record<string, string> = {
  mcq: 'Multiple choice',
  mcq_multi: 'Choose all that apply',
  true_false: 'True or false',
  fill_blank: 'Fill in the blank',
  sentence_order: 'Build the sentence',
  translation: 'Translate',
  hanzi_trace: 'Trace the character',
  hanzi_recall: 'Write from memory',
  short_answer: 'Write an answer',
  tone_mcq: 'Tone: choose',
  listening_mcq: 'Listening: choose',
  listening_dictation: 'Listening: transcribe',
  speak_repeat: 'Speaking: repeat',
  read_aloud: 'Speaking: read aloud',
  speak_answer: 'Speaking: answer',
}

export function describeExerciseType(value: string): string {
  return exerciseTypes[value] ?? value
}

const goalKinds: Record<string, string> = {
  minutes: 'minutes a day',
  xp: 'XP a day',
  lessons: 'lessons a day',
}

export function describeGoal(kind: string, value: number): string {
  return `${value} ${goalKinds[kind] ?? kind}`
}

const graders: Record<string, string> = {
  mcq: 'exact match',
  mcq_multi: 'set match',
  true_false: 'exact match',
  fill_blank: 'accepted answers',
  sentence_order: 'word order',
  translation: 'accepted answers',
  hanzi_trace: 'stroke check',
  hanzi_recall: 'stroke check',
  rubric: 'keyword rubric',
  tone_mcq: 'exact match',
  listening_mcq: 'exact match',
  listening_dictation: 'accepted answers',
  // Speaking is scored on what can be checked (a recording exists and is the right
  // length). No grader in this build hears the learner. Say so, always.
  speak_repeat: 'recording check (no speech recognition)',
  read_aloud: 'recording check (no speech recognition)',
  speak_answer: 'recording check (no speech recognition)',
}

/** How an answer was judged — shown so a learner knows what the check can see. */
export function describeGrader(value: string): string {
  return graders[value] ?? value
}

export function formatPercent(score: number): string {
  return `${Math.round(score * 100)}%`
}

export function formatDay(iso: string): string {
  const date = new Date(`${iso}T00:00:00`)
  return date.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
}

export function relativeDue(iso: string): string {
  const then = new Date(iso).getTime()
  const diff = then - Date.now()
  const hours = Math.round(Math.abs(diff) / 3_600_000)
  if (diff <= 0) return 'due now'
  if (hours < 24) return `in ${hours}h`
  const days = Math.round(hours / 24)
  return `in ${days}d`
}
