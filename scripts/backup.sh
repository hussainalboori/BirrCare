#!/usr/bin/env bash
set -euo pipefail

# Birrcare (رعاية بر) — Encrypted off-site backup (issue #51)
#
# Runs on the host every 6 hours (cron or scripts/systemd/birrcare-backup.*):
#   1. asks the backend for a consistent snapshot of the database (VACUUM INTO;
#      the backend checks integrity_check, so the host needs no sqlite3);
#   2. backs up that snapshot and data/photos/ with restic, encrypted on this
#      machine, to the main repository (Google Drive through rclone, or
#      R2/B2/USB);
#   3. applies retention (keep-last 8, daily 30, weekly 12, monthly 12);
#   4. copies the newest snapshot to a second repository, if one is set;
#   5. once a week, test-restores the newest snapshot and has the backend check
#      it (integrity and row counts);
#   6. writes data/backup/status.json, which the app shows to the Super Admin.
#
# .env is never backed up: it holds SESSION_SECRET.
#
# Usage:
#   scripts/backup.sh                 run a backup now
#   scripts/backup.sh --if-requested  only if "Back up now" was pressed in the app
#
# Settings: ~/.config/birrcare/backup.env (mode 600), see docs/DEPLOYMENT.md §9:
#   RESTIC_REPOSITORY      e.g. rclone:gdrive:birrcare-backups
#   RESTIC_PASSWORD_FILE   e.g. ~/.config/birrcare/restic-password
#   BACKUP_NAME            shown in the app, e.g. "Google Drive"
#   RESTIC_REPOSITORY2     optional second copy (R2, B2 or a USB path)
#   RESTIC_PASSWORD_FILE2  its password file (default: the first one)
#   BACKUP_NAME2           e.g. "Cloudflare R2"
# Env overrides (tests): BACKUP_ENV, BACKUP_DIR, PHOTO_DIR, BACKEND_URL,
# RESTORE_TEST_EVERY_DAYS (default 7), FORCE_RESTORE_TEST=1.

now_utc() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# write_status JQ_FILTER [jq args...]: merge into status.json atomically.
write_status() {
  local filter="$1" tmp
  shift
  tmp="${STATUS_FILE}.tmp.$$"
  { [ -f "$STATUS_FILE" ] && cat "$STATUS_FILE" || echo '{}'; } |
    jq "$@" "$filter" >"$tmp"
  chmod 644 "$tmp"
  mv -f "$tmp" "$STATUS_FILE"
}

# fail MESSAGE [LOGFILE]: record the failure for the app and stop.
fail() {
  local msg="$1" log="${2:-}" code=""
  if [ -n "$log" ] && grep -qi 'invalid_grant' "$log" 2>/dev/null; then
    code="invalid_grant"
    msg="Google sign-in for backups expired. Run: rclone config reconnect gdrive:"
  fi
  echo "backup: $msg" >&2
  write_status '.status = "failed" | .last_attempt_at = $at | .error = $msg | .error_code = $code' \
    --arg at "$ATTEMPT_AT" --arg msg "$msg" --arg code "$code"
  exit 1
}

# load_config FILE: plain KEY=value lines (not run as shell code, so a value
# like `BACKUP_NAME=Google Drive` needs no quotes). Only known keys; a leading
# ~ means the home folder.
load_config() {
  local line key val
  while IFS= read -r line || [ -n "$line" ]; do
    line="${line%$'\r'}"
    case "$line" in '' | '#'*) continue ;; esac
    key="${line%%=*}"
    val="${line#*=}"
    key="$(echo "$key" | tr -d '[:space:]')"
    val="$(echo "$val" | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//; s/^"(.*)"$/\1/; s/^'"'"'(.*)'"'"'$/\1/')"
    case "$val" in '~/'*) val="${HOME}/${val#\~/}" ;; esac
    case "$key" in
      RESTIC_REPOSITORY | RESTIC_PASSWORD_FILE | BACKUP_NAME | RESTIC_REPOSITORY2 | RESTIC_PASSWORD_FILE2 | BACKUP_NAME2 | \
        AWS_ACCESS_KEY_ID | AWS_SECRET_ACCESS_KEY | AWS_DEFAULT_REGION | B2_ACCOUNT_ID | B2_ACCOUNT_KEY | RCLONE_CONFIG)
        export "$key=$val" ;;
      *) echo "backup: ignoring unknown setting '$key' in $1" >&2 ;;
    esac
  done <"$1"
}

