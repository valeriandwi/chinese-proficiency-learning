#!/usr/bin/env bash
# HTTP smoke test for the hanpath API.
#
# This exercises the live server over real HTTP (not an in-process handler), and
# covers the things that break quietly: session cookies surviving a reload, CSRF
# enforcement on writes, strict request decoding, the exam-mode solution guard,
# and server-side enforcement of the lock state.
#
# Correct-answer grading and the derived-progress maths are covered by
# integration/api_flow_test.go, which reads the answer keys from the database.
#
# Usage: BASE_URL=http://127.0.0.1:8081 ./scripts/smoke.sh
#
# QUOTING RULE for this file: request bodies are built with printf into a
# variable and passed as -d "$body". Never inline a body as -d "{\"k\":\"v\"}".
# A body that loses its outer quoting is brace-expanded by bash: the request is
# silently split into several curl arguments, the status code captured is the
# wrong one, and check()'s positional parameters shift so a failing assertion
# reports success. check() now refuses to run with the wrong argument count, which
# is how that class of bug is caught rather than trusted.
set -uo pipefail

BASE="${BASE_URL:-http://127.0.0.1:8081}"
JAR="$(mktemp -t hanpath-smoke-cookies.XXXXXX)"
EMAIL="smoke-$(date +%s)-$$@example.com"
PASS=0
FAIL=0
SKIP=0

pass() { printf '  \033[32m✓\033[0m %s\n' "$1"; PASS=$((PASS + 1)); }
fail() { printf '  \033[31m✗\033[0m %s\n' "$1"; FAIL=$((FAIL + 1)); }
skip() { printf '  \033[33m–\033[0m %s\n' "$1"; SKIP=$((SKIP + 1)); }

# check <name> <actual> <expected>
check() {
  if [ "$#" -ne 3 ]; then
    fail "$1 — INTERNAL: check() needs 3 arguments but got $# (a request body was split by the shell)"
    return 1
  fi
  if [ "$2" = "$3" ]; then
    pass "$1 ($2)"
  else
    fail "$1 — expected $3, got $2"
  fi
}

# check_json is check() with an explicit failure when the response could not be
# parsed: an absent field must never be compared as if it were a value.
check_json() {
  if [ "$2" = "PARSE_ERROR" ] || [ -z "$2" ]; then
    fail "$1 — could not read that value from the response (got: ${2:-empty})"
    return 1
  fi
  check "$1" "$2" "$3"
}

# json <dotted.path> reads JSON on stdin; numeric components index arrays.
json() { python3 -c '
import sys, json
d = json.load(sys.stdin)
for part in sys.argv[1].split("."):
    d = d[int(part)] if part.lstrip("-").isdigit() else d[part]
print(d)' "$1" 2>/dev/null || echo "PARSE_ERROR"; }

# attempt_payload <exercise_id> <client_ref> <option_id> [mode]
attempt_payload() {
  printf '{"mode":"%s","attempts":[{"exercise_id":"%s","client_ref":"%s","response":{"option_id":"%s"},"duration_ms":3000}]}' \
    "${4:-practice}" "$1" "$2" "$3"
}

echo "hanpath smoke test against $BASE"
rm -f "$JAR"

# --- health ---------------------------------------------------------------
check "GET /healthz" "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")" "200"
check "GET /readyz (migrations applied)" "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/readyz")" "200"

# --- anonymous access is refused ------------------------------------------
check "GET /progress/overview without a session" \
  "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/progress/overview")" "401"

# --- registration ---------------------------------------------------------
register_body=$(printf '{"email":"%s","display_name":"Smoke Tester","password":"correct horse battery","timezone":"Asia/Jakarta","daily_goal_minutes":1}' "$EMAIL")
body=$(curl -s -w '\n%{http_code}' -c "$JAR" -X POST "$BASE/api/v1/auth/register" \
  -H 'Content-Type: application/json' -d "$register_body")
register_status=$(printf '%s' "$body" | tail -1)
body=$(printf '%s' "$body" | sed '$d')
check "POST /auth/register status" "$register_status" "201"
uid=$(printf '%s' "$body" | json user.id)
case "$uid" in
  ????????-????-????-????-????????????) pass "POST /auth/register returned a user ($uid)" ;;
  *) fail "POST /auth/register did not return a user id (got: $uid; body: $body)" ;;
esac
csrf=$(awk '/hanpath_csrf/ {print $7}' "$JAR")
[ -n "$csrf" ] && pass "CSRF cookie issued" || fail "no CSRF cookie in jar"
grep -q 'hanpath_session' "$JAR" && pass "session cookie stored" || fail "no session cookie in jar"

