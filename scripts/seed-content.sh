#!/usr/bin/env bash
#
# Seed the hanpath curriculum into a database — the one in docker compose, or any
# DATABASE_URL you point it at.
#
# Why this script exists: `make content-import` (cmd/contentctl) resolves reference audio
# through the synthesiser, and the only provider implemented is macOS `say`
# (internal/media.NewSayProvider fails without it). So importing *inside* a Linux
# container is impossible even though the 2,375 mp3s it would look up are committed under
# var/media. This script provides the two paths that do work:
#
#   import   run contentctl against $DATABASE_URL          — needs a synthesiser (macOS)
#   dump     write the curriculum out of a seeded database  — needs pg_dump
#   restore  load that dump into the compose database       — needs docker compose
#   verify   print what the target database actually holds
#
# Usage:
#   ./scripts/seed-content.sh verify
#   ./scripts/seed-content.sh dump [--out FILE]
#   ./scripts/seed-content.sh restore [FILE] [--force]
#   ./scripts/seed-content.sh import
#
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$REPO_ROOT"

DB_SERVICE=${DB_SERVICE:-db}
DUMP_FILE=${DUMP_FILE:-hanpath-content.sql}

# The curriculum lives in these tables. Everything else in the schema is either
# per-learner (attempts, progress, sessions) or owned by a migration.
CONTENT_TABLES=(
  curricula levels units lessons
  lexemes lexeme_levels
  exercises exercise_lexemes
  passages lesson_passages
)