main() {
  local project_root only_if_requested=""
  project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  [ "${1:-}" = "--if-requested" ] && only_if_requested=1

  BACKUP_DIR="${BACKUP_DIR:-${project_root}/data/backup}"
  STATUS_FILE="${BACKUP_DIR}/status.json"
  local photo_dir="${PHOTO_DIR:-${project_root}/data/photos}"
  local backend="${BACKEND_URL:-http://127.0.0.1:8080}"
  local config="${BACKUP_ENV:-$HOME/.config/birrcare/backup.env}"
  local restore_every="${RESTORE_TEST_EVERY_DAYS:-7}"

  mkdir -p "$BACKUP_DIR"
  local request="${BACKUP_DIR}/request.json"
  PROCESSING="${BACKUP_DIR}/request.processing"
  if [ -n "$only_if_requested" ] && [ ! -f "$request" ]; then
    exit 0
  fi

  for cmd in curl jq restic; do
    command -v "$cmd" >/dev/null 2>&1 || { echo "backup: '$cmd' is required but not installed" >&2; exit 1; }
  done

  # One run at a time: the 6-hour schedule and "Back up now" can meet.
  if command -v flock >/dev/null 2>&1; then
    exec 9>"${BACKUP_DIR}/.backup.lock"
    flock -n 9 || { echo "backup: another backup is running"; exit 0; }
  fi

  # A "Back up now" request runs once.
  if [ -f "$request" ]; then
    mv -f "$request" "$PROCESSING"
  fi
  trap 'rm -f "$PROCESSING"' EXIT

  if [ ! -f "$config" ]; then
    echo "backup: not set up yet ($config is missing); see docs/DEPLOYMENT.md §9" >&2
    exit 1
  fi
  load_config "$config"
  : "${RESTIC_REPOSITORY:?RESTIC_REPOSITORY is not set in $config}"
  : "${RESTIC_PASSWORD_FILE:?RESTIC_PASSWORD_FILE is not set in $config}"
  export RESTIC_REPOSITORY RESTIC_PASSWORD_FILE

  # Shared secret for the backend's internal backup endpoints (readable by
  # this user and by the backend, which runs as root in Docker).
  local secret_file="${BACKUP_DIR}/secret" secret
  if [ ! -s "$secret_file" ]; then
    (umask 077 && head -c 48 /dev/urandom | base64 | tr -d '/+=\n' >"$secret_file")
  fi
  secret="$(tr -d '\n' <"$secret_file")"

  ATTEMPT_AT="$(now_utc)"
  write_status '.status = "running" | .last_attempt_at = $at' --arg at "$ATTEMPT_AT"

  # 1. Consistent snapshot, checked by the backend.
  local snap staged integrity
  snap="$(curl -fsS --max-time 120 -X POST -H "X-Backup-Secret: ${secret}" "${backend}/internal/backup/snapshot")" ||
    fail "could not get a database snapshot from the app (is it running?)"
  integrity="$(jq -r '.integrity // ""' <<<"$snap")"
  [ "$integrity" = "ok" ] || fail "the database snapshot failed its integrity check: ${integrity:-no answer}"
  staged="${BACKUP_DIR}/$(jq -r '.file' <<<"$snap")"
  [ -f "$staged" ] || fail "the snapshot file is missing: $staged"

  # 2. Encrypted backup. Only the snapshot and the photos: never .env.
  local paths=("$staged") out err summary snapshot_id size
  [ -d "$photo_dir" ] && paths+=("$photo_dir")
  out="$(mktemp)"
  err="$(mktemp)"
  if ! restic backup --json --tag birrcare --host birrcare "${paths[@]}" >"$out" 2>"$err"; then
    cat "$err" >&2
    fail "restic backup to ${BACKUP_NAME:-the main repository} failed: $(tail -n 1 "$err")" "$err"
  fi
  summary="$(jq -c 'select(.message_type == "summary")' "$out" | tail -n 1)"
  snapshot_id="$(jq -r '.snapshot_id // ""' <<<"$summary")"
  size="$(jq -r '.total_bytes_processed // 0' <<<"$summary")"
  [ -n "$snapshot_id" ] || fail "restic did not report a snapshot id"
  local done_at destinations
  done_at="$(now_utc)"
  destinations="$(jq -nc --arg n "${BACKUP_NAME:-Main backup}" --arg at "$done_at" --arg id "$snapshot_id" --argjson size "$size" \
    '[{name: $n, last_success_at: $at, snapshot_id: $id, size_bytes: $size}]')"

  # 3. Retention.
  if ! restic forget --tag birrcare --keep-last 8 --keep-daily 30 --keep-weekly 12 --keep-monthly 12 --prune >/dev/null 2>"$err"; then
    cat "$err" >&2
    fail "restic forget/prune failed: $(tail -n 1 "$err")" "$err"
  fi

  # 4. Second copy.
  if [ -n "${RESTIC_REPOSITORY2:-}" ]; then
    local pw2="${RESTIC_PASSWORD_FILE2:-$RESTIC_PASSWORD_FILE}"
    if ! restic -r "$RESTIC_REPOSITORY2" --password-file "$pw2" copy \
      --from-repo "$RESTIC_REPOSITORY" --from-password-file "$RESTIC_PASSWORD_FILE" "$snapshot_id" >/dev/null 2>"$err" ||
      ! restic -r "$RESTIC_REPOSITORY2" --password-file "$pw2" forget --tag birrcare \
        --keep-last 8 --keep-daily 30 --keep-weekly 12 --keep-monthly 12 --prune >/dev/null 2>>"$err"; then
      cat "$err" >&2
      fail "copy to ${BACKUP_NAME2:-the second repository} failed: $(tail -n 1 "$err")" "$err"
    fi
    destinations="$(jq -c --arg n "${BACKUP_NAME2:-Second copy}" --arg at "$(now_utc)" --arg id "$snapshot_id" --argjson size "$size" \
      '. + [{name: $n, last_success_at: $at, snapshot_id: $id, size_bytes: $size}]' <<<"$destinations")"
  fi

  write_status '.status = "success" | .last_success_at = $at | .destinations = $d | .error = "" | .error_code = ""
                | .counts = $c' \
    --arg at "$done_at" --argjson d "$destinations" --argjson c "$(jq -c '.counts' <<<"$snap")"

  # 5. Weekly restore test.
  local last_test due=""
  last_test="$(jq -r '.last_restore_test_at // ""' "$STATUS_FILE")"
  if [ -n "${FORCE_RESTORE_TEST:-}" ] || [ -z "$last_test" ]; then
    due=1
  else
    local last_s now_s
    last_s="$(date -u -d "$last_test" +%s 2>/dev/null || date -u -j -f %Y-%m-%dT%H:%M:%SZ "$last_test" +%s 2>/dev/null || echo 0)"
    now_s="$(date -u +%s)"
    [ $((now_s - last_s)) -ge $((restore_every * 86400)) ] && due=1
  fi
  if [ -n "$due" ]; then
    local tmp restored verify ok=false why=""
    tmp="$(mktemp -d)"
    if ! restic check --read-data-subset=10% >/dev/null 2>"$err"; then
      why="restic check: $(tail -n 1 "$err")"
    elif ! restic restore "$snapshot_id" --target "$tmp" >/dev/null 2>"$err"; then
      why="restore: $(tail -n 1 "$err")"
    else
      restored="$(find "$tmp" -type f -name birrcare.db | head -n 1)"
      if [ -z "$restored" ]; then
        why="the restored snapshot has no database"
      else
        mkdir -p "${BACKUP_DIR}/restore-test"
        cp -f "$restored" "${BACKUP_DIR}/restore-test/birrcare.db"
        chmod 644 "${BACKUP_DIR}/restore-test/birrcare.db"
        if verify="$(curl -fsS --max-time 120 -X POST -H "X-Backup-Secret: ${secret}" "${backend}/internal/backup/verify")"; then
          # The restored copy must be sound and hold at least the rows the
          # snapshot had: nothing lost on the way to the cloud and back.
          if [ "$(jq -r '.integrity' <<<"$verify")" = "ok" ] &&
            jq -e --argjson want "$(jq -c '.counts' <<<"$snap")" \
              '.counts.medication_logs >= $want.medication_logs and .counts.vitals_logs >= $want.vitals_logs' <<<"$verify" >/dev/null; then
            ok=true
          else
            why="the restored database is damaged or missing rows: $(jq -c . <<<"$verify")"
          fi
        else
          why="the app could not check the restored database"
        fi
        rm -f "${BACKUP_DIR}/restore-test/birrcare.db"
      fi
    fi
    rm -rf "$tmp"
    [ -n "$why" ] && echo "backup: restore test failed: $why" >&2
    write_status '.last_restore_test_at = $at | .last_restore_test_ok = $ok | .restore_test_error = $why' \
      --arg at "$(now_utc)" --argjson ok "$ok" --arg why "$why"
  fi

  rm -f "$out" "$err"
  echo "backup: ${snapshot_id:0:8} saved to ${BACKUP_NAME:-the main repository}${RESTIC_REPOSITORY2:+ and ${BACKUP_NAME2:-the second repository}}"
}

main "$@"
