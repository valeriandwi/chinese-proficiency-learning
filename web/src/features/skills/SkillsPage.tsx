import { Link } from 'react-router-dom'
import { useOverview, useSession } from '../../api/hooks'
import { Chip, ErrorNote, Skeleton } from '../../components/ui'
import { ReadDoorArt, SpeakDoorArt } from './door-art'
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

      {/* One card anatomy for every door: art, name, one-line job, a real fact, and a button
          pinned to the bottom. The production skills get the larger row. */}
      <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-6">
        <SkillDoor
          to="/write"
          title="Write"
          blurb="Stroke order on a tianzige grid — trace, then from memory."
          art={<span className="hanzi text-6xl text-brand-700">写</span>}
          artClass="bg-brand-50"
          mastery={skills.find((s) => s.skill === 'writing')}
          cta="Open studio"
          large
        />
        <SkillDoor
          to="/speak"
          title="Speak"
          blurb="Repeat the model and play both back to back. Recordings never leave the browser."
          art={<SpeakDoorArt className="h-full w-full" />}
          artClass="bg-info-50 text-info-700"
          mastery={skills.find((s) => s.skill === 'speaking')}
          cta="Open studio"
          large
        />
        <SkillDoor
          to="/read"
          title="Read"
          blurb="Passages with pinyin and translation on tap."
          art={<ReadDoorArt className="h-full w-full" />}
          artClass="bg-reward-50 text-reward-700"
          mastery={skills.find((s) => s.skill === 'reading')}
          cta="Open"
        />
        <SkillDoor
          to="/tones"
          title="Tones"
          blurb="Hear it, pick the tone pattern."
          art={<span className="hanzi text-4xl text-brand-700">调</span>}
          artClass="bg-brand-50"
          mastery={undefined}
          cta="Open"
        />
        <SkillDoor
          to="/guide"
          title="Study guide"
          blurb="Every word, grammar notes, your bookmarks."
          art={<span className="hanzi text-4xl text-ink-2">学</span>}
          artClass="bg-surface-2"
          mastery={undefined}
          cta="Open"
        />
      </div>

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
  blurb,
  art,
  artClass,
  mastery,
  cta,
  large = false,
}: {
  to: string
  title: string
  blurb: string
  art: React.ReactNode
  artClass: string
  mastery: { attempts: number; correct: number; mastery: number } | undefined
  cta: string
  large?: boolean
}) {
  // Absence is not failure: a skill with no attempts is "not yet practised",
  // never 0% — a 0/N bar reads as a verdict on ability rather than missing data.
  const notPractised = !mastery || mastery.attempts === 0

  return (
    <Link
      to={to}
      className={`flex flex-col gap-2.5 rounded-card border-2 border-b-4 border-line bg-surface p-5 transition hover:border-line-strong focus-visible:outline-2 ${
        large ? 'sm:col-span-3' : 'sm:col-span-2'
      }`}
    >
      <div
        className={`grid place-items-center overflow-hidden rounded-tile p-3 ${large ? 'h-28' : 'h-20'} ${artClass}`}
        aria-hidden
      >
        {art}
      </div>
      <h2 className={`font-extrabold text-ink ${large ? 'text-xl' : 'text-lg'}`}>{title}</h2>
      <p className="text-sm text-ink-3">{blurb}</p>
      {mastery !== undefined ? (
        notPractised ? (
          <Chip tone="neutral">Not practised yet</Chip>
        ) : (
          <Chip tone="positive">
            {mastery.correct}/{mastery.attempts} on the path
          </Chip>
        )
      ) : null}
      <span className="mt-auto inline-flex min-h-12 items-center gap-2 self-start rounded-card border-2 border-b-4 border-line bg-surface px-5 text-base font-extrabold text-ink-2">
        {cta} <span aria-hidden>→</span>
      </span>
    </Link>
  )
}

/** Exported for the hub's own tests: the level choices it renders. */
export { LEVEL_CHOICES as SKILL_LEVEL_CHOICES }
