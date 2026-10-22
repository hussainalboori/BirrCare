#!/usr/bin/env bash
# Tests for scripts/backup.sh (issue #51) with stub restic / curl.
# Usage: bash scripts/tests/backup.test.sh   (needs jq)
set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/backup.sh"
fails=0

# setup: fresh dirs, config and stubs. Sets $W.
setup() {
  W="$(mktemp -d)"
  mkdir -p "$W/bin" "$W/data/backup/staging" "$W/data/photos" "$W/cfg"
  echo "box photo" >"$W/data/photos/a.jpg"
  echo "SESSION_SECRET=do-not-upload" >"$W/.env"
  echo "s3cret-password" >"$W/cfg/pw"
  cat >"$W/cfg/backup.env" <<EOF
RESTIC_REPOSITORY=rclone:gdrive:birrcare-backups
RESTIC_PASSWORD_FILE=$W/cfg/pw
BACKUP_NAME=Google Drive
EOF
  echo ok >"$W/integrity"
  echo '{"medication_logs":5,"vitals_logs":3}' >"$W/restored-counts"
  : >"$W/restic-calls"
  : >"$W/restic-fail"

  # curl: the backend's snapshot / verify endpoints.
  cat >"$W/bin/curl" <<EOF
#!/usr/bin/env bash
url="\${@: -1}"
case "\$url" in
  */internal/backup/snapshot)
    echo "fake db" >"$W/data/backup/staging/birrcare.db"
    echo '{"file":"staging/birrcare.db","integrity":"'"\$(cat "$W/integrity")"'","counts":{"medication_logs":5,"vitals_logs":3}}' ;;
  */internal/backup/verify)
    [ -f "$W/data/backup/restore-test/birrcare.db" ] || exit 22
    echo '{"integrity":"ok","counts":'"\$(cat "$W/restored-counts")"'}' ;;
  *) exit 7 ;;
esac
EOF
  # restic: records every call; "backup" prints a summary; "restore" writes the file.
  cat >"$W/bin/restic" <<EOF
#!/usr/bin/env bash
echo "\$*" >>"$W/restic-calls"
sub=""
for a in "\$@"; do case "\$a" in backup|forget|copy|check|restore) sub="\$a"; break ;; esac; done
if grep -qx "\$sub" "$W/restic-fail" 2>/dev/null; then
  echo "Fatal: oauth2: cannot fetch token: 400 Bad Request Response: {\"error\": \"invalid_grant\"}" >&2
  exit 1
fi
case "\$sub" in
  backup) echo '{"message_type":"status","percent_done":1}'
          echo '{"message_type":"summary","snapshot_id":"a1b2c3d4e5f6","total_bytes_processed":4096}' ;;
  restore) t=""; prev=""; for a in "\$@"; do [ "\$prev" = "--target" ] && t="\$a"; prev="\$a"; done
           mkdir -p "\$t/home/runner/birrcare/data/backup/staging" && echo "fake db" >"\$t/home/runner/birrcare/data/backup/staging/birrcare.db" ;;
esac
exit 0
EOF
  chmod +x "$W/bin/"*
}

run() {
  (cd "$W" && PATH="$W/bin:$PATH" BACKUP_ENV="$W/cfg/backup.env" BACKUP_DIR="$W/data/backup" PHOTO_DIR="$W/data/photos" \
    bash "$SCRIPT" "$@" >"$W/out" 2>&1) && echo 0 >"$W/rc" || echo $? >"$W/rc"
}

status() { jq -r "$1" "$W/data/backup/status.json"; }
calls() { grep -c "${1}" "$W/restic-calls" || true; }

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

# 1. integrity_check fails → nothing uploaded, status failed.
setup
echo "*** in database main *** Page 7: btree corrupted" >"$W/integrity"
run
check "bad snapshot: exit code" "$(cat "$W/rc")" "1"
check "bad snapshot: restic backup not called" "$(calls '^backup')" "0"
check "bad snapshot: status failed" "$(status .status)" "failed"
check "bad snapshot: error says integrity" "$(status .error | grep -c integrity)" "1"
rm -rf "$W"

