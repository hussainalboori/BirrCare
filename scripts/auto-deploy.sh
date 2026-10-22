#!/usr/bin/env bash
set -euo pipefail

# Birrcare (رعاية بر) — Pull-based updater
#
# Runs on the BirrCare host from systemd (scripts/systemd/): a timer every few
# minutes, and a path unit the moment the app asks for an install. Every run it
# fetches origin/main, checks that the commit's CI run on GitHub passed, and
# tells the app what is on offer. It deploys only when:
#   - the Super Admin pressed Install in the app (data/update/request.json) and
#     the requested commit is exactly the CI-passed origin/main, or
#   - automatic updates are switched on (data/update/policy.json {"mode":"auto"}).
# Without a policy file updates are manual (issue #43).
#
# Why pull, not push: the repo is public, and a self-hosted GitHub runner on a
# public repo executes workflow code from fork pull requests once approved. With
# this poller nothing on GitHub can run code on the host; the host only ever runs
# what is on main, and only after CI succeeded. The app never deploys either: it
# can at most ask for "the latest CI-passed main".
#
# Files shared with the app (data volume, data/update/):
#   available.json  host → app   {current_sha, target_sha, ci, checked_at, commits, changes_database,
#                                 needs_config: settings the new version needs that .env lacks}
#   policy.json     app  → host  {mode: "manual"|"auto", skipped_sha}
#   request.json    app  → host  {action: "install", target_sha, requested_by, requested_at}
#   result.json     host → app   {sha, status: "success"|"failed"|"rejected"|"needs_config", started_at,
#                                 finished_at, log_tail, missing}
#
# Requirements on the host: git, curl, jq, docker compose.
#
# Env:
#   DEPLOY_BRANCH  branch to track (default: main)
#   DRY_RUN=1      report what would happen without deploying
#   UPDATE_DIR     shared directory (default: <repo>/data/update)
#   DEPLOY_CMD     deploy command (default: ./scripts/deploy.sh; tests stub it)
#
# State (last deployed / rejected / CI-passed commit) lives outside the repo in
# ${XDG_STATE_HOME:-~/.local/state}/birrcare so `git reset --hard` never touches it.

# Everything runs inside functions: bash reads a script lazily, and `git reset
# --hard` below may rewrite this very file mid-run. Function bodies are parsed up front.

now_utc() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# write_json FILE: stdin → FILE through a temp file and a rename, so the app
# never reads half a file.
write_json() {
  local file="$1" tmp
  tmp="$(dirname "$file")/.$(basename "$file").tmp.$$"
  cat >"$tmp"
  chmod 644 "$tmp"
  mv -f "$tmp" "$file"
}

# needs_config_for SHA: settings that version requires but .env doesn't answer
# (its scripts/env.schema checked by setup-env.sh --check), as a JSON array.
# Sets NEEDS_CONFIG.
needs_config_for() {
  local sha="$1" schema out
  NEEDS_CONFIG="[]"
  [ -x ./scripts/setup-env.sh ] || return 0
  schema="$(mktemp)"
  if git show "${sha}:scripts/env.schema" >"$schema" 2>/dev/null; then
    out="$(./scripts/setup-env.sh --check --schema "$schema" 2>/dev/null)" || true
    NEEDS_CONFIG="$(jq -c '[.missing[] | {key, question_en, question_ar}]' <<<"$out" 2>/dev/null)" || NEEDS_CONFIG="[]"
    [ -n "$NEEDS_CONFIG" ] || NEEDS_CONFIG="[]"
  fi
  rm -f "$schema"
}

# write_available CURRENT OFFER CI
write_available() {
  local current="$1" offer="$2" ci="$3" commits="[]" changes_db=false
  if [ -n "$offer" ]; then
    local range="$offer" files
    if [ -n "$current" ] && git cat-file -e "${current}^{commit}" 2>/dev/null; then
      range="${current}..${offer}"
      files="$(git diff --name-only "$current" "$offer" -- backend/internal/db/)"
      [ -z "$files" ] || changes_db=true
    else
      # Unknown starting point: show recent commits and assume the database may change.
      range="-n 20 ${offer}"
      changes_db=true
    fi
    # shellcheck disable=SC2086 # range may hold "-n 20 sha"
    commits="$(git log --format='%h%x09%s%x09%cI' $range |
      jq -R -s -c 'split("\n") | map(select(length > 0) | split("\t") | {sha: .[0], subject: .[1], date: .[2]})')"
    needs_config_for "$offer"
  else
    NEEDS_CONFIG="[]"
  fi
  jq -n \
    --arg current "$current" --arg target "$offer" --arg ci "$ci" --arg checked "$(now_utc)" \
    --argjson commits "$commits" --argjson changes_db "$changes_db" --argjson needs "$NEEDS_CONFIG" \
    '{current_sha: $current, target_sha: $target, ci: $ci, checked_at: $checked,
      commits: $commits, changes_database: $changes_db, needs_config: $needs}' |
    write_json "$UPDATE_DIR/available.json"
}

