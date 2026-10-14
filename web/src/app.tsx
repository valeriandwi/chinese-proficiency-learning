import { Navigate, NavLink, Outlet, Route, Routes, useLocation } from 'react-router-dom'
import type { ReactNode } from 'react'
import { AudioLines, BookOpenCheck, Home, Languages, LogOut, Map, Settings, type LucideIcon } from 'lucide-react'
import { useLogout, useOverview, useSession } from './api/hooks'
import { Button, IconButton, Spinner } from './components/ui'
import { RightRail } from './components/RightRail'
import { usePrefs } from './stores/prefs'
import { LoginPage, RegisterPage } from './features/auth/AuthPages'
import { DashboardPage } from './features/dashboard/DashboardPage'
import { PathPage } from './features/path/PathPage'
import { TonePage } from './features/tone/TonePage'
import { LessonPage } from './features/lesson/LessonPage'
import { ReviewPage } from './features/review/ReviewPage'
import { ProfilePage } from './features/profile/ProfilePage'
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
        <Route
          path="/"
          element={
            <RequireAuth authenticated={authenticated}>
              <DashboardPage />
            </RequireAuth>
          }
        />
        <Route
          path="/path"
          element={
            <RequireAuth authenticated={authenticated}>
              <PathPage />
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
type Destination = { to: string; label: string; icon: LucideIcon; end?: boolean }

const DESTINATIONS: Destination[] = [
  { to: '/', label: 'Today', icon: Home, end: true },
  { to: '/path', label: 'Path', icon: Map },
  { to: '/reviews', label: 'Reviews', icon: BookOpenCheck },
  { to: '/tones', label: 'Tones', icon: AudioLines },
  { to: '/settings', label: 'Settings', icon: Settings },
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
  const logout = useLogout()
  const overview = useOverview()
  const signOut = () => void logout.mutateAsync()
  const due = overview.data?.due_reviews ?? 0

  return (
    <div className="min-h-screen bg-paper">
      {/* 6xl left the middle column 544px wide, which cramps the trail against a
          288px rail on a laptop and leaves a 1152px strip on a 1920 screen. 7xl gives
          the content room without stretching prose to an unreadable measure. */}
      <div className="mx-auto flex w-full max-w-7xl gap-6 px-4 lg:px-6">
        <Sidebar due={due} pinyinOn={prefs.pinyinVisible} onTogglePinyin={prefs.togglePinyin} onSignOut={signOut} />

        <div className="flex min-w-0 flex-1 flex-col">
          <MobileBar pinyinOn={prefs.pinyinVisible} onTogglePinyin={prefs.togglePinyin} onSignOut={signOut} />
          {/* pb-24 keeps the last control clear of the fixed bottom bar. */}
          <main className="flex-1 pb-24 pt-6 lg:pb-10 lg:pt-8">
            <Outlet />
          </main>
          <LicenceFooter className="hidden pb-8 lg:block" />
        </div>

        <RightRail />
      </div>

      <BottomNav due={due} />
    </div>
  )
}

function Brand() {
  return (
    <NavLink to="/" className="flex items-baseline gap-2">
      <span aria-hidden className="hanzi text-2xl font-bold text-brand-700">
        汉语
      </span>
      <span className="text-sm font-extrabold tracking-wide text-ink">hanpath</span>
    </NavLink>
  )
}

function Sidebar({
  due,
  pinyinOn,
  onTogglePinyin,
  onSignOut,
}: {
  due: number
  pinyinOn: boolean
  onTogglePinyin: () => void
  onSignOut: () => void
}) {
  return (
    // A nav, not an aside: this is the primary set of destinations on a wide screen.
    <nav className="hidden w-56 shrink-0 flex-col gap-1 py-6 lg:flex" aria-label="Main">
      <div className="px-3 pb-4">
        <Brand />
      </div>
      {DESTINATIONS.map((destination) => (
        <SidebarLink key={destination.to} destination={destination} badge={destination.to === '/reviews' ? due : 0} />
      ))}

      <div className="mt-auto flex flex-col gap-2 pt-6">
        <PinyinToggle on={pinyinOn} onToggle={onTogglePinyin} />
        <Button variant="ghost" onClick={onSignOut} className="justify-start">
          <LogOut size={16} aria-hidden /> Sign out
        </Button>
      </div>
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
        `flex items-center gap-3 rounded-tile px-3 py-2.5 text-sm font-bold transition ${
          isActive ? 'bg-brand-50 text-brand-700' : 'text-ink-2 hover:bg-surface-2 hover:text-ink'
        }`
      }
    >
      <Icon size={20} aria-hidden />
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
function MobileBar({
  pinyinOn,
  onTogglePinyin,
  onSignOut,
}: {
  pinyinOn: boolean
  onTogglePinyin: () => void
  onSignOut: () => void
}) {
  return (
    <header className="sticky top-0 z-20 flex items-center justify-between gap-3 border-b border-line bg-paper/90 px-1 py-3 backdrop-blur lg:hidden">
      <Brand />
      <div className="flex items-center gap-2">
        <PinyinToggle on={pinyinOn} onToggle={onTogglePinyin} />
        <IconButton label="Sign out" onClick={onSignOut}>
          <LogOut size={18} aria-hidden />
        </IconButton>
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
      className="fixed inset-x-0 bottom-0 z-20 border-t border-line bg-surface pb-[env(safe-area-inset-bottom)] shadow-raised lg:hidden"
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
                  `relative flex flex-col items-center gap-1 px-2 py-2.5 text-[0.7rem] font-bold transition ${
                    isActive ? 'text-brand-700' : 'text-ink-2'
                  }`
                }
              >
                <span className="relative">
                  <Icon size={22} aria-hidden />
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
      className={`inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs font-bold transition ${
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
