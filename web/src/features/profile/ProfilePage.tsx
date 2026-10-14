import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useCurricula, useLogout, useOverview, useSession, useUpdateMe } from '../../api/hooks'
import { describeGoal } from '../../lib/labels'
import { Button, Card, Chip, ErrorNote } from '../../components/ui'
import { usePrefs } from '../../stores/prefs'

const timezones = [
  'Asia/Jakarta',
  'Asia/Singapore',
  'Asia/Shanghai',
  'Asia/Tokyo',
  'Europe/London',
  'America/New_York',
  'Australia/Sydney',
  'UTC',
]

export function ProfilePage() {
  const session = useSession()
  const overview = useOverview()
  const curricula = useCurricula()
  const update = useUpdateMe()
  const logout = useLogout()
  const prefs = usePrefs()
  const [saved, setSaved] = useState(false)

  const user = session.data?.authenticated ? session.data.user : undefined
  if (!user) return <ErrorNote>Not signed in.</ErrorNote>

  const [displayName, setDisplayName] = useState(user.display_name)
  const [timezone, setTimezone] = useState(user.timezone)
  const [goalKind, setGoalKind] = useState(user.daily_goal_kind)
  const [goalValue, setGoalValue] = useState(user.daily_goal_value)
  const [pinyinDefault, setPinyinDefault] = useState(user.pinyin_default)
  const [targetLevel, setTargetLevel] = useState(user.target_level ?? 1)

  const save = async () => {
    setSaved(false)
    await update.mutateAsync({
      display_name: displayName,
      timezone,
      daily_goal_kind: goalKind,
      daily_goal_value: goalValue,
      pinyin_default: pinyinDefault,
      target_level: targetLevel,
    })
    setSaved(true)
  }

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-4">
      <h1 className="text-2xl font-semibold">Settings</h1>

      <Card className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-2">
          <Chip tone="accent">{user.role}</Chip>
          <Chip tone={user.email_verified_at ? 'positive' : 'warning'}>
            {user.email_verified_at ? 'email verified' : 'email not verified'}
          </Chip>
          <Chip>{user.email}</Chip>
        </div>

        <label className="flex flex-col gap-1">
          <span className="text-sm font-medium text-ink-2">Display name</span>
          <input className="input" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </label>

        <label className="flex flex-col gap-1">
          <span className="text-sm font-medium text-ink-2">
            Timezone — this decides which day your streak counts
          </span>
          <select className="input" value={timezone} onChange={(e) => setTimezone(e.target.value)}>
            {[...new Set([timezone, ...timezones])].map((tz) => (
              <option key={tz} value={tz}>
                {tz}
              </option>
            ))}
          </select>
        </label>

        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-sm font-medium text-ink-2">Daily goal measured in</span>
            <select className="input" value={goalKind} onChange={(e) => setGoalKind(e.target.value as never)}>
              <option value="minutes">minutes</option>
              <option value="xp">XP</option>
              <option value="lessons">lessons</option>
            </select>
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-sm font-medium text-ink-2">Target per day</span>
            <input
              className="input"
              type="number"
              min={1}
              max={600}
              value={goalValue}
              onChange={(e) => setGoalValue(Number(e.target.value))}
            />
          </label>
        </div>

        <label className="flex flex-col gap-1">
          <span className="text-sm font-medium text-ink-2">Pinyin by default</span>
          <select className="input" value={pinyinDefault} onChange={(e) => setPinyinDefault(e.target.value as never)}>
            <option value="always">always shown</option>
            <option value="on_tap">only when tapped</option>
            <option value="never">never</option>
          </select>
        </label>

        <label className="flex flex-col gap-1">
          <span className="text-sm font-medium text-ink-2">Target HSK level</span>
          <select className="input" value={targetLevel} onChange={(e) => setTargetLevel(Number(e.target.value))}>
            {Array.from({ length: 9 }, (_, i) => i + 1).map((level) => (
              <option key={level} value={level}>
                HSK {level}
              </option>
            ))}
          </select>
        </label>

        {update.error ? <ErrorNote>{(update.error as Error).message}</ErrorNote> : null}
        <div className="flex items-center gap-3">
          <Button onClick={save} disabled={update.isPending}>
            {update.isPending ? 'Saving…' : 'Save'}
          </Button>
          {saved ? <span className="text-sm text-brand-700">Saved — your rollups were recomputed.</span> : null}
        </div>
      </Card>

      <Card className="flex flex-col gap-3">
        <h2 className="font-semibold">Learner-facing options</h2>
        <div className="flex items-center justify-between">
          <span className="text-sm text-ink-2">Show pinyin in lessons (this session)</span>
          <Button variant="secondary" onClick={prefs.togglePinyin}>
            {prefs.pinyinVisible ? 'On' : 'Off'}
          </Button>
        </div>
        <Link to="/placement" className="text-sm font-semibold text-brand-700">
          Retake the placement check →
        </Link>
      </Card>

      <Card className="flex flex-col gap-2">
        <h2 className="font-semibold">Curricula available</h2>
        {curricula.data?.curricula.map((c) => (
          <div key={c.id} className="flex flex-wrap items-center justify-between gap-2 text-sm">
            <span>
              <strong>{c.version}</strong> · {c.standard}
            </span>
            <span className="text-ink-3">
              {c.level_count} levels · {c.lesson_count} lessons · {c.exercise_count} exercises · {c.status}
            </span>
          </div>
        ))}
        <p className="text-xs text-ink-3">
          A curriculum version is data, not code: importing HSK 3.0&apos;s nine levels adds a version here without a
          deployment.
        </p>
      </Card>

      <Card className="flex items-center justify-between">
        <div>
          <h2 className="font-semibold">Signed in as {user.display_name}</h2>
          <p className="text-sm text-ink-2">
            {overview.data ? describeGoal(overview.data.streak.goal_kind, overview.data.streak.goal_value) : ''}
          </p>
        </div>
        <Button
          variant="secondary"
          onClick={async () => {
            await logout.mutateAsync()
          }}
        >
          Sign out
        </Button>
      </Card>
    </div>
  )
}
