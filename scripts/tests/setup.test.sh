#!/usr/bin/env bash
# Tests for scripts/setup.sh (one command for a new server), with stubs.
# Usage: bash scripts/tests/setup.test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fails=0

setup() {
  W="$(mktemp -d)"
  mkdir -p "$W/repo/scripts" "$W/bin"
  cp "$ROOT/scripts/setup.sh" "$W/repo/scripts/"
  printf '#!/usr/bin/env bash\necho "deploy $*" >>"%s/calls"\n' "$W" >"$W/repo/scripts/deploy.sh"
  printf '#!/usr/bin/env bash\necho "install-deps" >>"%s/calls"\n' "$W" >"$W/repo/scripts/install-deps.sh"
  : >"$W/calls"
  printf '#!/usr/bin/env bash\necho "sudo $*" >>"%s/calls"; "$@"\n' "$W" >"$W/bin/sudo"
  printf '#!/usr/bin/env bash\n[ "$1" = info ] && [ -f "%s/docker-denied" ] && exit 1\nexit 0\n' "$W" >"$W/bin/docker"
  printf '#!/usr/bin/env bash\necho "sg $*" >>"%s/calls"\n' "$W" >"$W/bin/sg"
  printf '#!/usr/bin/env bash\necho "docker:x:999:runner"\n' >"$W/bin/getent"
  printf '#!/usr/bin/env bash\ncase "$1" in -u) cat "%s/uid" 2>/dev/null || echo 1000 ;; -un) echo runner ;; esac\n' "$W" >"$W/bin/id"
  for c in git curl jq restic; do printf '#!/usr/bin/env bash\nexit 0\n' >"$W/bin/$c"; done
  chmod +x "$W/bin/"* "$W/repo/scripts/"*.sh
}

run() {
  (cd "$W/repo" && PATH="$W/bin:/usr/bin:/bin" bash ./scripts/setup.sh "$@" </dev/null >"$W/out" 2>&1) && echo 0 >"$W/rc" || echo $? >"$W/rc"
}

check() {
  if [ "$2" = "$3" ]; then
    echo "ok   - $1"
  else
    echo "FAIL - $1: got '$2', want '$3'"
    sed 's/^/       | /' "$W/out" "$W/calls"
    fails=$((fails + 1))
  fi
}

# 1. Everything installed: straight to deploy, options passed on.
setup
run --lang ar
check "ready: deploy only" "$(cat "$W/calls")" "deploy --lang ar"
rm -rf "$W"

# 2. A tool missing: the installer through sudo first, then deploy.
setup
rm "$W/bin/restic" # a tool no system ships by default
run
check "missing: installer via sudo, then deploy" "$(tr '\n' '|' <"$W/calls")" "sudo ./scripts/install-deps.sh|install-deps|deploy |"
rm -rf "$W"

# 3. Just added to the docker group: deploy runs with it (sg), no log-out needed.
setup
touch "$W/docker-denied"
run
check "new group: deploy through sg docker" "$(grep -c '^sg docker -c ./scripts/deploy.sh' "$W/calls")" "1"
rm -rf "$W"

# 4. Run as root: refused.
setup
echo 0 >"$W/uid"
run
check "root: refused" "$(cat "$W/rc")" "1"
check "root: nothing run" "$(wc -l <"$W/calls" | tr -d ' ')" "0"
rm -rf "$W"

if [ "$fails" -gt 0 ]; then
  echo "$fails check(s) failed"
  exit 1
fi
echo "all setup checks passed"