if curl -s -b "$JAR" "$BASE/api/v1/auth/session" | grep -q '"authenticated":true'; then
  pass "session resolves on a fresh request (page-reload case)"
else
  fail "session did not resolve on a fresh request"
fi

# --- transactional email actually reaches the mail server ------------------
# Registration returning 201 proves nothing about delivery: the send happens after
# the account row is written, so a rejected envelope leaves the learner with no
# verification link and a successful response. Assert on the mail server's record.
# (This is the check that caught "501 5.5.4 invalid FROM parameter".)
MAILPIT="${MAILPIT_URL:-http://127.0.0.1:8025}"
if curl -s -o /dev/null --max-time 2 "$MAILPIT/api/v1/messages"; then
  sleep 1
  delivered=$(curl -s --max-time 5 "$MAILPIT/api/v1/messages?limit=50" | python3 -c '
import sys, json
want = sys.argv[1].lower()
try:
    data = json.load(sys.stdin)
except Exception:
    print("PARSE_ERROR"); raise SystemExit
count = 0
for message in data.get("messages") or []:
    for recipient in message.get("To") or []:
        if (recipient.get("Address") or "").lower() == want:
            count += 1
print(count)' "$EMAIL" 2>/dev/null || echo "PARSE_ERROR")
  check_json "the verification email reached the mail server" "$delivered" "1"
else
  skip "no mail server at $MAILPIT — set MAILPIT_URL to assert email delivery"
fi

# --- rejection paths ------------------------------------------------------
placement_body='{"version":"hsk2.0","known_lexeme_ids":[],"presented":10}'
check "write without the CSRF header" \
  "$(curl -s -b "$JAR" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/me/placement" \
     -H 'Content-Type: application/json' -d "$placement_body")" "403"

unknown_field_body='{"display_name":"x","role":"admin"}'
check "write with an unknown field (strict decode)" \
  "$(curl -s -b "$JAR" -o /dev/null -w '%{http_code}' -X PATCH "$BASE/api/v1/me" \
     -H 'Content-Type: application/json' -H "X-CSRF-Token: $csrf" -d "$unknown_field_body")" "400"

login_body=$(printf '{"email":"%s","password":"wrong password"}' "$EMAIL")
check "login with a wrong password" \
  "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/auth/login" \
     -H 'Content-Type: application/json' -d "$login_body")" "401"

# --- curriculum and path --------------------------------------------------
body=$(curl -s -b "$JAR" "$BASE/api/v1/curriculum")
check_json "GET /curriculum version" "$(printf '%s' "$body" | json curricula.0.version)" "hsk2.0"

body=$(curl -s -b "$JAR" "$BASE/api/v1/curriculum/hsk2.0/path")
states=$(printf '%s' "$body" | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(','.join(l['state'] for lv in d['levels'] for u in lv['units'] for l in u['lessons']))" 2>/dev/null)
lesson_id=$(printf '%s' "$body" | json levels.0.units.0.lessons.0.id)
locked_id=$(printf '%s' "$body" | json levels.0.units.0.lessons.1.id)
# The rule, not a count: on day one the first lesson is startable and nothing else
# is. Pinning the number of lessons broke the moment levels 2-5 were imported, which
# says nothing about whether locking still works.
first_state=$(printf '%s' "$states" | cut -d, -f1)
available_count=$(printf '%s' "$states" | tr ',' '\n' | grep -c '^available$' || true)
locked_count=$(printf '%s' "$states" | tr ',' '\n' | grep -c '^locked$' || true)
lesson_total=$(printf '%s' "$states" | tr ',' '\n' | grep -c . || true)
if [ "$first_state" = "available" ] && [ "$available_count" -eq 1 ] && [ "$locked_count" -eq $((lesson_total - 1)) ]; then
  pass "path lock state on day one: exactly the first of $lesson_total lessons is available"
else
  fail "path lock state on day one: first=$first_state available=$available_count locked=$locked_count total=$lesson_total"
fi

# --- lesson payload must not leak answer keys -----------------------------
lesson=$(curl -s -b "$JAR" "$BASE/api/v1/lessons/$lesson_id")
if printf '%s' "$lesson" | grep -q 'answer_key'; then
  fail "GET /lessons/{id} leaked answer_key"
else
  pass "GET /lessons/{id} contains no answer_key"
fi
exercise_id=$(printf '%s' "$lesson" | json exercises.0.id)

# --- server-side lock enforcement ----------------------------------------
locked_ex=$(curl -s -b "$JAR" "$BASE/api/v1/lessons/$locked_id" | json exercises.0.id)
locked_body=$(attempt_payload "$locked_ex" "smoke-locked" "a")
check "submitting to a locked lesson is refused" \
  "$(curl -s -b "$JAR" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/lessons/$locked_id/attempts" \
     -H 'Content-Type: application/json' -H "X-CSRF-Token: $csrf" -d "$locked_body")" "403"

