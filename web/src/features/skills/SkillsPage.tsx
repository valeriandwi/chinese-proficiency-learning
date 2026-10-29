import { Link } from 'react-router-dom'
import { useOverview, useSession } from '../../api/hooks'
import { Chip, ErrorNote, Skeleton } from '../../components/ui'
import { ReadDoorArt, SpeakDoorArt, WriteDoorArt } from './door-art'
// The skills hub: three doors into practice that the lesson path does not gate.
//
// This page is a crossroads, not a dashboard: one door per skill the studios
// cover, each stating what it is, what it practises, and the honest state of the
// thing behind it. Nothing here fabricates readiness — where a level has no
// drills the door says so on the other side, not here, and where the learner has
// no practice yet the door shows "not yet practised" rather than 0%.

type LevelChoice = { value: number; label: string }

const LEVEL_CHOICES: LevelChoice[] = [
  { value: 0, label: 'All' },
  { value: 1, label: 'HSK 1' },
  { value: 2, label: 'HSK 2' },
  { value: 3, label: 'HSK 3' },
  { value: 4, label: 'HSK 4' },
  { value: 5, label: 'HSK 5' },
]

export function SkillsPage() {
  const session = useSession()
  const overview = useOverview()
  const user = session.data?.authenticated ? session.data.user : undefined
  const targetLevel = user?.target_level ?? 1

  if (overview.error) return <ErrorNote>{(overview.error as Error).message}</ErrorNote>

  const skills = overview.data?.skills ?? []

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6">
      <header className="flex flex-col gap-2">
        <h1 className="text-3xl font-extrabold tracking-tight text-ink">
          练习 <span className="text-lg font-bold text-ink-3">practice</span>
        </h1>
        <p className="text-sm text-ink-2">
          Drill writing, speaking, reading and tones from any level. Studio practice earns no XP and moves no lesson —
          it is practice, not progress.
        </p>
      </header>

      {overview.isLoading ? (
        <div className="grid gap-4 sm:grid-cols-2">
          <Skeleton className="h-56" />
          <Skeleton className="h-56" />
        </div>
      ) : null}

      {/* One door per skill. Each door is a link, so a keyboard user can tab to
          it, and the description says what is behind it rather than selling it. */}
      <div className="grid gap-4 sm:grid-cols-2">
        <SkillDoor
          to="/write"
          title="Write"
          hanzi="写"
          blurb="A large canvas for stroke order: trace with guidance or write from memory, with replay and undo."
          art={<WriteDoorArt className="h-full w-full" />}
          mastery={skills.find((s) => s.skill === 'writing')}
          targetLevel={targetLevel}
        />
        <SkillDoor
          to="/speak"
          title="Speak"
          hanzi="说"
          blurb="Speak after the model, read aloud, and answer questions aloud — recorded in the browser, never judged for pronunciation."
          art={<SpeakDoorArt className="h-full w-full" />}
          mastery={skills.find((s) => s.skill === 'speaking')}
          targetLevel={targetLevel}
        />
      </div>

      {/* Reading and the guide: full-width doors, because reading is a passage
          and the guide is a whole book — half-width doors would cramp them. */}
      <SkillDoor
        to="/read"
        title="Read"
        hanzi="读"
        blurb="Read real passages from the lessons, sentence by sentence, with pinyin and translation on tap."
        art={<ReadDoorArt className="h-16 w-28 shrink-0" />}
        mastery={skills.find((s) => s.skill === 'reading')}
        targetLevel={targetLevel}
        wide
      />
      <SkillDoor
        to="/tones"
        title="Tones"
        hanzi="调"
        blurb="Hear a word and choose its tone pattern — the drill learners fail most, without walking the path first."
        art={<span className="hanzi grid h-16 w-16 shrink-0 place-items-center rounded-tile bg-brand-50 text-3xl font-bold text-brand-700" aria-hidden>调</span>}
        mastery={undefined}
        targetLevel={targetLevel}
        wide
      />
      <SkillDoor
        to="/guide"
        title="Study guide"
        hanzi="学"
        blurb="Every word in the curriculum with its meaning and audio, the grammar people wrote for level 1, and your bookmarks."
        art={<span className="hanzi grid h-16 w-16 shrink-0 place-items-center rounded-tile bg-brand-50 text-3xl font-bold text-brand-700" aria-hidden>学</span>}
        mastery={undefined}
        targetLevel={targetLevel}
        wide
      />

      <p className="text-center text-xs text-ink-3">
        Level {targetLevel} is the level you are working on — change it in settings. The studios let you practise from
        any level.
      </p>
    </div>
  )
}

function SkillDoor({
  to,
  title,
  hanzi,
  blurb,
  art,
  mastery,
  targetLevel,
  wide = false,
}: {
  to: string
  title: string
  hanzi: string
  blurb: string
  art: React.ReactNode
  mastery: { attempts: number; correct: number; mastery: number } | undefined
  targetLevel: number
  wide?: boolean
}) {
  // Absence is not failure: a skill with no attempts is "not yet practised",
  // never 0% — a 0/N bar reads as a verdict on ability rather than missing data.
  const notPractised = !mastery || mastery.attempts === 0

  return (
    <Link
      to={to}
      className={`group flex flex-col gap-4 rounded-card border border-line bg-surface p-5 shadow-card transition hover:border-brand-300 hover:shadow-raised focus-visible:outline-2 ${
        wide ? 'sm:flex-row sm:items-center' : ''
      }`}
    >
      {art ? (
        <div className="flex h-24 items-center justify-center rounded-tile bg-brand-50 p-2" aria-hidden>
          {art}
        </div>
      ) : (
        <span
          className="hanzi grid h-20 w-20 shrink-0 place-items-center rounded-tile bg-brand-50 text-4xl font-bold text-brand-700"
          aria-hidden
        >
          {hanzi}
        </span>
      )}
      <div className="flex min-w-0 flex-col gap-1.5">
        <h2 className="text-xl font-extrabold text-ink">{title}</h2>
        <p className="text-sm text-ink-2">{blurb}</p>
        <div className="mt-1 flex flex-wrap items-center gap-2">
          {notPractised ? (
            <Chip tone="neutral">not yet practised</Chip>
          ) : (
            <Chip tone="positive">
              {mastery!.correct}/{mastery!.attempts} on the path
            </Chip>
          )}
          <Chip tone="neutral">HSK {targetLevel} and beyond</Chip>
        </div>
      </div>
      <span className="ml-auto hidden shrink-0 items-center self-center text-sm font-bold text-brand-700 group-hover:translate-x-0.5 sm:flex" aria-hidden>
        →
      </span>
    </Link>
  )
}

/** Exported for the hub's own tests: the level choices it renders. */
export { LEVEL_CHOICES as SKILL_LEVEL_CHOICES }
