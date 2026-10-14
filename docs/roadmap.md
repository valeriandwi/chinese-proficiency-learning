# Roadmap and current status

Honest accounting: what is finished, what is designed but not built, and what is
deliberately out of scope. Milestones are sequenced so that the risky, expensive
work (content, audio, pronunciation scoring) comes after the engine that consumes
it is proven.

## Where the project is now — M0 done, M1 core done

**Shipped and verified**

| Area | State |
|---|---|
| Repo, local Postgres dev cluster, migrations, embedded schema, CI | done |
| Config with fail-fast validation, structured logs, request ids, graceful shutdown | done |
| Accounts: register / login / logout, argon2id, sessions, CSRF, rate limiting | done |
| Email verification and password reset tokens (single use, hashed at rest) | done |
| Profile and preferences: timezone, daily goal, pinyin default, target level | done |
| Placement: vocabulary self-check → recommended level | done (self-report, not a scored test) |
| Curriculum as data: parse → validate → idempotent import (`contentctl`) | done |
| HSK 2.0 Level 1 authored: 3 units, 6 lessons, 46 lexemes, 49 exercises, 1 reading passage | done |
| HSK 2.0 levels 2–5 generated from the published word lists: 392 lessons, 2,341 words, 8,076 drills (`cmd/contentgen`) | done (drills derived from the words; no generated sentences) |
| Exercise registry: 14 types, validated at import, graded on the server | done |
| Reference audio: TTS at import time, content-hash keys, served same-origin at `/media/{key}` | done (licence for end-user serving open — see docs/sources.md) |
| Listening drills: audio MCQ + dictation, graded against an answer key, transcript withheld until an attempt exists | done |
| Speaking drills: repeat / read aloud / answer, with recording, playback, self-rating | done (tier 1 — nothing judges pronunciation) |
| Lesson player: per-type renderers, pinyin toggle, exam-mode switch, batch submit | done |
| Interface: semantic design tokens with measured contrast, lesson trail, phone bottom navigation, celebration screen | done (see docs/architecture.md) |
| Hanzi writing: guided trace + write-from-memory, stroke errors recorded | done (client-side stroke check) |
| Progress: lesson state, XP, skill mastery, daily rollup, streaks, review deck | done |
| Spaced repetition: interval ladder, deck seeded from completed lessons | done |
| Worker: Postgres-backed job queue, session pruning, progress recomputation | done |
| Tests: Go unit, Go integration against Postgres, HTTP smoke, browser E2E, SPA unit | done |
| OpenAPI contract, licence inventory, this roadmap | done |

**Verified by running, not by inspection**

- Go unit tests: grading for all fourteen types — including the assertions that
  speaking never returns a verdict and that a listening transcript is structurally
  absent from a prompt — plus streak walks, interval ladder, lock-state derivation,
  placement mapping, audio key stability, and idempotent synthesis.
- Go integration tests against real Postgres: content import is idempotent; a
  learner walks the entire level with correct answers read from the database;
  XP, review deck, streak and daily rollup match; a replayed batch neither
  double-counts attempts nor XP; exam mode withholds solutions.
- Browser end-to-end (real headless Chrome against the real API): 40 checks plus
  one honestly-skipped capability. It drives the whole first-run path through the
  listening drills — the clip is fetched over the app origin and actually plays, the
  transcript is absent before answering and present after — and through the speaking
  drill's no-microphone path. The record path is covered by component tests with a
  fake `MediaRecorder` and by the HTTP tests, because Chrome on this machine
  enumerates capture devices but never settles `getUserMedia`, and the run says so
  instead of pretending.
- HTTP smoke test: session survives a reload, CSRF is required, strict request
  decoding, locked lessons refuse submissions, cross-lesson answers refuse.

## Not shipped yet

### M2 — Reading and listening in the product
**Audio and listening are shipped** (see the table above): the pipeline generates and
stores assets at import time behind a `TTSProvider` interface, and listening items
are graded like any other answer-key comparison. Two things remain:

- **A production TTS provider.** The shipped provider is macOS `say`, which needs no
  credentials and is what makes the pipeline testable — but the terms for serving
  its output to end users are Apple's, not ours, and that decision is recorded as
  open in `docs/sources.md`. Object storage (S3/R2) is the other swap: `media.Store`
  is the seam, and nothing above it changes.
- **Reading**: timed practice vs exam mode across a wider passage set, and tone
  identification as a third listening type.

### M3 — Writing composition, speaking, mock exams
- **Composition**: word/phrase ordering, fill-in, recall and HSK-style prompts.
  The rubric grader exists and is honest about its limits; an LLM-assisted scorer
  goes behind the same interface with a confidence value and a review flag.
