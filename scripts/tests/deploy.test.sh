#!/usr/bin/env bash
# Tests for scripts/deploy.sh with stub docker / curl / git: steps, checks,
# automatic rollback (images, then the database), and the everyday commands.
# Usage: bash scripts/tests/deploy.test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fails=0
NEW=2222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
OLD=1111111

setup() {
  W="$(mktemp -d)"
  R="$W/repo"
  mkdir -p "$R/scripts" "$R/data" "$W/bin"
  cp "$ROOT/scripts/deploy.sh" "$ROOT/scripts/setup-env.sh" "$ROOT/scripts/env.schema" "$ROOT/scripts/emergency-numbers" "$R/scripts/"
  cp "$ROOT/.env.example" "$R/"
  echo live-db >"$R/data/birrcare.db"
  cat >"$R/.env" <<'ENV'
APP_TIMEZONE=Asia/Bahrain
EMERGENCY_NUMBER=999
SESSION_SECRET=0123456789abcdefghij0123456789abcdefghij0123456789abcdefghij
AUTH_DEV_FALLBACK=false
CF_ACCESS_TRUST_EMAIL_HEADER=false
ENV
  touch "$W/running" "$W/has-prev"
  : >"$W/docker-calls"

  # docker: behaviour switched by files in $W.
  cat >"$W/bin/docker" <<EOF
#!/usr/bin/env bash
echo "\$*" >>"$W/docker-calls"
case "\$1" in
  info) [ -f "$W/docker-down" ] && exit 1; exit 0 ;;
  inspect) [ -f "$W/running" ] || exit 1; case "\$*" in *birrcare_backend*) echo sha256:oldback ;; *) echo sha256:oldfront ;; esac ;;
  tag) exit 0 ;;
  pull) [ -f "$W/pull-ok" ] || { echo "denied: requires authentication"; exit 1; } ;;
  image)
    case "\$2" in
      inspect)
        case "\$*" in
          *ghcr.io*) cat "$W/pulled-version" 2>/dev/null || echo "GIT_SHA=${NEW}" ;;
          *) [ -f "$W/has-prev" ] || exit 1; case "\$*" in *'{{range'*) echo "GIT_SHA=${OLD}cccc" ;; esac ;;
        esac ;;
      prune | rm) exit 0 ;;
    esac ;;
  builder) exit 0 ;;
  compose)
    case "\$2" in
      build) [ -f "$W/build-fails" ] && { echo "ERROR: go build failed"; exit 1; }; echo "#12 [builder 4/6] RUN go build" ;;
      up) case "\$*" in *--force-recreate*) touch "$W/rolled-back" ;; esac ;;
      logs) echo "birrcare_backend | panic: migration failed" ;;
      ps) printf 'birrcare_backend\tUp 2 minutes\nbirrcare_frontend\tUp 2 minutes\n' ;;
    esac ;;
esac
exit 0
EOF
  # curl: the app's health. New version healthy unless "new-broken"; after a
  # rollback the old version, unless "old-broken" (then only with the restored DB).
  cat >"$W/bin/curl" <<EOF
#!/usr/bin/env bash
url="\${@: -1}"
case "\$url" in
  */internal/backup/snapshot)
    mkdir -p "$R/data/backup/staging" && echo snapshot-db >"$R/data/backup/staging/birrcare.db"
    echo '{"integrity":"ok"}' ;;
  http://127.0.0.1:8080/api/v1/health)
    if [ -f "$W/rolled-back" ]; then
      if [ -f "$W/old-broken" ] && ! grep -q snapshot-db "$R/data/birrcare.db"; then exit 7; fi
      echo '{"status":"healthy","version":"${OLD}"}'
    else
      [ -f "$W/new-broken" ] && exit 7
      echo '{"status":"healthy","version":"${NEW:0:7}"}'
    fi ;;
  http://127.0.0.1:3000) exit 0 ;;
  https://care.example.com/api/v1/health) [ -f "$W/tunnel-down" ] && exit 6; echo '{"version":"${NEW:0:7}"}' ;;
  *) exit 7 ;;
esac
EOF
  printf '#!/usr/bin/env bash\n[ "$1" = remote ] && { echo https://github.com/HussainAlboori/BirrCare.git; exit 0; }\necho %s\n' "$NEW" >"$W/bin/git"
  chmod +x "$W/bin/"* "$R/scripts/"*.sh
}

