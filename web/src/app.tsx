import { Navigate, NavLink, Outlet, Route, Routes, useLocation } from 'react-router-dom'
import type { ReactNode } from 'react'
import { Languages } from 'lucide-react'
import type { ReactElement, SVGProps } from 'react'
import { LearnGlyph, ReviewsGlyph, SettingsGlyph, SkillsGlyph, TonesGlyph } from './components/icons'
import { MascotMark } from './components/Mascot'
import { useOverview, useSession } from './api/hooks'
import { Spinner } from './components/ui'
import { GamifiedHeader } from './components/GamifiedHeader'
import { usePrefs } from './stores/prefs'
import { LoginPage, RegisterPage } from './features/auth/AuthPages'
import { PathPage } from './features/path/PathPage'
import { ProgressPage } from './features/progress/ProgressPage'
import { ProficiencyPage } from './features/proficiency/ProficiencyPage'
import { PaperRoute } from './features/proficiency/PaperPage'
import { TonePage } from './features/tone/TonePage'
import { LessonPage } from './features/lesson/LessonPage'
import { ReviewPage } from './features/review/ReviewPage'
import { ProfilePage } from './features/profile/ProfilePage'
import { SkillsPage } from './features/skills/SkillsPage'
import { ReadPage } from './features/read/ReadPage'
import { WritePage } from './features/write/WritePage'
import { SpeakPage } from './features/speak/SpeakPage'
import { GuidePage } from './features/guide/GuidePage'
import { PlacementPage } from './features/profile/PlacementPage'