# --- attempt submission ---------------------------------------------------
# A deliberately wrong answer for the first question (its key is option "a").
wrong_body=$(attempt_payload "$exercise_id" "smoke-1" "b")
body=$(curl -s -b "$JAR" -X POST "$BASE/api/v1/lessons/$lesson_id/attempts" \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $csrf" -d "$wrong_body")
check_json "wrong MCQ answer graded as incorrect" "$(printf '%s' "$body" | json results.0.is_correct)" "False"
check_json "the grader reports which answer was correct" "$(printf '%s' "$body" | json results.0.feedback.correct_id)" "a"

# Idempotent replay: the same client_ref must not create a second attempt.
body=$(curl -s -b "$JAR" -X POST "$BASE/api/v1/lessons/$lesson_id/attempts" \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $csrf" -d "$wrong_body")
check_json "replayed client_ref is idempotent" "$(printf '%s' "$body" | json results.0.duplicate)" "True"

# A client_ref reused for a *different* exercise is a conflict (409), not a silent
# replay. That needs two unlocked lessons, so it lives in the Go integration test
# (integration/level_flow_test.go) rather than here — asserting it against a locked
# lesson would only re-test the lock.

# Exam mode withholds the solution even when the answer is correct.
exam_body=$(attempt_payload "$exercise_id" "smoke-exam-1" "a" exam)
body=$(curl -s -b "$JAR" -X POST "$BASE/api/v1/lessons/$lesson_id/attempts" \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $csrf" -d "$exam_body")
if printf '%s' "$body" | grep -q '"solution"'; then
  fail "exam mode leaked the solution"
else
  pass "exam mode withholds the solution"
fi

# An exercise from a different lesson must be refused.
other_ex=$(curl -s -b "$JAR" "$BASE/api/v1/lessons/$locked_id" | json exercises.0.id)
cross_body=$(attempt_payload "$other_ex" "smoke-cross" "a")
check "exercise from another lesson is refused" \
  "$(curl -s -b "$JAR" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/lessons/$lesson_id/attempts" \
     -H 'Content-Type: application/json' -H "X-CSRF-Token: $csrf" -d "$cross_body")" "403"

# --- derived state --------------------------------------------------------
body=$(curl -s -b "$JAR" "$BASE/api/v1/progress/overview")
check_json "attempts recorded (replay deduplicated)" "$(printf '%s' "$body" | json total_attempts)" "2"
check_json "streak is 0 while the daily goal is unmet" "$(printf '%s' "$body" | json streak.current_streak)" "0"
check_json "skill mastery derived from attempts" "$(printf '%s' "$body" | json skills.0.skill)" "vocabulary"

body=$(curl -s -b "$JAR" -X POST "$BASE/api/v1/progress/refresh" -H "X-CSRF-Token: $csrf")
check_json "POST /progress/refresh recomputes" "$(printf '%s' "$body" | json total_attempts)" "2"

body=$(curl -s -b "$JAR" "$BASE/api/v1/reviews/queue")
check_json "review queue is empty before a lesson is completed" "$(printf '%s' "$body" | json due_total)" "0"

# --- listening and speaking ------------------------------------------------
# Reference audio is served by the API on the app origin; the lesson payload carries
# a playable URL for audio items and never the transcript (that is part of the
# answer key, revealed only after grading).
pick() { # pick <lesson json> <type> -> exercise id, or NONE
  printf '%s' "$1" | python3 -c "
import sys, json
d = json.load(sys.stdin)
t = sys.argv[1]
print(next((e['id'] for e in d['exercises'] if e['type'] == t), 'NONE'))" "$2" 2>/dev/null || echo PARSE_ERROR
}

dict_ex=$(pick "$lesson" listening_dictation)
speak_ex=$(pick "$lesson" speak_repeat)
hanzi_ex=$(pick "$lesson" hanzi_trace)
listening_url=$(printf '%s' "$lesson" | python3 -c "
import sys, json
d = json.load(sys.stdin)
urls = [e['prompt'].get('audio_url') for e in d['exercises'] if e['prompt'].get('audio_url')]
print(urls[0] if urls else 'NONE')" 2>/dev/null || echo PARSE_ERROR)

case "$listening_url" in
  /media/*.mp3) pass "lesson: a listening item exposes a playable same-origin url ($listening_url)" ;;
  *) fail "lesson: no playable audio url in the payload (got: $listening_url)" ;;
esac
if printf '%s' "$lesson" | grep -q 'transcript'; then
  fail "lesson payload leaked a listening transcript before the attempt"
else
  pass "lesson payload contains no listening transcript"
fi

# The asset itself: bytes, content type, and an immutable cache header that is
# only safe because the key is a hash of the clip's own bytes.
audio_headers=$(curl -s -D - -o /tmp/hanpath-smoke-audio.bin "$BASE$listening_url")
check "GET /media/{key} status" "$(printf '%s' "$audio_headers" | head -1 | awk '{print $2}')" "200"
check "GET /media/{key} content type" \
  "$(printf '%s' "$audio_headers" | grep -i '^content-type:' | tr -d '\r' | awk '{print $2}')" "audio/mpeg"
check "GET /media/{key} is cacheable forever (content-hash key)" \
  "$(printf '%s' "$audio_headers" | grep -ci 'immutable')" "1"
audio_bytes=$(wc -c < /tmp/hanpath-smoke-audio.bin | tr -d ' ')
if [ "$audio_bytes" -gt 2000 ]; then
  pass "GET /media/{key} returned $audio_bytes bytes of audio"
else
  fail "GET /media/{key} returned only $audio_bytes bytes; that is not a spoken sentence"
fi
rm -f /tmp/hanpath-smoke-audio.bin
check "GET /media/{key} with a missing asset" "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/media/does-not-exist.mp3")" "404"
# A key is generated internally, so a path in it must never reach the filesystem.
check "GET /media/{key} refuses a traversal attempt" "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/media/..%2f..%2f.env")" "404"

# Listening is mechanically graded, like any other comparison to an answer key.
dict_body=$(printf '{"mode":"practice","attempts":[{"exercise_id":"%s","client_ref":"smoke-dict","response":{"text":"谢谢你。"},"duration_ms":4000}]}' "$dict_ex")
dict_res=$(curl -s -b "$JAR" -X POST "$BASE/api/v1/lessons/$lesson_id/attempts" \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $csrf" -d "$dict_body")
check_json "listening: a correct transcription is graded correct" "$(printf '%s' "$dict_res" | json results.0.is_correct)" "True"
check_json "listening: the transcript is revealed after answering" \
  "$(printf '%s' "$dict_res" | json results.0.feedback.transcript)" "谢谢你。"

# Speaking is never asserted correct: it reports what could be checked.
speak_body=$(printf '{"mode":"practice","attempts":[{"exercise_id":"%s","client_ref":"smoke-speak","response":{"duration_ms":2600,"self_rating":4},"duration_ms":2600}]}' "$speak_ex")
speak_res=$(curl -s -b "$JAR" -X POST "$BASE/api/v1/lessons/$lesson_id/attempts" \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $csrf" -d "$speak_body")
check_json "speaking: is_correct is null, never true" "$(printf '%s' "$speak_res" | json results.0.is_correct)" "None"
check_json "speaking: the result is flagged for review" "$(printf '%s' "$speak_res" | json results.0.needs_review)" "True"
check_json "speaking: the response states no speech recognition was used" \
  "$(printf '%s' "$speak_res" | json results.0.feedback.has_speech_recognition)" "False"

# A payload the exercise type cannot read is the caller's fault. This is the class
# of bug that made every writing submission a 500 while the API tests stayed green.
bad_body=$(printf '{"mode":"practice","attempts":[{"exercise_id":"%s","client_ref":"smoke-bad","response":{"character":"\u4f60","stroke_count":7,"strokes":[],"completed":false,"hints_used":0,"grade_me":true},"duration_ms":1000}]}' "$hanzi_ex")
check "an unreadable payload is a 422, not a 500" \
  "$(curl -s -b "$JAR" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/lessons/$lesson_id/attempts" \
     -H 'Content-Type: application/json' -H "X-CSRF-Token: $csrf" -d "$bad_body")" "422"

# --- logout ---------------------------------------------------------------
check "POST /auth/logout" \
  "$(curl -s -b "$JAR" -c "$JAR" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/auth/logout" -H "X-CSRF-Token: $csrf")" "204"
check "session is dead after logout" \
  "$(curl -s -b "$JAR" -o /dev/null -w '%{http_code}' "$BASE/api/v1/me")" "401"

rm -f "$JAR"
echo
echo "passed: $PASS   failed: $FAIL   skipped: $SKIP"
[ "$FAIL" -eq 0 ]
