#!/usr/bin/env bash
set -euo pipefail

# Birrcare (رعاية بر) — Pull-based auto-deploy
#
# Runs on the BirrCare host from a systemd timer (scripts/systemd/). Every tick it
# fetches origin/main and, if there is a new commit whose CI run on GitHub passed,
# checks it out and runs scripts/deploy.sh.
#
# Why pull, not push: the repo is public, and a self-hosted GitHub runner on a
# public repo executes workflow code from fork pull requests once approved. With
# this poller nothing on GitHub can run code on the host; the host only ever runs
# what is on main, and only after CI succeeded.
#
# Requirements on the host: git, curl, jq, docker compose.
#
# Env:
#   DEPLOY_BRANCH  branch to track (default: main)
#   DRY_RUN=1      report what would happen without deploying
#
# State (last deployed / last rejected commit) lives outside the repo in
# ${XDG_STATE_HOME:-~/.local/state}/birrcare so `git reset --hard` never touches it.

# Everything runs inside main(): bash reads a script lazily, and `git reset --hard`
# below may rewrite this very file mid-run. A function body is parsed up front.
main() {
  local project_root branch state_dir deployed_file rejected_file
  project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  branch="${DEPLOY_BRANCH:-main}"
  state_dir="${XDG_STATE_HOME:-$HOME/.local/state}/birrcare"
  deployed_file="${state_dir}/deployed-sha"
  rejected_file="${state_dir}/rejected-sha"

  for cmd in git curl jq docker; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      echo "auto-deploy: '$cmd' is required but not installed" >&2
      exit 1
    fi
  done

  mkdir -p "$state_dir"
  cd "$project_root"

  git fetch --quiet --prune origin "$branch"
  local target deployed rejected
  target="$(git rev-parse "origin/${branch}")"
  deployed="$(cat "$deployed_file" 2>/dev/null || true)"
  rejected="$(cat "$rejected_file" 2>/dev/null || true)"

  if [ "$target" = "$deployed" ] || [ "$target" = "$rejected" ]; then
    exit 0 # nothing new
  fi

  # Only deploy commits whose CI/CD workflow run on the push to main succeeded.
  local repo runs status conclusion
  repo="$(git remote get-url origin | sed -E 's#^(https://github\.com/|git@github\.com:)##; s#\.git$##')"
  runs="$(curl -fsSL --max-time 20 \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${repo}/actions/runs?head_sha=${target}&event=push&per_page=10")"
  read -r status conclusion < <(jq -r '
    [.workflow_runs[] | select(.path == ".github/workflows/ci-cd.yml")]
    | sort_by(.created_at) | last
    | if . == null then "missing none" else "\(.status) \(.conclusion // "none")" end
  ' <<<"$runs")

  case "$status/$conclusion" in
    completed/success) ;;
    completed/*)
      echo "auto-deploy: CI for ${target:0:7} finished with '${conclusion}'; not deploying it"
      [ -n "${DRY_RUN:-}" ] || echo "$target" >"$rejected_file"
      exit 0
      ;;
    *)
      echo "auto-deploy: CI for ${target:0:7} is '${status}'; will check again next tick"
      exit 0
      ;;
  esac

  if [ -n "${DRY_RUN:-}" ]; then
    echo "auto-deploy: DRY_RUN - would deploy ${target:0:7} (currently deployed: ${deployed:0:7})"
    exit 0
  fi

  echo "auto-deploy: deploying ${target:0:7} (previous: ${deployed:0:7})"
  # Git-ignored files (data/, .env) are left untouched by reset --hard.
  git reset --hard --quiet "$target"
  if ./scripts/deploy.sh; then
    echo "$target" >"$deployed_file"
  else
    # Don't rebuild a broken commit every tick; the next push to main retries.
    echo "$target" >"$rejected_file"
    echo "auto-deploy: deploy.sh failed for ${target:0:7}" >&2
    exit 1
  fi
}

main "$@"
exit