- **Speaking**: **tier 1 is shipped** — reference audio, recording, playback for
  self-comparison, and a self-rating, with the score derived only from what can be
  checked (a recording exists and is of a believable length). Recordings never leave
  the browser, so there is no learner-voice storage. Tier 2 — real pronunciation
  scoring via per-phone alignment — is a **paid, audio-egress decision** with a
  privacy policy attached, and stays gated until explicitly approved.
- **Mock exams**: examination blueprints per level and version, server-enforced
  timing, asynchronous grading of writing and speaking, per-section results with
  an estimated band clearly labelled as indicative.

### Content and curriculum
- **HSK 2.0 levels 2–6**: the engine is ready; this is authoring work, and it is
  the largest remaining cost in the project. The importer reports vocabulary that
  no lesson teaches, so gaps are visible rather than silent.
- **HSK 3.0 (three stages, nine levels)**: the standard's finalised syllabus was
  published in November 2025 with a July 2026 implementation date, a Levels 1–6
  pilot ran on 31 January 2026 (with speaking required for Levels 3–6), and
  Chinese Testing International has said regular sittings continued on HSK 2.0
  until a separate launch announcement. Because the syllabus is mid-transition,
  the app versions curricula as *data*: HSK 3.0 is a second import, and the
  stage model (`six-levels` vs `three-stages-nine-levels`) is already a column.
  Nothing in the code assumes six levels.
- **Admin content tooling**: today content is YAML plus `contentctl`. A
  review/publish UI is a separate, deliberately deferred scope decision.

### Explicitly out of scope for now
Microservices, JWT in localStorage, a self-hosted ASR model, leaderboards and
social features, an offline PWA, native apps, and a shop/currency economy.

## Known limitations, stated plainly

1. **Pronunciation is not graded** by the app. A speaking result always carries
   `is_correct: null`, `needs_review: true` and a `limitation` string naming what was
   *not* assessed; the score reflects a recording that exists and is the right
   length, so completing a speaking drill means the drill was done, not that it was
   done correctly. Tier 2 assessment is unbuilt.
2. **Free-form writing is rubric-checked** (required vocabulary, length, banned
   tokens). A valid Chinese answer can score low and an invalid one can score
   high; the response carries `needs_review` and the app says so on screen.
3. **Hanzi grading trusts the client's stroke report.** The server re-interprets
   it under fixed rules, but per-stroke geometric validation against the medians
   (resampling, direction angle, Hausdorff distance) is a hardening item, not a
   shipped feature.
4. **The lock state is enforced on submission, not on read.** A learner can open a
   locked lesson's payload to preview it; they cannot answer it.
5. **Rate limiting is per-process.** Two API instances would each allow the
   configured rate. Redis is provisioned for the distributed version.
6. **No mock-exam mode in the UI yet.** The API supports `mode: "exam"` and the
   lesson player exposes it, but blueprints and timed papers are M3.
7. **Exam mode reveals per-answer correctness but not the answer.** Submitting with
   `mode: "exam"` returns `is_correct` with `solution`, `feedback`, `explanation` and
   `score` stripped. A learner could therefore probe options and learn which was
   right without seeing why. That is pre-existing behaviour for every MCQ type, it
   matters more now that an exam can contain audio, and it should be closed when mock
   exams are built.
8. **Levels 2–5 are drilled, not taught.** They cover every word of the published
   HSK 2.0 lists for those levels with meaning, listening, dictation, speaking and
   stroke-order drills, but they contain no authored sentences: `fill_blank`,
   `sentence_order`, `translation` and `short_answer` exist only in level 1, because
   a generator asked to write Chinese would produce plausible but wrong grammar.
   Writing those items is human work. The glosses also carry an open licence
   question — see `docs/sources.md` §3.
9. **The interface shows only the gamification the server can back.** Streaks, XP,
   the daily goal, skill mastery, activity and the review-due count are real. Hearts,
   gems, leagues, badges and crowns would each need server work, so they are absent
   rather than mocked — a decorative progress bar is a promise the app cannot keep.
10. **The SPA ships as one bundle.** `pnpm build` reports ~520 kB before gzip
   (Vite's 500 kB warning) now that the icon set and the typeface are bundled. It is
   a warning, not a failure: nothing loads from a CDN, and the fonts are subsets the
   browser fetches on demand (39 kB for Latin). Route-level `import()` splitting is
   the obvious next step if the warning starts to matter.
11. **The mobile layout is verified at one size.** The E2E emulates 375×812 and
   asserts a single bottom navigation, reachable targets and the trail. Tablet widths
   and landscape phones are covered by the responsive utilities but not by a test.

12. **Email in development goes to the log.** There is no mail server in the local
   setup: with `SMTP_ADDR` unset, the log transport prints the message (including
   the verification link) rather than losing it. `brew install mailpit`, set
   `SMTP_ADDR=127.0.0.1:1025` and the smoke test's delivery assertion runs again.
