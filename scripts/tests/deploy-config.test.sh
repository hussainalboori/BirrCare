#!/usr/bin/env bash
# deploy.sh and settings (issue #52): with nobody at a terminal, a missing or
# unsafe setting stops the deploy before anything is rebuilt.
# Usage: bash scripts/tests/deploy-config.test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fails=0

setup() {
  W="$(mktemp -d)"
  mkdir -p "$W/repo/scripts" "$W/bin"
  cp "$ROOT/scripts/deploy.sh" "$ROOT/scripts/setup-env.sh" "$ROOT/scripts/env.schema" "$ROOT/scripts/emergency-numbers" "$W/repo/scripts/"
  cp "$ROOT/.env.example" "$W/repo/"
  : >"$W/docker-calls"
  printf '#!/usr/bin/env bash\necho "$*" >>"%s/docker-calls"\n' "$W" >"$W/bin/docker"
  # The app answers its health check as the version being deployed.
  printf '#!/usr/bin/env bash\ncase "${@: -1}" in *8080/api/v1/health) echo '"'"'{"version":"0000000"}'"'"' ;; esac\nexit 0\n' >"$W/bin/curl"
  printf '#!/usr/bin/env bash\necho 0000000\n' >"$W/bin/git"
  printf '#!/usr/bin/env bash\nexit 0\n' >"$W/bin/sqlite3"
  # An existing install: the first-account questions don't apply.
  mkdir -p "$W/repo/data" && echo db >"$W/repo/data/birrcare.db"
  chmod +x "$W/bin/"* "$W/repo/scripts/"*.sh
  cat >"$W/repo/.env" <<'ENV'
APP_TIMEZONE=Asia/Bahrain
EMERGENCY_NUMBER=999
SESSION_SECRET=0123456789abcdefghij0123456789abcdefghij0123456789abcdefghij
AUTH_DEV_FALLBACK=false
CF_ACCESS_TRUST_EMAIL_HEADER=false
ENV
  chmod 600 "$W/repo/.env"
}

run() {
  (cd "$W/repo" && PATH="$W/bin:$PATH" BIRRCARE_MIN_FREE_GB=0 BIRRCARE_HEALTH_TIMEOUT=3 bash ./scripts/deploy.sh </dev/null >"$W/out" 2>&1) && echo 0 >"$W/rc" || echo $? >"$W/rc"
}

check() {
  local name="$1" got="$2" want="$3"
  if [ "$got" = "$want" ]; then
    echo "ok   - $name"
  else
    echo "FAIL - $name: got '$got', want '$want'"
    sed 's/^/       | /' "$W/out"
    fails=$((fails + 1))
  fi
}

# 1. Complete settings: deploys.
setup
run
check "complete: exit 0" "$(cat "$W/rc")" "0"
check "complete: built" "$(grep -c '^compose build' "$W/docker-calls")" "1"
rm -rf "$W"

# 2. A required setting missing, no terminal: stop before building.
setup
grep -v '^APP_TIMEZONE=' "$W/repo/.env" >"$W/tmp" && cat "$W/tmp" >"$W/repo/.env"
# no time zone can be guessed on this machine
TZ_DIR=/nonexistent run
check "missing: exit 3" "$(cat "$W/rc")" "3"
check "missing: nothing built" "$(grep -c 'compose build' "$W/docker-calls" || true)" "0"
check "missing: machine-readable line" "$(grep '^NEEDS_CONFIG ' "$W/out" | cut -d' ' -f2- | jq -r '.missing[0].key')" "APP_TIMEZONE"
check "missing: says the running version stays up" "$(grep -c 'the running version stays up' "$W/out")" "1"
rm -rf "$W"

# 3. Unsafe AUTH_DEV_FALLBACK=true: stop, nothing built.
setup
sed -i.orig 's/^AUTH_DEV_FALLBACK=false/AUTH_DEV_FALLBACK=true/' "$W/repo/.env" && rm -f "$W/repo/.env.orig"
run
check "unsafe: exit 1" "$(cat "$W/rc")" "1"
check "unsafe: nothing built" "$(grep -c 'compose build' "$W/docker-calls" || true)" "0"
rm -rf "$W"

if [ "$fails" -gt 0 ]; then
  echo "$fails check(s) failed"
  exit 1
fi
echo "all deploy-config checks passed"
