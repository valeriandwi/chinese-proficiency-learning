import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useLogout, useOverview, useSession, useUpdateMe } from '../../api/hooks'
import { describeGoal } from '../../lib/labels'
import { Button, Card, ErrorNote } from '../../components/ui'
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
      <div className="flex items-center gap-4">
        <span
          aria-hidden
          className="grid h-14 w-14 shrink-0 place-items-center rounded-card bg-brand-400 text-2xl font-extrabold text-ink"
        >
          {user.display_name.trim().charAt(0).toUpperCase()}
        </span>
        <div className="min-w-0">
          <h1 className="truncate text-2xl font-extrabold tracking-tight text-ink">{user.display_name}</h1>
          <p className="truncate text-sm text-ink-2">
            {user.email} · {user.email_verified_at ? 'email verified' : 'email not verified'}
          </p>
        </div>
      </div>

      <Card className="flex flex-col gap-4" aria-labelledby="learning-h">
        <h2 id="learning-h" className="text-lg font-extrabold text-ink">
          Learning
        </h2>

        <label className="flex flex-col gap-1">
          <span className="text-sm font-bold text-ink-2">Display name</span>
          <input className="input" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </label>

        <fieldset className="flex flex-col gap-2">
          <legend className="text-sm font-bold text-ink-2">Daily goal</legend>
          <div className="grid grid-cols-2 gap-3">
            <select
              aria-label="Daily goal measured in"
              className="input"
              value={goalKind}
              onChange={(e) => setGoalKind(e.target.value as never)}
            >
              <option value="minutes">minutes</option>
              <option value="xp">XP</option>
              <option value="lessons">lessons</option>
            </select>
            <input
              aria-label="Target per day"
              className="input"
              type="number"
              min={1}
              max={600}
              value={goalValue}
              onChange={(e) => setGoalValue(Number(e.target.value))}
            />
          </div>
          <p className="text-xs text-ink-3">Your streak counts the days you meet this.</p>
        </fieldset>

        <label className="flex flex-col gap-1">
          <span className="text-sm font-bold text-ink-2">Target HSK level</span>
          <select className="input" value={targetLevel} onChange={(e) => setTargetLevel(Number(e.target.value))}>
            {Array.from({ length: 9 }, (_, i) => i + 1).map((level) => (
              <option key={level} value={level}>
                HSK {level}
              </option>
            ))}
          </select>
        </label>

        <label className="flex flex-col gap-1">
          <span className="text-sm font-bold text-ink-2">Time zone — this decides where your day ends</span>
          <select className="input" value={timezone} onChange={(e) => setTimezone(e.target.value)}>
            {[...new Set([timezone, ...timezones])].map((tz) => (
              <option key={tz} value={tz}>
                {tz}
              </option>
            ))}
          </select>
        </label>

        <label className="flex flex-col gap-1">
          <span className="text-sm font-bold text-ink-2">Pinyin by default</span>
          <select className="input" value={pinyinDefault} onChange={(e) => setPinyinDefault(e.target.value as never)}>
            <option value="always">always shown</option>
            <option value="on_tap">only when tapped</option>
            <option value="never">never</option>
          </select>
        </label>

        {update.error ? <ErrorNote>{(update.error as Error).message}</ErrorNote> : null}
        <div className="flex items-center gap-3">
          <Button onClick={save} disabled={update.isPending}>
            {update.isPending ? 'Saving…' : 'Save'}
          </Button>
          {saved ? (
            <span role="status" className="text-sm font-bold text-brand-700">
              Saved — your rollups were recomputed.
            </span>
          ) : null}
        </div>
      </Card>

      <Card className="flex flex-col gap-3" aria-labelledby="display-h">
        <h2 id="display-h" className="text-lg font-extrabold text-ink">
          Display &amp; sound
        </h2>
        <div className="flex items-center justify-between gap-3">
          <span className="text-sm text-ink-2">Show pinyin in lessons (this session)</span>
          <Button variant="secondary" onClick={prefs.togglePinyin} aria-pressed={prefs.pinyinVisible}>
            {prefs.pinyinVisible ? 'On' : 'Off'}
          </Button>
        </div>
        <div className="flex items-center justify-between gap-3">
          <span className="text-sm text-ink-2">Sound effects (音效)</span>
          <Button variant="secondary" onClick={prefs.toggleSound} aria-pressed={prefs.soundEnabled}>
            {prefs.soundEnabled ? 'On' : 'Off'}
          </Button>
        </div>
      </Card>

      <Card className="flex flex-col gap-3" aria-labelledby="account-h">
        <h2 id="account-h" className="text-lg font-extrabold text-ink">
          Account
        </h2>
        <p className="text-sm text-ink-2">
          {overview.data ? describeGoal(overview.data.streak.goal_kind, overview.data.streak.goal_value) : ''}
        </p>
        <div className="flex flex-wrap gap-2">
          <Link
            to="/placement"
            className="inline-flex items-center rounded-tile border-2 border-line bg-surface px-5 py-3 text-sm font-extrabold tracking-wide text-ink transition hover:bg-surface-2"
          >
            Retake placement
          </Link>
          <Button
            variant="ghost"
            onClick={async () => {
              await logout.mutateAsync()
            }}
          >
            Sign out
          </Button>
        </div>
      </Card>
    </div>
  )
}
