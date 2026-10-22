#!/usr/bin/env bash
# Tests for scripts/auto-deploy.sh (issue #43) with stub git / curl / docker.
# Usage: bash scripts/tests/auto-deploy.test.sh   (needs jq)
set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/auto-deploy.sh"
T1=1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
T2=2222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
OLD=0000000ccccccccccccccccccccccccccccccccc
fails=0

# setup: a fresh fake repo, state dir and stubs. Sets $W.
setup() {
  W="$(mktemp -d)"
  mkdir -p "$W/repo/scripts" "$W/bin" "$W/state/birrcare" "$W/repo/data/update"
  cp "$SCRIPT" "$W/repo/scripts/auto-deploy.sh"
  echo "$OLD" >"$W/state/birrcare/deployed-sha"
  echo "$T1" >"$W/target"
  echo success >"$W/ci"
  : >"$W/deploys"

  cat >"$W/bin/git" <<EOF
#!/usr/bin/env bash
case "\$1" in
  fetch) exit 0 ;;
  rev-parse) cat "$W/target" ;;
  remote) echo "https://github.com/example/birrcare.git" ;;
  reset) echo "\$4" >"$W/reset" ;;
  cat-file) exit 0 ;;
  diff) [ -f "$W/db-change" ] && echo backend/internal/db/migrations.go; exit 0 ;;
  log) printf 'abc1234\tfix(vitals): larger numbers\t2026-10-21T10:00:00+03:00\n' ;;
  show) [ -f "$W/target-schema" ] && cat "$W/target-schema" || exit 128 ;;
  *) echo "stub git: unexpected \$*" >&2; exit 1 ;;
esac
EOF
  cat >"$W/bin/curl" <<EOF
#!/usr/bin/env bash
echo '{"workflow_runs":[{"path":".github/workflows/ci-cd.yml","created_at":"2026-10-21T10:00:00Z","status":"completed","conclusion":"'"\$(cat "$W/ci")"'"}]}'
EOF
  printf '#!/usr/bin/env bash\nexit 0\n' >"$W/bin/docker"
  cat >"$W/deploy" <<EOF
#!/usr/bin/env bash
echo "deployed \$GIT_SHA" >>"$W/deploys"
echo "build ok"
EOF
  chmod +x "$W/bin/"* "$W/deploy"
}

run() {
  PATH="$W/bin:$PATH" XDG_STATE_HOME="$W/state" DEPLOY_CMD="$W/deploy" \
    bash "$W/repo/scripts/auto-deploy.sh" >"$W/out" 2>&1 || echo "exit $?" >>"$W/out"
}

deploys() { wc -l <"$W/deploys" | tr -d ' '; }
avail() { jq -r "$1" "$W/repo/data/update/available.json"; }
result() { jq -r "$1" "$W/repo/data/update/result.json"; }

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

request() {
  printf '{"action":"install","target_sha":"%s","requested_by":"sa1","requested_at":"2026-10-21T10:00:00Z"}' "$1" \
    >"$W/repo/data/update/request.json"
}

# 1. No policy.json + new CI-passed commit → offered, not deployed.
setup
touch "$W/db-change"
seq 1 200000 >"$W/state/birrcare/updater.log" # a cron log that grew too big
run
check "log: trimmed to the last 2000 lines" "$(wc -l <"$W/state/birrcare/updater.log" | tr -d ' ')" "2000"
check "manual by default: available.json offers the commit" "$(avail .target_sha)" "$T1"
check "manual by default: CI recorded" "$(avail .ci)" "success"
check "manual by default: commit list" "$(avail '.commits[0].subject')" "fix(vitals): larger numbers"
check "manual by default: database change flagged" "$(avail .changes_database)" "true"
check "manual by default: current version" "$(avail .current_sha)" "$OLD"
check "manual by default: deploy.sh not called" "$(deploys)" "0"
rm -rf "$W"

# 2. request.json with the matching SHA → deployed once, request consumed, result success.
setup
request "$T1"
run
check "install request: deploy.sh called once" "$(deploys)" "1"
check "install request: deploy.sh got GIT_SHA" "$(cat "$W/deploys")" "deployed $T1"
check "install request: checked out the target" "$(cat "$W/reset")" "$T1"
check "install request: request.json gone" "$(ls "$W/repo/data/update" | grep -c request || true)" "0"
check "install request: result success" "$(result .status)" "success"
check "install request: result sha" "$(result .sha)" "$T1"
check "install request: deployed-sha updated" "$(cat "$W/state/birrcare/deployed-sha")" "$T1"
check "install request: nothing offered afterwards" "$(avail .target_sha)" ""
run
check "install request: a second run does not deploy again" "$(deploys)" "1"
rm -rf "$W"

# 3. request.json for a SHA that isn't the CI-passed origin/main → rejected.
setup
request "$T2"
run
check "wrong sha: deploy.sh not called" "$(deploys)" "0"
check "wrong sha: result rejected" "$(result .status)" "rejected"
check "wrong sha: request consumed" "$(ls "$W/repo/data/update" | grep -c request || true)" "0"
check "wrong sha: no checkout" "$([ -f "$W/reset" ] && echo yes || echo no)" "no"
rm -rf "$W"

# 3b. Matching SHA but CI failed → rejected, not offered.
setup
echo failure >"$W/ci"
request "$T1"
run
check "CI failed: deploy.sh not called" "$(deploys)" "0"
check "CI failed: result rejected" "$(result .status)" "rejected"
check "CI failed: not offered" "$(avail .target_sha)" ""
rm -rf "$W"

