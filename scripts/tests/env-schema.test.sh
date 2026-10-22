#!/usr/bin/env bash
# Every setting has a question (issue #52): fails if a key is read by the
# backend or listed in .env.example but missing from scripts/env.schema, if a
# schema line is malformed, or if a setting never reaches the containers.
# Usage: bash scripts/tests/env-schema.test.sh
#        BACKEND_DIR=… SCHEMA=… bash scripts/tests/env-schema.test.sh --check-only
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# Set by Docker Compose / the scripts, not asked: they never go in .env.
EXEMPT="APP_ENV PORT DB_PATH PHOTO_DIR UPDATE_DIR BACKUP_DIR GIT_SHA"
# Read by the host scripts only (deploy.sh), never by a container.
HOST_ONLY="PUBLIC_URL"
KINDS="ask secret generate locked default"
VALIDATORS="tz pin4 phone name text domain aud email-or-url ips cidrs origins https-url secret48 false-only empty-in-normal-runs"

# check BACKEND_DIR SCHEMA EXAMPLE COMPOSE → prints problems, returns 1 if any.
check() {
  local backend="$1" schema="$2" example="$3" compose="$4" problems=0 keys line key kind req val n
  keys=" "
  while IFS= read -r line; do
    case "$line" in "" | \#*) continue ;; esac
    n="$(awk -F'|' '{ print NF }' <<<"$line")"
    IFS='|' read -r key kind req _ val _ <<<"$line"
    if [ "$n" -ne 7 ]; then
      echo "schema: '$key' has $n fields, expected 7"; problems=$((problems + 1))
    fi
    case " $KINDS " in *" $kind "*) ;; *) echo "schema: '$key' has unknown kind '$kind'"; problems=$((problems + 1)) ;; esac
    case "$req" in yes | no | first-run | if:*) ;; *) echo "schema: '$key' has unknown required '$req'"; problems=$((problems + 1)) ;; esac
    if [ -n "$val" ]; then
      case " $VALIDATORS " in *" $val "*) ;; *) echo "schema: '$key' has unknown validator '$val'"; problems=$((problems + 1)) ;; esac
    fi
    case "$kind" in ask | secret)
      [ -n "$(cut -d'|' -f6 <<<"$line")" ] && [ -n "$(cut -d'|' -f7 <<<"$line")" ] ||
        { echo "schema: '$key' is asked but has no question in English and Arabic"; problems=$((problems + 1)); } ;;
    esac
    keys="${keys}${key} "
  done <"$schema"

  # Keys the backend reads.
  for key in $(grep -rhoE '(Getenv|LookupEnv|envBool|envInt|envDuration)\("[A-Z][A-Z0-9_]*"' "$backend" --include='*.go' 2>/dev/null |
    grep -v '_test.go' | sed -E 's/.*\("//; s/"$//' | sort -u); do
    case " $EXEMPT " in *" $key "*) continue ;; esac
    case "$keys" in *" $key "*) ;; *) echo "backend reads $key, but scripts/env.schema has no line for it"; problems=$((problems + 1)) ;; esac
  done
  # Keys in .env.example.
  for key in $(grep -oE '^[A-Z][A-Z0-9_]*=' "$example" | tr -d =); do
    case "$keys" in *" $key "*) ;; *) echo ".env.example has $key, but scripts/env.schema has no line for it"; problems=$((problems + 1)) ;; esac
  done
  # Every setting must reach the containers.
  for key in $keys; do
    case " $HOST_ONLY " in *" $key "*) continue ;; esac
    grep -q "\${${key}" "$compose" || { echo "$key is in the schema but docker-compose.yml never passes it to a container"; problems=$((problems + 1)); }
  done
  [ "$problems" -eq 0 ]
}

if [ "${1:-}" = "--check-only" ]; then
  check "${BACKEND_DIR:-$ROOT/backend}" "${SCHEMA:-$ROOT/scripts/env.schema}" "${EXAMPLE:-$ROOT/.env.example}" "${COMPOSE:-$ROOT/docker-compose.yml}"
  exit
fi

fails=0
ok() { echo "ok   - $1"; }
bad() { echo "FAIL - $1"; fails=$((fails + 1)); }

# 1. The repository is consistent.
if out="$(check "$ROOT/backend" "$ROOT/scripts/env.schema" "$ROOT/.env.example" "$ROOT/docker-compose.yml")"; then
  ok "every setting has a schema line, a question and a way into the containers"
else
  bad "schema check failed:"
  printf '%s\n' "$out" | sed 's/^/       | /'
fi

# 2. Adding os.Getenv("NEW_THING") without a schema line is caught.
W="$(mktemp -d)"
cp -R "$ROOT/backend" "$W/backend"
printf 'package handlers\n\nimport "os"\n\nvar newThing = os.Getenv("NEW_THING")\n' >"$W/backend/internal/handlers/new_thing.go"
if out="$(check "$W/backend" "$ROOT/scripts/env.schema" "$ROOT/.env.example" "$ROOT/docker-compose.yml")"; then
  bad "a new os.Getenv(\"NEW_THING\") without a schema line was not caught"
else
  if grep -q 'backend reads NEW_THING' <<<"$out"; then ok "a new os.Getenv(\"NEW_THING\") without a schema line fails"; else bad "wrong message: $out"; fi
fi
# … and a schema line for it without compose wiring is caught too.
cp "$ROOT/scripts/env.schema" "$W/schema"
echo 'NEW_THING|ask|no||text|New thing (Enter to skip)|شيء جديد' >>"$W/schema"
if out="$(check "$W/backend" "$W/schema" "$ROOT/.env.example" "$ROOT/docker-compose.yml")"; then
  bad "a setting missing from docker-compose.yml was not caught"
else
  if grep -q 'NEW_THING is in the schema but docker-compose.yml never passes it' <<<"$out"; then ok "a setting that never reaches the containers fails"; else bad "wrong message: $out"; fi
fi
# A malformed line is caught.
printf 'BROKEN|ask|yes\n' >>"$W/schema"
if check "$W/backend" "$W/schema" "$ROOT/.env.example" "$ROOT/docker-compose.yml" >/dev/null; then
  bad "a malformed schema line was not caught"
else
  ok "a malformed schema line fails"
fi
rm -rf "$W"

if [ "$fails" -gt 0 ]; then
  echo "$fails check(s) failed"
  exit 1
fi
echo "all env-schema checks passed"