run() {
  (cd "$R" && env PATH="$W/bin:$PATH" BIRRCARE_MIN_FREE_GB="${MIN_GB:-0}" BIRRCARE_HEALTH_TIMEOUT=3 \
    bash ./scripts/deploy.sh "$@" </dev/null >"$W/out" 2>&1) && echo 0 >"$W/rc" || echo $? >"$W/rc"
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
dcalls() { grep -c -- "$1" "$W/docker-calls" || true; }
out_has() { grep -c -- "$1" "$W/out" || true; }

# 1. A good update: seven short steps, the full output in a log.
setup
run
check "good: exit 0" "$(cat "$W/rc")" "0"
check "good: seven steps" "$(grep -cE '^\[[1-7]/7\]' "$W/out")" "7"
check "good: no docker build noise on screen" "$(out_has 'RUN go build')" "0"
check "good: build output in the log" "$(grep -l 'RUN go build' "$R"/data/logs/deploy-*.log | wc -l | tr -d ' ')" "1"
check "good: previous version kept" "$(dcalls 'tag sha256:oldback birrcare-backend:previous')" "1"
check "good: previous kept before building" "$(grep -nE '^(tag sha256:oldback|compose build)' "$W/docker-calls" | cut -d: -f2 | cut -d' ' -f1 | tr '\n' ' ')" "tag compose "
check "good: new version confirmed" "$(out_has 'version 2222222 is answering')" "1"
check "good: tried the CI image first" "$(dcalls 'pull ghcr.io/hussainalboori/birrcare-backend:sha-2222222')" "1"
check "good: built here when it can't be downloaded" "$(out_has 'built here')" "1"
check "good: database copied first" "$(ls "$R"/data/backups/ | grep -c birrcare_backup_)" "1"
check "good: says done" "$(out_has 'Done in')" "1"
rm -rf "$W"

# 1b. The CI-built images for this version download: no build at all.
setup
touch "$W/pull-ok"
run
check "pull: exit 0" "$(cat "$W/rc")" "0"
check "pull: says downloaded" "$(out_has 'downloaded')" "1"
check "pull: nothing built" "$(dcalls 'compose build')" "0"
check "pull: used as the local images" "$(dcalls 'tag ghcr.io/hussainalboori/birrcare-frontend:sha-2222222 birrcare-frontend:local')" "1"
check "pull: started without building" "$(dcalls 'compose up -d --no-build --remove-orphans')" "1"
check "pull: registry names dropped afterwards" "$(dcalls 'image rm ghcr.io/hussainalboori/birrcare-backend:sha-2222222')" "1"
rm -rf "$W"

# 1c. A downloaded image of another version is not used.
setup
touch "$W/pull-ok"
echo "GIT_SHA=9999999" >"$W/pulled-version"
run
check "wrong pulled version: built instead" "$(dcalls 'compose build')" "1"
rm -rf "$W"

# 1d. --pull: download or nothing; --build: never download.
setup
run --pull
check "--pull without images: exit 1" "$(cat "$W/rc")" "1"
check "--pull without images: nothing built" "$(dcalls 'compose build')" "0"
check "--pull without images: nothing started" "$(dcalls 'compose up')" "0"
: >"$W/docker-calls"
touch "$W/pull-ok"
run --build
check "--build: no download" "$(dcalls '^pull')" "0"
check "--build: built" "$(dcalls 'compose build')" "1"
rm -rf "$W"

# 2. The new version never answers: back to the previous images by itself.
setup
touch "$W/new-broken"
run
check "rollback: exit 4" "$(cat "$W/rc")" "4"
check "rollback: previous images restored" "$(dcalls 'tag birrcare-backend:previous birrcare-backend:local')" "1"
check "rollback: restarted without building" "$(dcalls 'compose up -d --no-build --force-recreate')" "1"
check "rollback: says so" "$(out_has 'rolled back: the app is running the previous version (1111111)')" "1"
check "rollback: shows the app's error" "$(out_has 'panic: migration failed')" "1"
check "rollback: database untouched" "$(cat "$R/data/birrcare.db")" "live-db"
rm -rf "$W"

# 3. The previous version doesn't start either: the database from before the update.
setup
touch "$W/new-broken" "$W/old-broken"
run
check "db restore: exit 5" "$(cat "$W/rc")" "5"
check "db restore: database from before the update" "$(cat "$R/data/birrcare.db")" "snapshot-db"
check "db restore: updated database kept aside" "$(cat "$R"/data/birrcare.db.failed-* 2>/dev/null)" "live-db"
check "db restore: says where" "$(out_has 'is kept in data/birrcare.db.failed-')" "1"
rm -rf "$W"

# 4. First install fails: nothing to go back to.
setup
rm "$W/running"
touch "$W/new-broken"
run
check "first install fails: exit 6" "$(cat "$W/rc")" "6"
rm -rf "$W"

# 5. Not enough disk: stop before building.
setup
MIN_GB=999999 run
check "disk: exit 1" "$(cat "$W/rc")" "1"
check "disk: explained" "$(out_has 'GB needed')" "1"
check "disk: old build cache cleared first" "$(dcalls 'builder prune -af')" "1"
check "disk: nothing built" "$(dcalls 'compose build')" "0"
rm -rf "$W"

# 6. Docker not running.
setup
touch "$W/docker-down"
run
check "docker down: exit 1" "$(cat "$W/rc")" "1"
check "docker down: tells how to fix" "$(out_has 'sudo systemctl start docker')" "1"
rm -rf "$W"

# 7. The build fails: nothing changes.
setup
touch "$W/build-fails"
run
check "build fails: exit 1" "$(cat "$W/rc")" "1"
check "build fails: not started" "$(dcalls 'compose up')" "0"
check "build fails: log tail shown" "$(out_has 'go build failed')" "1"
rm -rf "$W"

# 8. One deploy at a time.
setup
if command -v flock >/dev/null 2>&1; then
  (cd "$R" && flock data/.deploy.lock sleep 3) &
  sleep 0.5
else
  mkdir "$R/data/.deploy.lock.d"
fi
run
check "lock: exit 1" "$(cat "$W/rc")" "1"
check "lock: explained" "$(out_has 'Another deploy or update is running')" "1"
check "lock: nothing built" "$(dcalls 'compose build')" "0"
wait 2>/dev/null || true
rm -rf "$W"

# 9. The public address is checked too; a broken tunnel is a warning, not a failure.
setup
echo "PUBLIC_URL=https://care.example.com" >>"$R/.env"
run
check "public: reachable" "$(out_has 'reachable from the internet: https://care.example.com')" "1"
touch "$W/tunnel-down"
run
check "public down: still exit 0" "$(cat "$W/rc")" "0"
check "public down: warned" "$(out_has 'check the tunnel / DNS')" "1"
rm -rf "$W"

# 9b. Never with sudo; and files left behind by an earlier sudo run are explained.
setup
printf '#!/usr/bin/env bash\n[ "$1" = "-u" ] && [ $# -eq 1 ] && { echo 0; exit 0; }\n/usr/bin/id "$@"\n' >"$W/bin/id"
chmod +x "$W/bin/id"
(cd "$R" && env PATH="$W/bin:$PATH" SUDO_USER=runner BIRRCARE_MIN_FREE_GB=0 bash ./scripts/deploy.sh </dev/null >"$W/out" 2>&1) && echo 0 >"$W/rc" || echo $? >"$W/rc"
check "sudo: refused" "$(cat "$W/rc")" "1"
check "sudo: tells to run as the user" "$(out_has "Don't run this with sudo. Run it as runner")" "1"
check "sudo: nothing built" "$(dcalls 'compose build')" "0"
rm -f "$W/bin/id"
chmod 400 "$R/.env"
run
check "root-owned files: refused" "$(cat "$W/rc")" "1"
check "root-owned files: one-time fix shown" "$(out_has 'sudo chown -R')" "1"
check "root-owned files: fix uses full paths" "$(out_has "/repo/.env\" \"")" "1"
chmod 600 "$R/.env"
rm -rf "$W"

# 10. status
setup
run status
check "status: exit 0" "$(cat "$W/rc")" "0"
check "status: version" "$(out_has 'running, version 2222222')" "1"
check "status: previous kept" "$(out_has 'Previous version kept for rollback: 1111111')" "1"
check "status: backups" "$(out_has 'not set up')" "1"
rm -rf "$W"

# 11. rollback on request
setup
run rollback --yes
check "rollback cmd: exit 0" "$(cat "$W/rc")" "0"
check "rollback cmd: switched" "$(dcalls 'compose up -d --no-build --force-recreate')" "1"
run rollback
check "rollback cmd without terminal or --yes: refused" "$(cat "$W/rc")" "1"
rm -f "$W/has-prev"
run rollback --yes
check "rollback cmd: nothing kept" "$(out_has 'no previous version kept')" "1"
rm -rf "$W"

if [ "$fails" -gt 0 ]; then
  echo "$fails check(s) failed"
  exit 1
fi
echo "all deploy checks passed"
