# Architecture and the decisions behind it

## Shape

One Go module, three binaries, one Postgres database, one SPA.

```
browser ──SPA (React/TS, same origin)──► Caddy (prod) / Vite proxy (dev) ──► cmd/api ──► Postgres
                                                                              │        └── (Redis: cache, distributed rate limiting)
                                                                              └── jobs table ──► cmd/worker
cmd/contentctl: author YAML ──► validate ──► idempotent upsert ──► Postgres
```

It is a modular monolith on purpose. Every seam that would become a service
boundary is already a package boundary (`auth`, `curriculum`, `attempts`,
`progress`, `exercises`), and the only genuinely asynchronous work — grading
free-form answers, generating audio, sending reminders — is already behind a
durable job table. Nothing requires a network hop to be correct today.

## Decisions

### Chi over Gin

Handlers are `http.HandlerFunc`, so they are testable with `httptest` without a
framework context object, and middleware is portable stdlib. More importantly, we
own request decoding: strict decoding (`DisallowUnknownFields`) and one centralised
error mapper are exactly what a versioned contract needs, and a framework's binding
magic hides that layer. Gin's advantages — batteries-included binding, `swaggo`,
a larger ecosystem — are real but were traded for standard-library compatibility
and swappability. See `internal/httpapi/server.go` for the error envelope and
`decodeJSON` for the strict decoder.

### Session cookies over JWT

An SPA in a browser cannot revoke a JWT in localStorage and is exposed to XSS
reading it. The session is an opaque token in an HttpOnly, SameSite=Lax cookie,
stored server-side as a SHA-256 hash; logout revokes it and a password reset
revokes every session. Writes additionally require a double-submit CSRF header,
and the token is accepted **only** as a header — accepting the readable cookie as
a fallback would reduce the check to a cookie-value comparison that cookie tossing
defeats.

The trade-off accepted here: the SPA and the API must be same-origin. That is why
`web/.env` must not set an absolute API base URL; the Vite proxy exists so that
`localhost:5174` is both. `localhost` and `127.0.0.1` are different cookie sites,
and mixing them silently drops the session.

### Attempts are the event log

`attempts` is append-only: user, exercise, response, correctness, score, grader,
duration, mode. Everything a learner sees on the dashboard is derived from it:

| Derived value | Where it is computed | When |
|---|---|---|
| `lesson_progress` (status, best score, completion) | SQL over attempts | every submission, in-transaction |
| `xp_events` | one award per completed lesson, deduplicated by `(user, source, ref_id)` | same |
| `skill_mastery` | mean score per skill, including partial credit | same |
| `daily_activity` | bucketed by the learner's IANA timezone | same |
| `user_streaks` | Go walk over goal-met days (pure, unit-tested) | same |
| `review_items` | deck seeded from completed lessons' vocabulary | same |

Because it is one code path, `POST /progress/refresh` is a legitimate answer to
"these numbers look stale", and there is no incremental-update bug surface.

### Curriculum as versioned data

Content is a build artifact of a generator where generation is safe. `cmd/contentgen`
derives HSK levels 2–5 from the published word lists into the same schema levels 1 is
authored in — one file per level, each carrying a header copied from level 1 so
importing one file cannot rewrite another's metadata (`make content-gen`, then
`make content-validate content-import`). It emits only drills that follow from a word
without judgement, and validates every one of them through the same registry that
grades it, in `internal/curriculum/generated_content_test.go` — a check that runs in
seconds without a database, because the full import synthesises audio for every
listening item.

A curriculum version owns levels → units → lessons → exercises. The importer
upserts on natural keys and identifies exercises by a stable `content_key`, which
matters more than it looks: `attempts.exercise_id` is `ON DELETE CASCADE`, so an
import that deleted and re-inserted exercises would quietly destroy learner
history. Exercises removed from the file are *reported*, never deleted.

Content is validated before it is written: every payload is checked by the
exercise registry, vocabulary references must resolve, blank markers must exist,
answer keys must reference real options. A mistake is an import failure, not an
ungradeable exercise in front of a learner.

### The exercise registry is the extension seam

One interface per type: `Validate` (import time) and `Grade` (request time).
Grading never trusts the client; an unrecognised option id is a wrong answer, not
a 500. `Result.IsCorrect` is a `*bool`, and `nil` means "not mechanically
gradable" — free-form writing returns a rubric score, `needs_review: true` and a
`limitation` string that the UI displays verbatim. Adding a type means one Go file
and one React renderer; no migration, no pipeline change.