export function App() {
  const session = useSession()

  if (session.isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Spinner label="Starting hanpath" />
      </div>
    )
  }

  const authenticated = session.data?.authenticated === true

  return (
    <Routes>
      <Route element={<Layout authenticated={authenticated} />}>
        <Route path="/login" element={authenticated ? <Navigate to="/" replace /> : <LoginPage />} />
        <Route path="/register" element={authenticated ? <Navigate to="/" replace /> : <RegisterPage />} />
        {/* The home screen IS the path: a learner opens the app to be told what to do
            next, not to read a dashboard. /path stays as a redirect so old links work. */}
        <Route
          path="/"
          element={
            <RequireAuth authenticated={authenticated}>
              <PathPage />
            </RequireAuth>
          }
        />
        <Route path="/path" element={<Navigate to="/" replace />} />
        {/* The skills hub and the four studios it leads to. These pages shipped in
            ef75a84 with no route, so they could not be opened at all; each one is a
            real surface with server-graded drills, so each gets a URL. */}
        <Route
          path="/skills"
          element={
            <RequireAuth authenticated={authenticated}>
              <SkillsPage />
            </RequireAuth>
          }
        />
        <Route
          path="/read"
          element={
            <RequireAuth authenticated={authenticated}>
              <ReadPage />
            </RequireAuth>
          }
        />
        <Route
          path="/write"
          element={
            <RequireAuth authenticated={authenticated}>
              <WritePage />
            </RequireAuth>
          }
        />
        <Route
          path="/speak"
          element={
            <RequireAuth authenticated={authenticated}>
              <SpeakPage />
            </RequireAuth>
          }
        />
        <Route
          path="/guide"
          element={
            <RequireAuth authenticated={authenticated}>
              <GuidePage />
            </RequireAuth>
          }
        />
        <Route
          path="/progress"
          element={
            <RequireAuth authenticated={authenticated}>
              <ProgressPage />
            </RequireAuth>
          }
        />
        <Route
          path="/lessons/:id"
          element={
            <RequireAuth authenticated={authenticated}>
              <LessonPage />
            </RequireAuth>
          }
        />
        <Route
          path="/reviews"
          element={
            <RequireAuth authenticated={authenticated}>
              <ReviewPage />
            </RequireAuth>
          }
        />
        <Route
          path="/tones"
          element={
            <RequireAuth authenticated={authenticated}>
              <TonePage />
            </RequireAuth>
          }
        />
        {/* Test out: a scored paper that opens a level. Not a nav destination — a
            learner meets it where the decision is made (a level header, or progress). */}
        <Route
          path="/proficiency"
          element={
            <RequireAuth authenticated={authenticated}>
              <ProficiencyPage />
            </RequireAuth>
          }
        />
        <Route
          path="/proficiency/:level"
          element={
            <RequireAuth authenticated={authenticated}>
              <PaperRoute />
            </RequireAuth>
          }
        />
        <Route
          path="/placement"
          element={
            <RequireAuth authenticated={authenticated}>
              <PlacementPage />
            </RequireAuth>
          }
        />
        <Route
          path="/settings"
          element={
            <RequireAuth authenticated={authenticated}>
              <ProfilePage />
            </RequireAuth>
          }
        />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

/** One definition of the navigation, rendered twice: a labelled sidebar on a wide
 *  screen, a thumb-reachable bottom bar on a phone. Adding a destination is one line. */
type Destination = { to: string; label: string; icon: (props: SVGProps<SVGSVGElement>) => ReactElement; end?: boolean }

// Five destinations. "Today" and "Path" were two labels for one screen, so neither needs a
// tab; Progress earns one because it was only reachable through links inside other pages;
// Tones is an off-path drill, so it lives in Practice with the other studios.
// The icons are hanpath's own (components/icons.tsx) — filled and chunky, so the chrome
// reads as pressable rather than as a library default.
const DESTINATIONS: Destination[] = [
  { to: '/', label: 'Learn', icon: LearnGlyph, end: true },
  // Practice is the skills hub: the door to the reading, writing and speaking studios
  // and the study guide. Without this the studio pages exist but nothing reaches them.
  { to: '/skills', label: 'Practice', icon: SkillsGlyph },
  { to: '/reviews', label: 'Reviews', icon: ReviewsGlyph },
  // The bars glyph reads as "progress" here; the tone drills no longer have a tab.
  { to: '/progress', label: 'Progress', icon: TonesGlyph },
  { to: '/settings', label: 'Profile', icon: SettingsGlyph },
]

function RequireAuth({ authenticated, children }: { authenticated: boolean; children: ReactNode }) {
  const location = useLocation()
  if (!authenticated) return <Navigate to="/login" replace state={{ from: location.pathname }} />
  return <>{children}</>
}

function Layout({ authenticated }: { authenticated: boolean }) {
  if (!authenticated) return <PublicShell />
  return <LearnerShell />
}

/** Signed out: one centred column, no navigation to get lost in. */
function PublicShell() {
  return (
    <div className="flex min-h-screen flex-col bg-paper">
      <header className="flex items-center justify-between px-5 py-4">
        <Brand />
        <nav className="flex items-center gap-1 text-sm">
          <PublicLink to="/login">Sign in</PublicLink>
          <PublicLink to="/register">Create account</PublicLink>
        </nav>
      </header>
      <main className="mx-auto w-full max-w-md flex-1 px-5 py-8">
        <Outlet />
      </main>
      <LicenceFooter className="mx-auto w-full max-w-md px-5 pb-8" />
    </div>
  )
}

/** Signed in: sidebar + content + progress rail on desktop, bottom bar on mobile. */
function LearnerShell() {
  const prefs = usePrefs()
  const overview = useOverview()
  const due = overview.data?.due_reviews ?? 0
  // A lesson is a focus screen: no navigation, no numbers strip, nothing to leave through
  // by accident. The lesson page carries its own way out.
  const focus = useLocation().pathname.startsWith('/lessons/')

  if (focus) {
    return (
      <div className="min-h-screen bg-paper">
        <main className="mx-auto w-full max-w-2xl px-4 pb-10 pt-4">
          <Outlet />
        </main>
        <LicenceFooter className="mx-auto w-full max-w-2xl px-4 pb-8" />
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-paper">
      {/* 6xl left the middle column 544px wide, which cramps the trail against a
          288px rail on a laptop and leaves a 1152px strip on a 1920 screen. 7xl gives
          the content room without stretching prose to an unreadable measure. */}
      {/* No rail any more, so the content column no longer needs a 7xl row to hold it. */}
      <div className="mx-auto flex w-full max-w-5xl gap-6 px-4 lg:px-6">
        <Sidebar due={due} />

        <div className="flex min-w-0 flex-1 flex-col">
          <TopBar pinyinOn={prefs.pinyinVisible} onTogglePinyin={prefs.togglePinyin} />
          {/* pb-24 keeps the last control clear of the fixed bottom bar. */}
          <main className="flex-1 pb-24 pt-4 lg:pb-10 lg:pt-0">
            <Outlet />
          </main>
          <LicenceFooter className="hidden pb-8 lg:block" />
        </div>
      </div>

      <BottomNav due={due} />
    </div>
  )
}

function Brand() {
  return (
    <NavLink to="/" className="flex items-center gap-2.5">
      <MascotMark size={40} />
      <span className="flex items-center gap-2.5 leading-none text-ink">
        <span className="text-[1.3rem] font-bold tracking-[0.01em]">hanpath</span>
        <span aria-hidden className="h-5 w-px bg-current opacity-25" />
        <span aria-hidden className="hanzi text-[0.55rem] font-medium tracking-[0.2em] opacity-70">
          汉路
        </span>
      </span>
    </NavLink>
  )
}

function Sidebar({ due }: { due: number }) {
  return (
    // A nav, not an aside: this is the primary set of destinations on a wide screen.
    <nav className="hidden w-56 shrink-0 flex-col gap-1 py-6 lg:flex" aria-label="Main">
      <div className="px-3 pb-4">
        <Brand />
      </div>
      {DESTINATIONS.map((destination) => (
        <SidebarLink key={destination.to} destination={destination} badge={destination.to === '/reviews' ? due : 0} />
      ))}
    </nav>
  )
}

function SidebarLink({ destination, badge }: { destination: Destination; badge: number }) {
  const Icon = destination.icon
  return (
    <NavLink
      to={destination.to}
      end={destination.end}
      className={({ isActive }) =>
        `flex min-h-12 items-center gap-3 rounded-tile border-2 px-3 text-sm font-extrabold uppercase tracking-widest transition ${
          isActive
            ? 'border-info-200 bg-info-50 text-info-700'
            : 'border-transparent text-ink-2 hover:bg-surface-2'
        }`
      }
    >
      <Icon className="h-5 w-5" aria-hidden />
      {destination.label}
      {badge > 0 ? <span className="ml-auto rounded-full bg-danger-100 px-2 py-0.5 text-xs font-extrabold text-danger-700">{badge}</span> : null}
    </NavLink>
  )
}

function PublicLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) =>
        `rounded-tile px-3 py-1.5 font-bold transition ${isActive ? 'bg-brand-50 text-brand-700' : 'text-ink-2 hover:bg-surface-2 hover:text-ink'}`
      }
    >
      {children}
    </NavLink>
  )
}