# 4. {"mode":"auto"} → deploys without a request, as before.
setup
echo '{"mode":"auto"}' >"$W/repo/data/update/policy.json"
run
check "auto: deploy.sh called" "$(deploys)" "1"
check "auto: result success" "$(result .status)" "success"
rm -rf "$W"

# 5. skipped_sha == target → not offered; a newer commit → offered.
setup
printf '{"mode":"manual","skipped_sha":"%s"}' "$T1" >"$W/repo/data/update/policy.json"
run
check "skipped: not offered" "$(avail .target_sha)" ""
check "skipped: checker still reports in" "$(avail .checked_at | grep -c T || true)" "1"
echo "$T2" >"$W/target"
run
check "newer than skipped: offered" "$(avail .target_sha)" "$T2"
check "newer than skipped: not deployed" "$(deploys)" "0"
rm -rf "$W"

# 6. A failing deploy → result failed with the log tail, request consumed.
setup
printf '#!/usr/bin/env bash\necho "backend health check failed"\nexit 1\n' >"$W/deploy"
request "$T1"
run
check "deploy fails: result failed" "$(result .status)" "failed"
check "deploy fails: log tail kept" "$(result .log_tail)" "backend health check failed"
check "deploy fails: request consumed" "$(ls "$W/repo/data/update" | grep -c request || true)" "0"
check "deploy fails: deployed-sha unchanged" "$(cat "$W/state/birrcare/deployed-sha")" "$OLD"
rm -rf "$W"

# 7. DRY_RUN leaves a pending request alone.
setup
request "$T1"
DRY_RUN=1 run
check "dry run: request untouched" "$(ls "$W/repo/data/update" | grep -c 'request.json' || true)" "1"
check "dry run: deploy.sh not called" "$(deploys)" "0"
rm -rf "$W"

# with_settings: the real setup-env.sh, a complete .env, and a target version
# whose schema adds a required setting this .env doesn't have.
with_settings() {
  local root
  root="$(cd "$(dirname "$SCRIPT")/.." && pwd)"
  cp "$root/scripts/setup-env.sh" "$W/repo/scripts/" && chmod +x "$W/repo/scripts/setup-env.sh"
  mkdir -p "$W/repo/data" && echo db >"$W/repo/data/birrcare.db"
  printf 'APP_TIMEZONE=Asia/Bahrain\nEMERGENCY_NUMBER=999\nSESSION_SECRET=0123456789abcdefghij0123456789abcdefghij0123456789abcdefghij\nAUTH_DEV_FALLBACK=false\nCF_ACCESS_TRUST_EMAIL_HEADER=false\n' >"$W/repo/.env"
  cat >"$W/target-schema" <<'SCHEMA'
APP_TIMEZONE|ask|yes|@detect-tz|tz|Household time zone|المنطقة الزمنية
EMERGENCY_NUMBER|ask|yes|112|phone|Emergency number|رقم الطوارئ
WARD_NAME|ask|yes||text|Name of the care ward|اسم جناح الرعاية
SESSION_SECRET|generate|yes|@random48|secret48||
SCHEMA
}

# 11. The new version needs a setting .env lacks: offered with needs_config,
#     and Install refuses without touching anything.
setup
with_settings
run
check "needs config: offered" "$(avail .target_sha)" "$T1"
check "needs config: key listed" "$(avail '.needs_config[0].key')" "WARD_NAME"
check "needs config: question listed" "$(avail '.needs_config[0].question_en')" "Name of the care ward"
request "$T1"
run
check "needs config: deploy.sh not called" "$(deploys)" "0"
check "needs config: no checkout" "$([ -f "$W/reset" ] && echo yes || echo no)" "no"
check "needs config: result" "$(result .status)" "needs_config"
check "needs config: missing key in result" "$(result '.missing[0]')" "WARD_NAME"
check "needs config: request consumed" "$(ls "$W/repo/data/update" | grep -c '^request' || true)" "0"
# Once the setting is answered, the same version installs.
echo "WARD_NAME=Home" >>"$W/repo/.env"
run
check "answered: no longer needs config" "$(avail '.needs_config | length')" "0"
request "$T1"
run
check "answered: deployed" "$(deploys)" "1"
rm -rf "$W"

# 12. Automatic updates don't install a version that needs a setting.
setup
with_settings
echo '{"mode":"auto"}' >"$W/repo/data/update/policy.json"
run
check "auto + needs config: not deployed" "$(deploys)" "0"
rm -rf "$W"

# 13. deploy.sh stops with exit 3 (setting missing): needs_config, not rejected.
setup
printf '#!/usr/bin/env bash\necho '"'"'NEEDS_CONFIG {"missing":[{"key":"WARD_NAME","question_en":"Ward","question_ar":"جناح"}],"unsafe":false}'"'"'\nexit 3\n' >"$W/deploy"
request "$T1"
run
check "exit 3: result needs_config" "$(result .status)" "needs_config"
check "exit 3: missing key" "$(result '.missing[0]')" "WARD_NAME"
check "exit 3: version not rejected" "$([ -f "$W/state/birrcare/rejected-sha" ] && cat "$W/state/birrcare/rejected-sha" || echo none)" "none"
check "exit 3: still offered" "$(avail .target_sha)" "$T1"
rm -rf "$W"

if [ "$fails" -gt 0 ]; then
  echo "$fails check(s) failed"
  exit 1
fi
echo "all auto-deploy checks passed"