### Locked, not deleted

A lesson is `locked`, `available`, `in_progress` or `completed`, derived from path
position plus the learner's completed set (`progress.DeriveLessonStates`, a pure
function with unit tests). The API enforces the lock on submission, so a
hand-crafted request cannot skip ahead, while reading a locked lesson is allowed
so the UI can preview it.

### Time

Streaks, goals and activity are bucketed by the learner's IANA timezone, resolved
by Postgres (`now() AT TIME ZONE timezone`) so the answer does not depend on the Go
binary's tzdata. The streak walker requires calendar-adjacent days and tolerates an
unfinished today. A learner who changes timezone gets every daily bucket
recomputed immediately, because otherwise their streak would describe a timezone
they no longer live in.

### Failure and idempotency

- Every attempt carries a `client_ref`. Replaying one is a no-op; reusing one for a
  different exercise is `409`.
- `POST /lessons/{id}/attempts` persists attempts and recomputes derived state in
  a single transaction: a submission cannot leave progress stale.
- Migrations take a Postgres advisory lock, so two processes starting at once
  cannot race, and `/readyz` reports pending migrations so a binary/schema
  mismatch is visible instead of mysterious.
- The worker claims jobs with `SELECT ... FOR UPDATE SKIP LOCKED`; a crash mid-job
  leaves the row reclaimable rather than lost.

## Development topology

| Component | Port | Note |
|---|---|---|
| API | 8081 | deliberately not 8080 |
| SPA (Vite) | 5174 | proxies `/api` to the API |
| Postgres | 5435 | separate from any other local cluster |
| Redis | 6380 | provisioned; used for caching and distributed rate limiting in M2 |
| Postgres (data) | .local/pgdata | local cluster, not a container |

Object storage is still absent, and deliberately so: reference audio is generated
at import time and served by the API from `MEDIA_DIR` (see `internal/media`), which
needs no cloud credentials and keeps the SPA same-origin. `media.Store` is the seam
— an S3 implementation is a new type, not a change to any caller — and the
`S3_*` variables stay unread until one exists, rather than being wired to a stub
that hides the fact that nothing consumes them.

## The interface is a system, not a stylesheet

The redesign that made this app feel like a game rather than a textbook is held
together by one rule: **a component names a role, never a hue.** `brand` is what
the learner acts on, `success` what they completed, `reward` what they earned
(XP, streak, the daily goal), `danger` a mistake, `ink` text, `neutral` surfaces.
Those names live once, in `web/src/index.css` under `@theme`, and everything else
refers to them.

Two things make the rule hold rather than hope:

- **Contrast is measured, not asserted.** `src/lib/design-tokens.test.ts` reads the
  stylesheet and computes WCAG ratios for every documented text pair (≥ 4.5:1),
  every white-on-colour button, and every meaning-carrying fill and boundary
  (≥ 3:1). The palette this replaced failed in five places without anyone noticing
  — `text-ink/40` measured 2.47:1 — which is why text now uses a named ramp
  (`ink`, `ink-2`, `ink-3`, `ink-4`) and an opacity modifier on text is itself a
  test failure.
- **A stray hue is a build failure.** The same file scans every component for raw
  Tailwind colour classes. Fills use the 600/700 shades because they sit on the
  pale `track`, not on white, where a 500 shade measures 2.1:1.

**Layout.** One definition of the navigation (`DESTINATIONS` in `src/app.tsx`) is
rendered twice: a labelled sidebar and a right-hand progress rail on a wide screen,
a fixed bottom bar within thumb reach on a phone. The rail reads the same
`GET /progress/overview` query as Today through one cache key, so it costs no extra
request and cannot disagree with the page beside it.

**Gamification is real or absent.** Streak, XP, daily goal, skill mastery, activity
and the review-due count are all fields the API already returns. Hearts, gems,
leagues, badges and crowns would need new server work, so they are not drawn:
a fake progress bar is a lie the learner eventually catches.

**Motion** is two durations and one curve, and `prefers-reduced-motion` neutralises
animation globally — the feedback still happens, it just stops moving. The
celebration screen states the result in words and numbers, so it survives having no
animation at all.

**Copy is a contract.** Roughly twenty user-visible strings are pinned by
`scripts/e2e-smoke.mjs` and the component tests ("Finish lesson", "N / 9",
"unlock in order", "checked by exact match"). A redesign that renames one must
update both suites in the same change.
