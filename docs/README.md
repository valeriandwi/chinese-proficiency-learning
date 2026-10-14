# hanpath

A full-stack web app for learning Mandarin and preparing for the HSK exams:
structured lessons by level, practice in reading / listening / writing / speaking,
spaced repetition, streaks, and (in progress) mock exams.

**Status: M0–M2 are playable end to end.** Accounts, the HSK 2.0 levels 1–5 path
(2,387 words, 8,125 drills, 392 lessons at levels 2–5 generated from the published
word lists),
practice lessons with **fourteen** exercise types (reading, listening and speaking
included), reference audio generated at import time and served same-origin,
server-side grading, derived progress, streaks and the review deck all work against
real Postgres, covered by Go unit tests, Go integration tests, an HTTP smoke test
and a browser end-to-end test. Hanzi writing is practised in-app but graded from the
client's stroke report, speaking is deliberately not judged, and mock exams are
designed but not built — `docs/roadmap.md` says exactly what is and is not shipped.

The interface is Duolingo-inspired by design, not by imitation: a lesson **trail**
with four distinguishable node states, a thumb-reachable action bar and bottom
navigation on a phone, a finish screen that states what was earned in words and
numbers, and a design system whose contrast ratios are *computed by a test* rather
than eyeballed. A component names a role (`brand`, `success`, `reward`, `danger`),
never a hue, and a stray colour class fails the build — see `docs/architecture.md`,
"The interface is a system, not a stylesheet". Icons and typeface are self-hosted;
nothing is fetched from a third-party CDN at runtime.

---

## Requirements

Go (the toolchain pinned in `go.mod`), Node + pnpm, and Postgres 15 or 16 — install
it with `brew install postgresql@16`. **No container runtime is needed:**
`make dev-db-up` creates and starts an isolated local Postgres cluster in
`.local/pgdata` on port 5435 (binaries are picked from Homebrew, falling back to
whatever `initdb` is on `PATH`). Redis and a mail server are optional and unused
here — the rate limiter runs in-process, and mail prints to the log unless you set
`SMTP_ADDR` (e.g. `brew install mailpit`).

## Quick start

```bash
make dev            # .env, local Postgres cluster, migrations, content import
make run-api        # API on http://127.0.0.1:8081
make web-install    # SPA dependencies + stroke-order data
make web-dev        # SPA on http://localhost:5174
make run-worker     # optional: async jobs
```

Open **http://localhost:5174** — not the API port. The SPA and the API must be the
same origin, which is what the Vite proxy provides in development; pointing the
SPA at `127.0.0.1:8081` directly would make the session cookie third-party and it
would be silently dropped.

Everything else:

```bash
make help           # every target, with descriptions
```

## Layout

```
cmd/api             HTTP server
cmd/worker          background jobs (session pruning, progress recomputation)
cmd/contentctl      validate / import / migrate / list curriculum content
internal/config     environment loading with fail-fast validation
internal/httpapi    routing, middleware, handlers, error envelope
internal/auth       argon2id passwords, session + CSRF tokens, rate limiting
internal/store      all SQL, plus the derived-progress recompute path
internal/exercises  the exercise registry: per-type validation and grading
internal/progress   streak maths, interval ladder, lock-state derivation
internal/curriculum authoring format: parse, validate, idempotent import
migrations          goose migrations, embedded in the binaries
content/            authored HSK curriculum as YAML
openapi/            the API contract
integration/        end-to-end Go tests against a real database
scripts/            HTTP smoke test and browser end-to-end test
web/                React + TypeScript SPA
docs/               architecture, licensing, roadmap
```

## The five ideas worth knowing

1. **Attempts are the source of truth; everything else is derived.** `attempts` is
   an append-only event log. Lesson progress, XP, skill mastery, daily rollups,
   streaks and the review deck are rebuilt from it by one recompute path
   (`store.RecomputeProgress`), called inside the submission transaction and again
   by `POST /progress/refresh`. A stale number is therefore always one call away
   from being correct.
2. **Curriculum is data, not code.** A curriculum version, its levels, units,
   lessons, vocabulary and exercises live in YAML under `content/`, validated at
   import time and upserted on natural keys. HSK 3.0 — three stages, nine levels —
   is an import, not a release. Re-importing never deletes an exercise, because a
   delete would cascade away every learner attempt against it.
3. **The server grades; the client renders.** Nine exercise types are validated and
   graded by one registry. Answer keys are structurally absent from lesson
   payloads, exam mode withholds solutions, and a free-form answer is rubric-checked
   and flagged `needs_review` rather than declared correct — no keyword check can
   judge grammar or style, and the API says so in its own response.
4. **Time is the learner's, not the server's.** Streaks, daily goals and activity
   rollups are bucketed by the learner's IANA timezone, and the streak walker
   requires calendar-adjacent days — a gap breaks the run even if those days are
   simply absent from the data.
5. **Idempotent by construction.** Every attempt carries a `client_ref`: a retried
   batch is a no-op, and reusing a reference for a *different* exercise is a `409`
   instead of a silently swallowed answer.

## Testing

```bash
make test              # Go unit tests, no database needed
make test-integration  # Go end-to-end tests against Postgres
make smoke             # HTTP contract test against a running API
make web-test          # SPA unit and component tests (Vitest + Testing Library)
node scripts/e2e-smoke.mjs   # real browser end-to-end, needs the API and dev server up
```

The browser test drives a headless Chrome over the DevTools Protocol: it walks
register → dashboard → path → a nine-exercise lesson → results → back, then
**measures the shell** at six desktop widths (including a Retina Mac at 2×) and on
an emulated 375×812 phone, asserting no horizontal overflow, the progress rail
inside the viewport, connectors centred on their nodes, one fixed bottom
navigation and ≥44px targets. Overflow failures name the offending element.

Two opt-ins, both inside the gitignored `.local/`:

- every run leaves a transcript at `.local/e2e-last.log`, so a failure that
  scrolls past your terminal is still diagnosable;
- `HANPATH_E2E_SHOTS=1` also writes screenshots of each screen to
  `.local/e2e-shots/`, to look at the interface the run just proved is functional.

The integration suite is gated on `HANPATH_TEST_DATABASE_URL`; `make
test-integration` sets it. A bare `go test ./...` skips it and proves nothing about
grading or progress, so use the target.

## Configuration

All configuration is environment-based; see `.env.example`. `internal/config`
refuses to start on a missing database URL, a `Secure` cookie over plain `http`, or
a production environment without a secure cookie. Secrets are never logged.

## Licensing and third-party data

Stroke-order data, vocabulary placement and any derived definitions each carry
their own obligations. Those are recorded per source, with the clause that matters,
in `docs/sources.md`. Attribution required by the Arphic Public License ships in
the app footer and next to the data itself.