/** The phone header: identity, plus the two things a learner reaches for mid-lesson. */
/** The one place the numbers appear, in one element at every width. On a phone: the level on
 *  the left, streak / XP / goal ring on the right. On a desktop: the pinyin toggle on the
 *  left. Sound and sign out live in Profile. */
function TopBar({ pinyinOn, onTogglePinyin }: { pinyinOn: boolean; onTogglePinyin: () => void }) {
  const session = useSession()
  const level = session.data?.authenticated ? session.data.user.target_level : null
  return (
    <header className="sticky top-0 z-20 -mx-4 flex items-center gap-2.5 border-b-2 border-line bg-surface px-4 py-3 lg:static lg:mx-0 lg:mb-5 lg:bg-paper lg:px-0 lg:pt-6">
      <NavLink
        to="/settings"
        className="inline-flex min-h-10 items-center rounded-card border-2 border-line bg-surface px-3 text-sm font-extrabold text-ink-2 lg:hidden"
      >
        {level ? `HSK ${level}` : 'Level'}
      </NavLink>
      <div className="mr-auto max-lg:hidden">
        <PinyinToggle on={pinyinOn} onToggle={onTogglePinyin} />
      </div>
      <div className="ml-auto lg:ml-0">
        <GamifiedHeader />
      </div>
    </header>
  )
}

/** Bottom navigation: every primary destination within thumb reach, with the
 *  review count visible without opening anything. */
function BottomNav({ due }: { due: number }) {
  return (
    <nav
      aria-label="Main"
      className="fixed inset-x-0 bottom-0 z-20 border-t-2 border-line bg-surface pb-[env(safe-area-inset-bottom)] shadow-raised lg:hidden"
    >
      <ul className="mx-auto flex max-w-md">
        {DESTINATIONS.map((destination) => {
          const Icon = destination.icon
          return (
            <li key={destination.to} className="flex-1">
              <NavLink
                to={destination.to}
                end={destination.end}
                className={({ isActive }) =>
                  `relative flex flex-col items-center gap-1 px-2 py-2 text-[0.7rem] font-extrabold transition ${
                    isActive
                      ? 'bg-brand-600 text-white shadow-[0_3px_0_var(--color-brand-700)]'
                      : 'text-ink-4 hover:text-ink-2'
                  }`
                }
              >
                <span className="relative">
                  <Icon className="h-[22px] w-[22px]" aria-hidden />
                  {destination.to === '/reviews' && due > 0 ? (
                    <span className="absolute -right-2 -top-1.5 grid h-4 min-w-4 place-items-center rounded-full bg-danger-600 px-1 text-[0.6rem] font-extrabold text-white">
                      {due}
                    </span>
                  ) : null}
                </span>
                {destination.label}
              </NavLink>
            </li>
          )
        })}
      </ul>
    </nav>
  )
}

function PinyinToggle({ on, onToggle }: { on: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-pressed={on}
      title="Toggle pinyin across the app"
      className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-3 py-1.5 text-xs font-bold transition ${
        on ? 'border-brand-500 bg-brand-50 text-brand-700' : 'border-line-strong bg-surface text-ink-2 hover:text-ink'
      }`}
    >
      <Languages size={14} aria-hidden />
      拼音 {on ? 'on' : 'off'}
    </button>
  )
}


/** The licence notice is a legal obligation, not decoration, so it stays in the DOM
 *  on every screen — including the phone, where the sidebar does not exist. */
function LicenceFooter({ className = '' }: { className?: string }) {
  return (
    <footer className={`text-xs text-ink-3 ${className}`}>
      <p>
        Stroke-order data from the Make Me a Hanzi project, used under the Arphic Public License; the licence text ships
        with the data at <code>/hanzi/LICENSE.txt</code>. Vocabulary placement follows the published HSK word lists.
      </p>
    </footer>
  )
}