# write_result SHA STATUS STARTED FINISHED LOG_TAIL [MISSING_KEYS_JSON]
write_result() {
  jq -n --arg sha "$1" --arg status "$2" --arg started "$3" --arg finished "$4" --arg log "$5" \
    --argjson missing "${6:-[]}" \
    '{sha: $sha, status: $status, started_at: $started, finished_at: $finished, log_tail: $log, missing: $missing}' |
    write_json "$UPDATE_DIR/result.json"
}

# ci_state SHA → success | failure | pending (and remembers the answer)
ci_state() {
  local sha="$1" repo runs status conclusion
  if [ "$sha" = "$(cat "$CI_OK_FILE" 2>/dev/null || true)" ]; then
    echo success
    return
  fi
  if [ "$sha" = "$(cat "$REJECTED_FILE" 2>/dev/null || true)" ]; then
    echo failure
    return
  fi
  # Only commits whose CI/CD workflow run on the push to main succeeded.
  repo="$(git remote get-url origin | sed -E 's#^(https://github\.com/|git@github\.com:)##; s#\.git$##')"
  runs="$(curl -fsSL --max-time 20 \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${repo}/actions/runs?head_sha=${sha}&event=push&per_page=10")"
  read -r status conclusion < <(jq -r '
    [.workflow_runs[] | select(.path == ".github/workflows/ci-cd.yml")]
    | sort_by(.created_at) | last
    | if . == null then "missing none" else "\(.status) \(.conclusion // "none")" end
  ' <<<"$runs")
  case "$status/$conclusion" in
    completed/success)
      [ -n "${DRY_RUN:-}" ] || echo "$sha" >"$CI_OK_FILE"
      echo success
      ;;
    completed/*)
      echo "auto-deploy: CI for ${sha:0:7} finished with '${conclusion}'; not offering it" >&2
      [ -n "${DRY_RUN:-}" ] || echo "$sha" >"$REJECTED_FILE"
      echo failure
      ;;
    *)
      echo "auto-deploy: CI for ${sha:0:7} is '${status}'; will check again next run" >&2
      echo pending
      ;;
  esac
}

# deploy SHA: check out SHA, run deploy.sh, record the result.
deploy() {
  local sha="$1" started log rc=0 status
  started="$(now_utc)"
  if [ -n "${DRY_RUN:-}" ]; then
    echo "auto-deploy: DRY_RUN - would deploy ${sha:0:7} (currently deployed: ${DEPLOYED:0:7})"
    return 0
  fi
  echo "auto-deploy: deploying ${sha:0:7} (previous: ${DEPLOYED:0:7})"
  log="${STATE_DIR}/last-deploy.log"
  # Git-ignored files (data/, .env) are left untouched by reset --hard.
  git reset --hard --quiet "$sha"
  GIT_SHA="$sha" $DEPLOY_CMD >"$log" 2>&1 || rc=$?
  cat "$log"
  if [ "$rc" -eq 0 ]; then
    status=success
    echo "$sha" >"$DEPLOYED_FILE"
    DEPLOYED="$sha"
  elif [ "$rc" -eq 3 ]; then
    # A new setting has no answer yet (issue #52). Not a broken version: once
    # someone answers it with ./scripts/deploy.sh, the same version installs.
    local missing
    missing="$(grep '^NEEDS_CONFIG ' "$log" | tail -n 1 | cut -d' ' -f2- | jq -c '[.missing[].key]' 2>/dev/null || echo '[]')"
    write_result "$sha" needs_config "$started" "$(now_utc)" \
      "This version needs a new setting. Run ./scripts/deploy.sh on the server once to answer it." "${missing:-[]}"
    rm -f "$UPDATE_DIR/request.processing"
    write_available "$DEPLOYED" "$sha" success
    return 1
  else
    # Don't rebuild a broken commit every run; the next push to main retries.
    status=failed
    echo "$sha" >"$REJECTED_FILE"
    echo "auto-deploy: deploy.sh failed for ${sha:0:7}" >&2
  fi
  write_result "$sha" "$status" "$started" "$(now_utc)" "$(tail -n 40 "$log")"
  rm -f "$UPDATE_DIR/request.processing"
  write_available "$DEPLOYED" "" "$([ "$status" = success ] && echo deployed || echo failure)"
  [ "$status" = success ]
}

# Cron appends to these logs (systemd uses the journal, which rotates itself):
# keep them from growing forever.
trim_log() {
  local f="$1"
  if [ -f "$f" ] && [ "$(wc -c <"$f")" -gt 1048576 ]; then
    tail -n 2000 "$f" >"${f}.tmp" && mv -f "${f}.tmp" "$f"
  fi
  return 0
}

main() {
  local project_root branch
  project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  branch="${DEPLOY_BRANCH:-main}"
  STATE_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/birrcare"
  DEPLOYED_FILE="${STATE_DIR}/deployed-sha"
  REJECTED_FILE="${STATE_DIR}/rejected-sha"
  CI_OK_FILE="${STATE_DIR}/ci-ok-sha"
  UPDATE_DIR="${UPDATE_DIR:-${project_root}/data/update}"
  DEPLOY_CMD="${DEPLOY_CMD:-./scripts/deploy.sh}"

  for cmd in git curl jq docker; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      echo "auto-deploy: '$cmd' is required but not installed; run once: sudo ./scripts/install-deps.sh" >&2
      exit 1
    fi
  done

  mkdir -p "$STATE_DIR" "$UPDATE_DIR"
  trim_log "${STATE_DIR}/updater.log"
  cd "$project_root"

  # One run at a time: the timer and the path unit can fire together.
  if command -v flock >/dev/null 2>&1; then
    exec 9>"${STATE_DIR}/auto-deploy.lock"
    flock -n 9 || { echo "auto-deploy: another run is in progress"; exit 0; }
  fi

  git fetch --quiet --prune origin "$branch"
  local target mode skipped ci offer
  target="$(git rev-parse "origin/${branch}")"
  DEPLOYED="$(cat "$DEPLOYED_FILE" 2>/dev/null || true)"

  # No policy file means manual: nothing installs unless the Super Admin asks.
  mode=manual
  skipped=""
  if [ -f "$UPDATE_DIR/policy.json" ]; then
    mode="$(jq -r '.mode // "manual"' "$UPDATE_DIR/policy.json" 2>/dev/null || echo manual)"
    skipped="$(jq -r '.skipped_sha // ""' "$UPDATE_DIR/policy.json" 2>/dev/null || true)"
    [ "$mode" = auto ] || mode=manual
  fi

  if [ "$target" = "$DEPLOYED" ]; then
    ci=deployed
  else
    ci="$(ci_state "$target")"
  fi

  offer=""
  if [ "$ci" = success ] && [ "$target" != "$skipped" ]; then
    offer="$target"
  fi
  write_available "$DEPLOYED" "$offer" "$ci"

  # An install the Super Admin asked for. Rename first so it runs once.
  if [ -f "$UPDATE_DIR/request.json" ]; then
    local req_target
    if [ -n "${DRY_RUN:-}" ]; then
      echo "auto-deploy: DRY_RUN - install requested for $(jq -r '.target_sha // ""' "$UPDATE_DIR/request.json" | cut -c1-7); offered: ${offer:0:7}"
      exit 0
    fi
    mv -f "$UPDATE_DIR/request.json" "$UPDATE_DIR/request.processing"
    req_target="$(jq -r '.target_sha // ""' "$UPDATE_DIR/request.processing" 2>/dev/null || true)"
    if [ "$ci" = success ] && [ "$req_target" = "$target" ] && [ "$NEEDS_CONFIG" != "[]" ]; then
      echo "auto-deploy: ${target:0:7} needs a new setting first: $(jq -r '[.[].key] | join(", ")' <<<"$NEEDS_CONFIG")"
      write_result "$req_target" needs_config "" "$(now_utc)" \
        "This version needs a new setting. Run ./scripts/deploy.sh on the server once to answer it." \
        "$(jq -c '[.[].key]' <<<"$NEEDS_CONFIG")"
      rm -f "$UPDATE_DIR/request.processing"
    elif [ "$ci" = success ] && [ "$req_target" = "$target" ]; then
      deploy "$target" || exit 1
    else
      echo "auto-deploy: refusing install request for '${req_target:0:7}': only the CI-passed origin/${branch} (${target:0:7}) can be installed"
      write_result "$req_target" rejected "" "$(now_utc)" \
        "Only the latest version that passed its checks (${target:0:7}) can be installed. CI: ${ci}."
      rm -f "$UPDATE_DIR/request.processing"
    fi
    exit 0
  fi

  if [ "$mode" = auto ] && [ -n "$offer" ]; then
    if [ "$NEEDS_CONFIG" != "[]" ]; then
      echo "auto-deploy: not installing ${target:0:7} automatically: it needs a new setting first"
    else
      deploy "$target" || exit 1
    fi
  fi
}

main "$@"
exit