say() { printf '  %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# "postgres://user:secret@host:port/db" → "user@host:port/db", so the target of a dump or a
# restore is never a guess (and the secret never lands in a log).
describe_url() {
  printf '%s' "$1" | sed -E 's#^(postgres(ql)?)://([^:/@]+)(:[^@]*)?@#\1://\3@#'
}

# Rows a pg_dump COPY block carries, read straight out of the .sql so a restore can judge
# the file without a database.
count_rows() {
  awk -v tag="COPY public.$2 " '
    index($0, tag) == 1 { inside = 1; next }
    inside && $0 == "\\." { inside = 0 }
    inside { n++ }
    END { print n + 0 }
  ' "$1"
}

compose() {
  if [[ -f .env.compose ]]; then
    docker compose --env-file .env.compose "$@"
  else
    docker compose "$@"
  fi
}

# The database inside the compose network, for psql calls that run in the container.
# Storage order: the shell environment, then .env.compose, then the compose defaults.
db_env() {
  local name=${1:?} default=${2:-} value=
  value=${!name:-}
  if [[ -z $value && -f .env.compose ]]; then
    value=$(grep -E "^${name}=" .env.compose | head -1 | cut -d= -f2- || true)
  fi
  [[ -n $value ]] || value=$default
  printf '%s' "$value"
}

psql_container() {
  local user db
  user=$(db_env HANPATH_DB_USER hanpath)
  db=$(db_env HANPATH_DB_NAME hanpath)
  compose exec -T "$DB_SERVICE" psql -U "$user" -d "$db" "$@"
}

# A local DATABASE_URL, for dump and import. pg_dump needs to be on PATH.
local_dburl() {
  if [[ -n ${DATABASE_URL:-} ]]; then
    printf '%s' "$DATABASE_URL"
    return
  fi
  if [[ -f .env ]]; then
    grep -E '^DATABASE_URL=' .env | head -1 | cut -d= -f2-
    return
  fi
  die "no DATABASE_URL: export one or run this from a repo checkout with .env"
}

cmd_verify() {
  say "target: compose service '$DB_SERVICE'"
  psql_container -Atc "
    select 'curricula', count(*) from curricula
    union all select 'levels', count(*) from levels
    union all select 'units', count(*) from units
    union all select 'lessons', count(*) from lessons
    union all select 'exercises', count(*) from exercises
    union all select 'lexemes', count(*) from lexemes
    order by 1" | sed 's/^/  /'

  local lessons
  lessons=$(psql_container -Atc "select count(*) from lessons")
  if [[ $lessons -eq 0 ]]; then
    say ""
    say "0 lessons: the app will answer 404 \"unknown curriculum version\"."
    say "Seed it with: ./scripts/seed-content.sh dump && ./scripts/seed-content.sh restore"
  fi
}

server_major() { psql "$1" -Atc "select current_setting('server_version_num')::int/10000" 2>/dev/null; }

# pg_dump refuses to talk to a server of a different major version, and Homebrew happily
# puts an older one first on PATH. Pick the one that matches the server instead of letting
# the user discover it at dump time.
resolve_pg_dump() {
  local url=$1 want candidate candidates=()
  want=$(server_major "$url")

  [[ -n ${PG_DUMP:-} ]] && candidates+=("$PG_DUMP")
  if command -v brew >/dev/null 2>&1; then
    local prefix
    for candidate in 17 16 15; do
      prefix=$(brew --prefix "postgresql@$candidate" 2>/dev/null || true)
      [[ -n $prefix ]] && candidates+=("$prefix/bin/pg_dump")
    done
  fi
  candidates+=(/opt/homebrew/opt/postgresql@16/bin/pg_dump /usr/lib/postgresql/16/bin/pg_dump)
  command -v pg_dump >/dev/null 2>&1 && candidates+=("$(command -v pg_dump)")

  local major
  for candidate in "${candidates[@]}"; do
    [[ -x $candidate ]] || continue
    major=$("$candidate" --version 2>/dev/null | grep -oE '[0-9]+' | head -1)
    if [[ -n $want && $major != "$want" ]]; then
      continue
    fi
    printf '%s' "$candidate"
    return 0
  done
  return 1
}

cmd_dump() {
  local out=$DUMP_FILE
  while [[ $# -gt 0 ]]; do
    case $1 in
      --out) out=${2:?--out needs a path}; shift 2 ;;
      *) die "dump: unknown argument $1" ;;
    esac
  done

  local url pgdump tmp args=()
  url=$(local_dburl)
  pgdump=$(resolve_pg_dump "$url") || die "no pg_dump matching the server's major version
       (set PG_DUMP=/path/to/pg_dump; macOS: brew install postgresql@16)"
  for table in "${CONTENT_TABLES[@]}"; do args+=(--table="$table"); done

  # Write to a temporary file and only move it into place if the dump succeeded: a failed
  # dump otherwise leaves an empty .sql that a later `restore` would load silently.
  tmp=$(mktemp "${TMPDIR:-/tmp}/hanpath-content.XXXXXX")
  # shellcheck disable=SC2064 # expand now, not at trap time
  trap "rm -f '$tmp'" EXIT

  # Data only: the schema comes from migrations on the target, never from a dump.
  if ! "$pgdump" "$url" --data-only --no-owner --no-acl "${args[@]}" >"$tmp"; then
    die "pg_dump failed — nothing written"
  fi
  if ! grep -q '^COPY ' "$tmp"; then
    die "the dump has no COPY blocks: the source database holds no curriculum
       (import it first: ./scripts/seed-content.sh import, or make content-import)"
  fi

  mv "$tmp" "$out"
  trap - EXIT

  # Report what was captured, table by table, and refuse a dump that could only seed an
  # empty curriculum: an import that died partway leaves a curriculum row with no lessons
  # behind, and restoring that looks like success while the app still answers 404.
  local table rows lessons=0
  say "source: $(describe_url "$url")"
  for table in "${CONTENT_TABLES[@]}"; do
    rows=$(count_rows "$out" "$table")
    [[ $table == lessons ]] && lessons=$rows
    printf '  %-16s %6s\n' "$table" "$rows"
  done
  if [[ $lessons -eq 0 ]]; then
    rm -f "$out"
    die "that database has no lessons: the dump was deleted rather than shipped.

       Run this on the machine that already holds the curriculum (your Mac, after
       'make content-import' there), then restore the file here:

         ./scripts/seed-content.sh dump        # on the Mac
         scp hanpath-content.sql vps:/tmp/
         ./scripts/seed-content.sh restore /tmp/hanpath-content.sql   # on the server

       Importing on a Linux server cannot work while the only synthesiser is macOS 'say'."
  fi

  say "wrote $out ($(wc -c <"$out" | tr -d ' ') bytes) using $("$pgdump" --version)"
  say "restore it on the server with: ./scripts/seed-content.sh restore $out"
}

cmd_restore() {
  local file=$DUMP_FILE force=0
  while [[ $# -gt 0 ]]; do
    case $1 in
      --force) force=1; shift ;;
      -*) die "restore: unknown flag $1" ;;
      *) file=$1; shift ;;
    esac
  done
  [[ -f $file ]] || die "restore: no such file $file"

  # Judge the file before touching the target: a dump without lessons seeds nothing usable,
  # and would otherwise look like a successful restore.
  local file_lessons
  file_lessons=$(count_rows "$file" lessons)
  if [[ $file_lessons -eq 0 ]]; then
    die "restore: $file contains no lessons — nothing to load.
       Re-dump from a database that holds the full curriculum, or import there first."
  fi
  say "file: $file ($file_lessons lessons)"

  local existing
  existing=$(psql_container -Atc "select count(*) from curricula")
  if [[ $existing -gt 0 && $force -eq 0 ]]; then
    die "the target already holds $existing curriculum row(s) — restoring would collide on
       primary keys. Re-run with --force to replace the curriculum (this CASCADEs away every
       learner attempt against those exercises), or start clean: docker compose down -v"
  fi

  if [[ $force -eq 1 ]]; then
    local joined
    joined=$(IFS=,; printf '%s' "${CONTENT_TABLES[*]}")
    say "clearing the curriculum (learners' attempts go with it)…"
    psql_container -qAtc "truncate table $joined cascade"
  fi

  say "loading $file into $DB_SERVICE…"
  psql_container -q -v ON_ERROR_STOP=1 <"$file"
  cmd_verify
}

cmd_import() {
  local url
  url=$(local_dburl)
  # The synthesiser is required here: content with audio_text fails loudly rather than
  # importing silence. On Linux, seed from a dump instead (see the header).
  export DATABASE_URL=$url
  local files=()
  for f in content/hsk2.0/*.yaml; do files+=(--file "$f"); done
  say "importing ${#files[@]} file(s) into the database behind DATABASE_URL"
  go run ./cmd/contentctl import "${files[@]}"
  say "done — verify with ./scripts/seed-content.sh verify"
}

case ${1:-} in
  verify)  shift; cmd_verify "$@" ;;
  dump)    shift; cmd_dump "$@" ;;
  restore) shift; cmd_restore "$@" ;;
  import)  shift; cmd_import "$@" ;;
  *)
    sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
    exit 1
    ;;
esac
