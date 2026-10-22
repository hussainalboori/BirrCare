#!/usr/bin/env bash
set -euo pipefail

# Birrcare (رعاية بر) — Production Deployment & Self-Healing Script
# Usage: ./scripts/deploy.sh

echo "=========================================================="
echo " Birrcare (رعاية بر) — Deploying Updates"
echo " Date: $(date -u +"%Y-%m-%d %H:%M:%SZ")"
echo "=========================================================="

# Ensure script is run from project root
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${PROJECT_ROOT}"

# 0. Pre-flight: the backend refuses to start in production without
# APP_TIMEZONE (issue #26). Check before rebuilding, so a missing value leaves
# the running version up instead of replacing it with one that cannot start.
tz="${APP_TIMEZONE:-}"
if [ -z "${tz}" ] && [ -f .env ]; then
  tz="$(grep -E '^[[:space:]]*APP_TIMEZONE=' .env | tail -n 1 | cut -d= -f2- | tr -d "\"' " || true)"
fi
if [ -z "${tz}" ]; then
  echo "ERROR: APP_TIMEZONE is not set. Add it to .env (e.g. APP_TIMEZONE=Asia/Riyadh)" >&2
  echo "       and run this script again. Nothing was changed; the running version stays up." >&2
  exit 1
fi
echo "--> Household time zone: ${tz}"

# 1. Automated Safety Backup of SQLite Database
if [ -f "data/birrcare.db" ]; then
  BACKUP_DIR="data/backups"
  mkdir -p "${BACKUP_DIR}"
  BACKUP_FILE="${BACKUP_DIR}/birrcare_backup_$(date +%Y%m%d_%H%M%S).db"
  echo "--> Creating safety database snapshot at ${BACKUP_FILE}..."

  # A consistent copy only (issue #51): with WAL, a plain cp of birrcare.db can
  # miss the newest changes. Ask the running app for a snapshot (VACUUM INTO,
  # integrity-checked); if the app is down, use sqlite3's online backup.
  SECRET_FILE="data/backup/secret"
  if [ ! -s "${SECRET_FILE}" ]; then
    mkdir -p data/backup
    (umask 077 && head -c 48 /dev/urandom | base64 | tr -d '/+=\n' >"${SECRET_FILE}")
  fi
  SNAP=""
  SNAP="$(curl -fsS --max-time 120 -X POST -H "X-Backup-Secret: $(tr -d '\n' <"${SECRET_FILE}")" \
    http://127.0.0.1:8080/internal/backup/snapshot 2>/dev/null)" || SNAP=""
  if printf '%s' "${SNAP}" | grep -q '"integrity":"ok"' && [ -f data/backup/staging/birrcare.db ]; then
    cp data/backup/staging/birrcare.db "${BACKUP_FILE}"
  elif command -v sqlite3 >/dev/null 2>&1; then
    sqlite3 data/birrcare.db ".backup '${BACKUP_FILE}'"
  elif [ -n "${SKIP_DB_BACKUP:-}" ]; then
    echo "    WARNING: no consistent snapshot possible; SKIP_DB_BACKUP is set, continuing without one." >&2
    BACKUP_FILE=""
  else
    echo "ERROR: could not take a consistent database backup: the app did not answer and sqlite3" >&2
    echo "       is not installed. Start the app (docker compose up -d) or install sqlite3, then run" >&2
    echo "       this again. Nothing was changed; the running version stays up." >&2
    echo "       (To update without a backup anyway: SKIP_DB_BACKUP=1 ./scripts/deploy.sh)" >&2
    exit 1
  fi
  [ -n "${BACKUP_FILE}" ] && echo "    Backup created successfully."
  
  # Retain only the last 10 backups to preserve disk space
  ls -tp "${BACKUP_DIR}"/birrcare_backup_*.db 2>/dev/null | grep -v '/$' | tail -n +11 | xargs -I {} rm -- {} 2>/dev/null || true
fi

# 2. Build or Pull latest Docker containers
# The running commit goes into both images, so /api/v1/health reports it and
# the app can confirm an update took effect (issue #43).
GIT_SHA="${GIT_SHA:-$(git rev-parse HEAD 2>/dev/null || echo dev)}"
export GIT_SHA
# Leftovers from a build that failed half-way (step 4 below never ran).
docker image prune -f >/dev/null 2>&1 || true
docker builder prune -af >/dev/null 2>&1 || true
echo "--> Rebuilding and starting Docker services (version ${GIT_SHA:0:7})..."
docker compose build --pull
docker compose up -d --remove-orphans

# 3. Health check verification
echo "--> Verifying backend and frontend health..."
MAX_ATTEMPTS=20
ATTEMPT=0
BACKEND_HEALTHY=false

while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
  if curl -sf http://127.0.0.1:8080/api/v1/health >/dev/null 2>&1; then
    BACKEND_HEALTHY=true
    break
  fi
  ATTEMPT=$((ATTEMPT + 1))
  sleep 1
done

if [ "$BACKEND_HEALTHY" = true ]; then
  echo "    ✔ Backend is healthy on http://127.0.0.1:8080"
else
  echo "    ✖ Backend health check failed after ${MAX_ATTEMPTS}s"
  docker compose logs --tail=30 birrcare-backend
  exit 1
fi

ATTEMPT=0
FRONTEND_HEALTHY=false
while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
  if curl -sf http://127.0.0.1:3000 >/dev/null 2>&1; then
    FRONTEND_HEALTHY=true
    break
  fi
  ATTEMPT=$((ATTEMPT + 1))
  sleep 1
done

if [ "$FRONTEND_HEALTHY" = true ]; then
  echo "    ✔ Frontend is healthy on http://127.0.0.1:3000"
else
  echo "    ✖ Frontend health check failed after ${MAX_ATTEMPTS}s"
  docker compose logs --tail=30 birrcare-frontend
  exit 1
fi

# 4. Clean up after the build, so the disk doesn't fill up update by update.
# Each build leaves roughly 2-3 GB: the previous app images (now untagged) and
# Docker's build cache. Only unused images and cache are removed; the running
# containers, the database and the backups are untouched. The base images
# (golang, node, alpine) are kept, so the next build doesn't download them again.
echo "--> Cleaning up old images and build cache..."
docker image prune -f >/dev/null 2>&1 || true
docker builder prune -af >/dev/null 2>&1 || true
df -h . | awk 'NR==2 {print "    Disk: " $3 " used of " $2 " (" $5 ")"}'

echo "=========================================================="
echo " ✔ Birrcare successfully deployed and verified!"
echo "=========================================================="