# 2. success → snapshot id recorded; .env never handed to restic.
setup
run
check "success: exit code" "$(cat "$W/rc")" "0"
check "success: status" "$(status .status)" "success"
check "success: snapshot id" "$(status '.destinations[0].snapshot_id')" "a1b2c3d4e5f6"
check "success: destination name" "$(status '.destinations[0].name')" "Google Drive"
check "success: last_success_at set" "$(status '.last_success_at | test("T")')" "true"
check "success: .env never backed up" "$(grep -c '\.env' "$W/restic-calls" || true)" "0"
check "success: photos backed up" "$(grep '^backup' "$W/restic-calls" | grep -c 'data/photos')" "1"
check "success: snapshot backed up" "$(grep '^backup' "$W/restic-calls" | grep -c 'staging/birrcare.db')" "1"
check "success: retention applied" "$(grep -c 'forget --tag birrcare --keep-last 8 --keep-daily 30 --keep-weekly 12 --keep-monthly 12 --prune' "$W/restic-calls")" "1"
check "success: first run does the restore test" "$(status .last_restore_test_ok)" "true"
check "success: restore-test copy cleaned up" "$([ -e "$W/data/backup/restore-test/birrcare.db" ] && echo left || echo gone)" "gone"
check "success: secret created, private" "$(stat -c %a "$W/data/backup/secret" 2>/dev/null || stat -f %Lp "$W/data/backup/secret")" "600"
: >"$W/restic-calls"
run
check "restore test not repeated within the week" "$(calls '^check')" "0"
rm -rf "$W"

# 3. "Back up now": request.json runs once and is removed.
setup
run --if-requested
check "no request: nothing runs" "$(calls '^backup')" "0"
echo '{"action":"backup"}' >"$W/data/backup/request.json"
run --if-requested
check "request: backup ran" "$(calls '^backup')" "1"
check "request: request.json removed" "$(ls "$W/data/backup" | grep -c '^request' || true)" "0"
run --if-requested
check "request: runs only once" "$(calls '^backup')" "1"
rm -rf "$W"

# 4. Google token expired → invalid_grant reported for the app's alert.
setup
echo backup >"$W/restic-fail"
run
check "invalid_grant: status failed" "$(status .status)" "failed"
check "invalid_grant: error code" "$(status .error_code)" "invalid_grant"
check "invalid_grant: reconnect hint" "$(status .error | grep -c 'rclone config reconnect gdrive:')" "1"
rm -rf "$W"

# 5. Restore test that comes back with fewer rows → restore test failed.
setup
echo '{"medication_logs":4,"vitals_logs":3}' >"$W/restored-counts"
run
check "short restore: backup still recorded" "$(status .status)" "success"
check "short restore: restore test failed" "$(status .last_restore_test_ok)" "false"
rm -rf "$W"

# 6. Second repository gets a copy.
setup
printf 'RESTIC_REPOSITORY2=s3:https://acct.r2.cloudflarestorage.com/birrcare\nBACKUP_NAME2=Cloudflare R2\n' >>"$W/cfg/backup.env"
run
check "second copy: copied" "$(grep -c 'copy --from-repo rclone:gdrive:birrcare-backups' "$W/restic-calls")" "1"
check "second copy: listed" "$(status '.destinations[1].name')" "Cloudflare R2"
rm -rf "$W"

# 7. Not set up yet → clear message, no status written.
setup
rm "$W/cfg/backup.env"
run
check "not set up: exit code" "$(cat "$W/rc")" "1"
check "not set up: no status.json" "$([ -e "$W/data/backup/status.json" ] && echo yes || echo no)" "no"
rm -rf "$W"

if [ "$fails" -gt 0 ]; then
  echo "$fails check(s) failed"
  exit 1
fi
echo "all backup checks passed"
