#!/usr/bin/env bash
set -euo pipefail

# Birrcare (رعاية بر) — a new server in one command
#
#   ./scripts/setup.sh
#
# 1. Installs the tools if anything is missing (asks for your password once,
#    through sudo: see scripts/install-deps.sh).
# 2. Asks the settings questions and starts the app (scripts/deploy.sh).
# Run it as your normal user, not with sudo. Options are passed to deploy.sh
# (e.g. --lang ar). On a Mac the installer runs without sudo (Homebrew); on
# Windows run this inside WSL2 (see docs/DEPLOYMENT.md). Test hook:
# BIRRCARE_UNAME (Linux|Darwin).

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [ "$(id -u)" = "0" ]; then
  echo "Run this as your normal user (it uses sudo itself, once, when it has to install something)." >&2
  exit 1
fi

missing=""
for c in git curl jq docker; do command -v "$c" >/dev/null 2>&1 || missing="$missing $c"; done
if [ -z "$missing" ] && ! docker compose version >/dev/null 2>&1; then missing=" docker-compose"; fi
if [ -n "$missing" ] || ! command -v restic >/dev/null 2>&1; then
  echo "Installing what this server needs:${missing:- (optional backup tools)}"
  if [ "${BIRRCARE_UNAME:-$(uname -s)}" = "Darwin" ]; then
    ./scripts/install-deps.sh # Homebrew refuses to run as root
  else
    sudo ./scripts/install-deps.sh
  fi
fi

# Just added to the docker group? That only applies to new logins; run the
# deploy with it right away instead of asking you to log out and in.
me="$(id -un)"
if ! docker info >/dev/null 2>&1 && command -v sg >/dev/null 2>&1 &&
  getent group docker 2>/dev/null | grep -qE "[:,]${me}(,|$)"; then
  exec sg docker -c "$(printf '%q ' ./scripts/deploy.sh "$@")"
fi
exec ./scripts/deploy.sh "$@"
