#!/usr/bin/env bash
set -euo pipefail

# Birrcare (رعاية بر) — Deploy and look after the server
#
#   ./scripts/deploy.sh             build and start the current version (default)
#   ./scripts/deploy.sh status      versions, containers, health, disk, backups, timers
#   ./scripts/deploy.sh logs [backend|frontend] [-f]   recent app logs
#   ./scripts/deploy.sh restart     restart without rebuilding (applies .env changes)
#   ./scripts/deploy.sh rollback    go back to the version before the last deploy
#
# Options: --verbose (show everything, not just the steps), --yes (don't ask),
#          --build (always build here) / --pull (only download), --lang ar|en.
#
# By default a deploy downloads the images CI already built and checked for
# exactly this version (ghcr.io/<owner>/birrcare-*:sha-<version>), which takes
# seconds; if they can't be downloaded (private packages, no internet, an ARM
# board such as a Raspberry Pi), it builds here instead, as before.
#
# A deploy shows seven short steps; the full output goes to data/logs/. If the
# new version doesn't come up, it goes back to the previous one by itself (and,
# if that doesn't come up either, to the database from before the update).
#
# Exit codes (scripts/auto-deploy.sh reads them):
#   0 done
#   1 stopped before changing anything (a check failed); the running version stays up
#   3 a setting needs an answer (run this in a terminal); nothing was changed
#   4 the new version failed; rolled back to the previous version
#   5 the new version failed; rolled back, including the database from before the update
#   6 the new version failed and the app could not be brought back; see the log

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${PROJECT_ROOT}"

CMD="deploy"
GET_MODE="${BIRRCARE_GET_MODE:-auto}" # auto | pull | build
VERBOSE=""
ASSUME_YES=""
LANG_SEL=""
LOG_SERVICE=""
LOG_FOLLOW=""
while [ $# -gt 0 ]; do
  case "$1" in
    deploy | status | logs | restart | rollback) CMD="$1" ;;
    backend | frontend) LOG_SERVICE="birrcare-$1" ;;
    -f | --follow) LOG_FOLLOW=1 ;;
    --verbose | -v) VERBOSE=1 ;;
    --build) GET_MODE="build" ;;
    --pull) GET_MODE="pull" ;;
    --yes | -y) ASSUME_YES=1 ;;
    --lang) LANG_SEL="$2"; shift ;;
    -h | --help | help) sed -n '3,27p' "$0"; exit 0 ;;
    *) echo "deploy: unknown option $1 (see ./scripts/deploy.sh --help)" >&2; exit 1 ;;
  esac
  shift
done
if [ -z "$LANG_SEL" ]; then
  case "${LANG:-}${LC_ALL:-}" in ar*) LANG_SEL=ar ;; *) LANG_SEL=en ;; esac
fi

HEALTH_TIMEOUT="${BIRRCARE_HEALTH_TIMEOUT:-120}"
MIN_FREE_GB="${BIRRCARE_MIN_FREE_GB:-3}"
BACKEND_URL="http://127.0.0.1:8080"
FRONTEND_URL="http://127.0.0.1:3000"
# Where CI publishes the images: ghcr.io/<GitHub owner of this checkout>.
registry_prefix() {
  if [ -n "${BIRRCARE_REGISTRY:-}" ]; then
    printf '%s' "$BIRRCARE_REGISTRY"
    return
  fi
  local owner
  owner="$(git remote get-url origin 2>/dev/null | sed -nE 's#^(https://github\.com/|git@github\.com:)([^/]+)/.*#\2#p' | tr '[:upper:]' '[:lower:]')"
  if [ -n "$owner" ]; then printf 'ghcr.io/%s' "$owner"; fi
  return 0 # no GitHub remote: no download, build here
}
# docker-compose.yml names the images, so the previous ones can be kept.
IMG_BACKEND="birrcare-backend:local"
IMG_FRONTEND="birrcare-frontend:local"
PREV_BACKEND="birrcare-backend:previous"
PREV_FRONTEND="birrcare-frontend:previous"
C_BACKEND="birrcare_backend"
C_FRONTEND="birrcare_frontend"

t() { if [ "$LANG_SEL" = "ar" ]; then printf '%s' "$2"; else printf '%s' "$1"; fi; }
say() { printf '%s\n' "$*"; }
err() { printf '%s\n' "$*" >&2; }
IS_TTY=""
[ -t 1 ] && IS_TTY=1
mark_ok() { if [ -n "$IS_TTY" ]; then printf '\033[32m✔\033[0m'; else printf '✔'; fi; }
mark_bad() { if [ -n "$IS_TTY" ]; then printf '\033[31m✖\033[0m'; else printf '✖'; fi; }
mark_warn() { if [ -n "$IS_TTY" ]; then printf '\033[33m⚠\033[0m'; else printf '⚠'; fi; }

# ---------------------------------------------------------------------------
# Log and steps
# ---------------------------------------------------------------------------
mkdir -p data/logs
case "$CMD" in
  status | logs) LOG="/dev/null" ;; # nothing worth keeping
  *)
    LOG="data/logs/${CMD}-$(date +%Y%m%d-%H%M%S).log"
    : >"$LOG"
    # Keep the last 20 logs of each kind.
    { ls -1t data/logs/"${CMD}"-*.log 2>/dev/null || true; } | tail -n +21 | while IFS= read -r old; do rm -f -- "$old"; done
    ;;
esac

# run CMD…: output to the log (and the screen with --verbose); keeps the exit code.
run() {
  echo "\$ $*" >>"$LOG"
  if [ -n "$VERBOSE" ]; then
    "$@" 2>&1 | tee -a "$LOG"
    return "${PIPESTATUS[0]}"
  fi
  "$@" >>"$LOG" 2>&1
}

STEP=0
TOTAL=7
step() {
  STEP=$((STEP + 1))
  local label="$1" dots=".........................."
  printf '[%d/%d] %s %s ' "$STEP" "$TOTAL" "$label" "${dots:0:$((24 - ${#label} > 2 ? 24 - ${#label} : 2))}"
}
done_step() { printf '%s %s\n' "$(mark_ok)" "${1:-}"; }
fail_step() { printf '%s %s\n' "$(mark_bad)" "${1:-}"; }

show_log_tail() {
  say ""
  say "$(t "Last lines of the log:" "آخر أسطر السجل:")"
  tail -n 30 "$LOG" | sed 's/^/  │ /'
  say "$(t "Full log:" "السجل الكامل:") ${PROJECT_ROOT}/${LOG}"
}

# One deploy at a time (a manual run and an install from the app can meet).
take_lock() {
  if command -v flock >/dev/null 2>&1; then
    exec 8>"data/.deploy.lock"
    if ! flock -n 8; then
      err "$(t "Another deploy or update is running right now. Try again in a few minutes." "يوجد نشر أو تحديث آخر قيد التشغيل الآن. حاول بعد بضع دقائق.")"
      exit 1
    fi
  else
    if ! mkdir data/.deploy.lock.d 2>/dev/null; then
      err "$(t "Another deploy or update is running right now (or data/.deploy.lock.d was left behind)." "يوجد نشر أو تحديث آخر قيد التشغيل الآن (أو بقي data/.deploy.lock.d).")"
      exit 1
    fi
    trap 'rmdir data/.deploy.lock.d 2>/dev/null || true' EXIT
  fi
}

env_value() { grep -m1 "^$1=" .env 2>/dev/null | cut -d= -f2- | tr -d "\"'" || true; }
free_gb() { df -Pk . | awk 'NR == 2 { printf "%d", $4 / 1048576 }'; }
used_pct() { df -Pk . | awk 'NR == 2 { print $5 }'; }
fmt_secs() {
  if [ "$1" -ge 60 ]; then
    printf "$(t '%d min %d s' '%d د %d ث')" $(($1 / 60)) $(($1 % 60))
  else
    printf "$(t '%d s' '%d ث')" "$1"
  fi
}
image_version() { # the GIT_SHA baked into an image
  { docker image inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$1" 2>/dev/null || true; } | sed -n 's/^GIT_SHA=//p' | cut -c1-7
}

# wait_healthy EXPECTED_VERSION: backend answers (with that version, if given)
# and the frontend answers, within HEALTH_TIMEOUT seconds.
HEALTH_PROBLEM=""
wait_healthy() {
  local want="$1" start=$SECONDS body version waited
  while :; do
    waited=$((SECONDS - start))
    HEALTH_PROBLEM=""
    if body="$(curl -fsS --max-time 5 "${BACKEND_URL}/api/v1/health" 2>/dev/null)"; then
      version="$(printf '%s' "$body" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')"
      if [ -n "$want" ] && [ "$want" != "dev" ] && [ "$version" != "$want" ]; then
        HEALTH_PROBLEM="$(t "the backend still answers as version ${version:-?}, not ${want}" "الخادم الخلفي ما زال يعمل بالإصدار ${version:-?} وليس ${want}")"
      elif ! curl -fsS --max-time 5 -o /dev/null "${FRONTEND_URL}" 2>/dev/null; then
        HEALTH_PROBLEM="$(t "the website (frontend) is not answering" "الموقع (الواجهة) لا يستجيب")"
      else
        [ -n "$IS_TTY" ] && printf '\r\033[K'
        return 0
      fi
    else
      HEALTH_PROBLEM="$(t "the backend is not answering" "الخادم الخلفي لا يستجيب")"
    fi
    if [ "$waited" -ge "$HEALTH_TIMEOUT" ]; then
      [ -n "$IS_TTY" ] && printf '\r\033[K'
      return 1
    fi
    [ -n "$IS_TTY" ] && printf '\r[%d/%d] %s %s' "$STEP" "$TOTAL" "$(t "Health check" "فحص السلامة")" "$(t "waiting for the app… ${waited} s" "بانتظار التطبيق… ${waited} ث")"
    sleep 2
  done
}

# ---------------------------------------------------------------------------
# Going back when the new version doesn't come up
# ---------------------------------------------------------------------------
HAVE_PREVIOUS=""
PREV_VERSION=""
BACKUP_FILE=""

recover() {
  local why="$1"
  fail_step "$why"
  {
    echo "--- health problem: $why"
    docker compose logs --tail=60 birrcare-backend birrcare-frontend 2>&1 || true
  } >>"$LOG"
  if [ -z "$HAVE_PREVIOUS" ]; then
    show_log_tail
    err "$(t "The new version failed and there is no previous version to go back to (first install?). See the log above." "فشل الإصدار الجديد ولا يوجد إصدار سابق للرجوع إليه. راجع السجل أعلاه.")"
    exit 6
  fi
  say "$(t "Going back to the previous version (${PREV_VERSION:-?})…" "الرجوع إلى الإصدار السابق (${PREV_VERSION:-?})…")"
  run docker tag "$PREV_BACKEND" "$IMG_BACKEND" || true
  run docker tag "$PREV_FRONTEND" "$IMG_FRONTEND" || true
  if run docker compose up -d --no-build --force-recreate && wait_healthy ""; then
    show_log_tail
    say ""
    say "$(mark_warn) $(t "The update failed, so it was rolled back: the app is running the previous version (${PREV_VERSION:-?}) again." "فشل التحديث فتم التراجع عنه: التطبيق يعمل بالإصدار السابق (${PREV_VERSION:-?}) مجدداً.")"
    exit 4
  fi
  # The old version doesn't start either: the update probably changed the
  # database. Put back the copy taken just before the update.
  if [ -n "$BACKUP_FILE" ] && [ -f "$BACKUP_FILE" ]; then
    local kept="data/birrcare.db.failed-$(date +%Y%m%d-%H%M%S)"
    say "$(t "The previous version doesn't start with the updated database; restoring the database from before the update…" "الإصدار السابق لا يعمل مع قاعدة البيانات المحدّثة؛ استعادة قاعدة البيانات من قبل التحديث…")"
    run docker compose stop birrcare-backend || true
    mv -f data/birrcare.db "$kept"
    for ext in -wal -shm; do [ -f "data/birrcare.db${ext}" ] && mv -f "data/birrcare.db${ext}" "${kept}${ext}"; done
    cp "$BACKUP_FILE" data/birrcare.db
    if run docker compose up -d --no-build --force-recreate && wait_healthy ""; then
      show_log_tail
      say ""
      say "$(mark_warn) $(t "The update failed. The previous version and the database from before the update are back. Anything saved during the update is kept in ${kept}." "فشل التحديث. عاد الإصدار السابق وقاعدة البيانات من قبل التحديث. ما حُفظ أثناء التحديث موجود في ${kept}.")"
      exit 5
    fi
  fi
  show_log_tail
  err "$(t "The app could not be brought back automatically. See docs/DEPLOYMENT.md §11 (roll back a bad update)." "تعذر إعادة التطبيق تلقائياً. راجع docs/DEPLOYMENT.md §11.")"
  exit 6
}

# ---------------------------------------------------------------------------
# deploy
# ---------------------------------------------------------------------------
cmd_deploy() {
  local started=$SECONDS t0 free tz
  say "$(t "Birrcare (رعاية بر) — deploying" "رعاية بر — النشر") · $(date -u +"%Y-%m-%d %H:%M:%SZ")"
  take_lock

  # 1. Checks, before touching anything.
  step "$(t "Checks" "الفحوصات")"
  for cmd in docker curl; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      fail_step "$(t "'$cmd' is not installed" "'$cmd' غير مثبت")"
      err "$(t "Run this once (it asks for your password one time): sudo ./scripts/install-deps.sh" "شغّل هذا مرة واحدة (يطلب كلمة المرور مرة واحدة): sudo ./scripts/install-deps.sh")"
      exit 1
    fi
  done
  if ! docker info >>"$LOG" 2>&1; then
    fail_step "$(t "Docker isn't running, or this user can't use it" "Docker لا يعمل، أو لا يستطيع هذا المستخدم استخدامه")"
    err "$(t "Start it: sudo systemctl start docker. If you just ran install-deps.sh, log out and back in (or: newgrp docker)." "شغّله: sudo systemctl start docker. إذا شغّلت install-deps.sh للتو، سجّل الخروج والدخول (أو: newgrp docker).")"
    exit 1
  fi
  free="$(free_gb)"
  if [ "$free" -lt "$MIN_FREE_GB" ]; then
    # Old build leftovers are the usual cause; clear them and look again.
    run docker builder prune -af || true
    run docker image prune -f || true
    free="$(free_gb)"
  fi
  if [ "$free" -lt "$MIN_FREE_GB" ]; then
    fail_step "$(t "only ${free} GB free, ${MIN_FREE_GB} GB needed" "المساحة الحرة ${free} GB فقط، والمطلوب ${MIN_FREE_GB} GB")"
    err "$(t "A build needs about ${MIN_FREE_GB} GB. Make room (e.g. a larger disk for this machine) and run this again. Nothing was changed; the running version stays up." "يحتاج البناء نحو ${MIN_FREE_GB} GB. وفّر مساحة ثم أعد المحاولة. لم يتغير شيء؛ الإصدار الحالي يبقى يعمل.")"
    exit 1
  fi
  done_step "$(t "${free} GB free" "${free} GB متاحة")"

  # 2. Settings (.env), issue #52: ask what's missing in a terminal; with nobody
  # there (the Install button in the app), only check.
  step "$(t "Settings" "الإعدادات")"
  local setup_rc=0 setup_out=""
  if [ -t 0 ] || [ -n "${BIRRCARE_SETUP_TTY:-}" ]; then
    say ""
    ./scripts/setup-env.sh ${LANG_SEL:+--lang "$LANG_SEL"} || setup_rc=$?
  else
    setup_out="$(./scripts/setup-env.sh --non-interactive 2>>"$LOG")" || setup_rc=$?
  fi
  case "$setup_rc" in
    0) ;;
    2)
      fail_step "$(t "a setting needs an answer" "إعداد يحتاج إجابة")"
      # One machine-readable line for scripts/auto-deploy.sh, then the explanation.
      echo "NEEDS_CONFIG ${setup_out}"
      err "$(t "This version needs a setting that has no answer yet. Run ./scripts/deploy.sh on the server, in a terminal, to answer it. Nothing was changed; the running version stays up." "يحتاج هذا الإصدار إعداداً بلا إجابة بعد. شغّل ./scripts/deploy.sh على الخادم في الطرفية للإجابة. لم يتغير شيء؛ الإصدار الحالي يبقى يعمل.")"
      exit 3
      ;;
    *)
      fail_step "$(t "unsafe or unreadable settings" "إعدادات غير آمنة أو غير مقروءة")"
      err "$(t "The settings in .env are not safe or could not be written (see above). Nothing was changed; the running version stays up." "الإعدادات في .env غير آمنة أو تعذرت كتابتها (انظر أعلاه). لم يتغير شيء؛ الإصدار الحالي يبقى يعمل.")"
      exit 1
      ;;
  esac
  tz="$(env_value APP_TIMEZONE)"
  done_step "$(t "time zone ${tz}" "المنطقة الزمنية ${tz}")"

  # 3. A consistent copy of the database (issue #51): with WAL, a plain cp of
  # birrcare.db can miss the newest changes. Ask the running app for a snapshot
  # (VACUUM INTO, integrity-checked); if the app is down, use sqlite3's backup.
  step "$(t "Database backup" "نسخ قاعدة البيانات")"
  if [ -f "data/birrcare.db" ]; then
    mkdir -p data/backups
    BACKUP_FILE="data/backups/birrcare_backup_$(date +%Y%m%d_%H%M%S).db"
    local secret_file="data/backup/secret" snap=""
    if [ ! -s "$secret_file" ]; then
      mkdir -p data/backup
      (umask 077 && head -c 48 /dev/urandom | base64 | tr -d '/+=\n' >"$secret_file")
    fi
    snap="$(curl -fsS --max-time 120 -X POST -H "X-Backup-Secret: $(tr -d '\n' <"$secret_file")" \
      "${BACKEND_URL}/internal/backup/snapshot" 2>>"$LOG")" || snap=""
    if printf '%s' "$snap" | grep -q '"integrity":"ok"' && [ -f data/backup/staging/birrcare.db ]; then
      cp data/backup/staging/birrcare.db "$BACKUP_FILE"
    elif command -v sqlite3 >/dev/null 2>&1; then
      run sqlite3 data/birrcare.db ".backup '${BACKUP_FILE}'"
    elif [ -n "${SKIP_DB_BACKUP:-}" ]; then
      BACKUP_FILE=""
    else
      fail_step "$(t "no consistent copy possible" "تعذرت نسخة متسقة")"
      err "$(t "The app did not answer and sqlite3 is not installed. Start the app (docker compose up -d) or install sqlite3 (sudo ./scripts/install-deps.sh), then run this again. Nothing was changed. (To update without a backup anyway: SKIP_DB_BACKUP=1 ./scripts/deploy.sh)" "التطبيق لم يستجب و sqlite3 غير مثبت. شغّل التطبيق أو ثبّت sqlite3 ثم أعد المحاولة. لم يتغير شيء.")"
      exit 1
    fi
    # Keep the last 10.
    { ls -t data/backups/birrcare_backup_*.db 2>/dev/null || true; } | tail -n +11 | while IFS= read -r old; do rm -f -- "$old"; done
    if [ -n "$BACKUP_FILE" ]; then done_step "$BACKUP_FILE"; else printf '%s %s\n' "$(mark_warn)" "$(t "skipped (SKIP_DB_BACKUP)" "تم التخطي (SKIP_DB_BACKUP)")"; fi
  else
    done_step "$(t "no database yet (first install)" "لا توجد قاعدة بيانات بعد (تثبيت أول)")"
  fi

  # 4. Build. Keep the images that are running now, to go back to.
  step "$(t "New version" "الإصدار الجديد")"
  GIT_SHA="${GIT_SHA:-$(git rev-parse HEAD 2>/dev/null || echo dev)}"
  export GIT_SHA
  local want="${GIT_SHA:0:7}" b_id f_id
  b_id="$(docker inspect -f '{{.Image}}' "$C_BACKEND" 2>/dev/null || true)"
  f_id="$(docker inspect -f '{{.Image}}' "$C_FRONTEND" 2>/dev/null || true)"
  if [ -n "$b_id" ] && [ -n "$f_id" ] && run docker tag "$b_id" "$PREV_BACKEND" && run docker tag "$f_id" "$PREV_FRONTEND"; then
    HAVE_PREVIOUS=1
    PREV_VERSION="$(image_version "$PREV_BACKEND")"
  fi
  t0=$SECONDS
  local how="" reg
  # Download the images CI built and checked for exactly this version…
  reg="$(registry_prefix)"
  if [ "$GET_MODE" != "build" ] && [ -n "$reg" ] && [ "$want" != "dev" ]; then
    if run docker pull "${reg}/birrcare-backend:sha-${want}" && run docker pull "${reg}/birrcare-frontend:sha-${want}" &&
      [ "$(image_version "${reg}/birrcare-backend:sha-${want}")" = "$want" ] &&
      [ "$(image_version "${reg}/birrcare-frontend:sha-${want}")" = "$want" ]; then
      run docker tag "${reg}/birrcare-backend:sha-${want}" "$IMG_BACKEND"
      run docker tag "${reg}/birrcare-frontend:sha-${want}" "$IMG_FRONTEND"
      how="$(t "downloaded" "تم التنزيل")"
    else
      echo "--- could not download ${reg}/birrcare-*:sha-${want} (private packages, no internet or another processor); building here" >>"$LOG"
    fi
  fi
  if [ -z "$how" ] && [ "$GET_MODE" = "pull" ]; then
    fail_step "$(t "the images for ${want} could not be downloaded" "تعذر تنزيل صور الإصدار ${want}")"
    show_log_tail
    err "$(t "Nothing was changed; the running version stays up. (Without --pull, it builds here instead.)" "لم يتغير شيء؛ الإصدار الحالي يبقى يعمل.")"
    exit 1
  fi
  # …or build here.
  if [ -z "$how" ]; then
    # Leftovers from a build that failed half-way.
    run docker builder prune -af || true
    if ! run docker compose build --pull; then
      fail_step "$(t "the build failed" "فشل البناء")"
      show_log_tail
      err "$(t "Nothing was changed; the running version stays up." "لم يتغير شيء؛ الإصدار الحالي يبقى يعمل.")"
      exit 1
    fi
    how="$(t "built here" "بُني محلياً")"
  fi
  done_step "$(t "version ${want}" "الإصدار ${want}") · ${how} · $(fmt_secs $((SECONDS - t0)))"

  # 5. Start.
  step "$(t "Starting" "التشغيل")"
  if ! run docker compose up -d --no-build --remove-orphans; then
    recover "$(t "the new containers did not start" "لم تبدأ الحاويات الجديدة")"
  fi
  done_step

  # 6. Health: the new version must answer, or it is rolled back.
  step "$(t "Health check" "فحص السلامة")"
  if ! wait_healthy "$want"; then
    recover "${HEALTH_PROBLEM} ($(t "after ${HEALTH_TIMEOUT} s" "بعد ${HEALTH_TIMEOUT} ث"))"
  fi
  done_step "$(t "version ${want} is answering" "الإصدار ${want} يستجيب")"

  # 7. Clean up after the build, so the disk doesn't fill up update by update:
  # untagged old images and the build cache. The running containers, the
  # previous version (for rollback), the database and backups are kept.
  step "$(t "Clean-up" "التنظيف")"
  # Downloaded images stay under their birrcare-*:local names; drop the registry names.
  if [ -n "$reg" ]; then
    run docker image rm "${reg}/birrcare-backend:sha-${want}" "${reg}/birrcare-frontend:sha-${want}" || true
  fi
  run docker image prune -f || true
  run docker builder prune -af || true
  done_step "$(t "disk $(used_pct) used" "القرص مستخدم $(used_pct)")"

  # From outside too, through Cloudflare, when the address is known.
  local public
  public="$(env_value PUBLIC_URL)"
  if [ -n "$public" ]; then
    if curl -fsS --max-time 20 "${public%/}/api/v1/health" 2>>"$LOG" | grep -q "\"version\":\"${want}\""; then
      say "      $(mark_ok) $(t "reachable from the internet:" "يمكن الوصول إليه من الإنترنت:") ${public}"
    else
      say "      $(mark_warn) $(t "the app works here, but ${public} does not answer with this version (check the tunnel / DNS)" "التطبيق يعمل هنا، لكن ${public} لا يستجيب بهذا الإصدار (افحص النفق / DNS)")"
    fi
  fi

  say ""
  say "$(mark_ok) $(t "Done in $(fmt_secs $((SECONDS - started))).${public:+ Open ${public}}" "تم خلال $(fmt_secs $((SECONDS - started))).${public:+ افتح ${public}}")"
  say "  $(t "Log:" "السجل:") ${LOG}"
}

# ---------------------------------------------------------------------------
# status / logs / restart / rollback
# ---------------------------------------------------------------------------
cmd_status() {
  local body version prev
  say "$(t "Birrcare status" "حالة رعاية بر")"
  if body="$(curl -fsS --max-time 5 "${BACKEND_URL}/api/v1/health" 2>/dev/null)"; then
    version="$(printf '%s' "$body" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')"
    say "  $(mark_ok) $(t "App" "التطبيق"): $(t "running, version" "يعمل، الإصدار") ${version:-?}"
  else
    say "  $(mark_bad) $(t "App" "التطبيق"): $(t "not answering" "لا يستجيب")"
  fi
  if command -v docker >/dev/null 2>&1; then
    docker compose ps --format '{{.Name}}	{{.Status}}' 2>/dev/null | sed 's/^/      /' || true
    prev="$(image_version "$PREV_BACKEND")"
    [ -n "$prev" ] && say "  $(t "Previous version kept for rollback:" "الإصدار السابق المحفوظ للتراجع:") ${prev}"
  fi
  say "  $(t "Disk" "القرص"): $(used_pct) $(t "used" "مستخدم"), $(free_gb) GB $(t "free" "متاحة")"
  if [ -f data/backup/status.json ] && command -v jq >/dev/null 2>&1; then
    say "  $(t "Backups" "النسخ الاحتياطي"): $(jq -r '"\(.status // "?"), last success \(.last_success_at // "never")"' data/backup/status.json)"
  else
    say "  $(t "Backups" "النسخ الاحتياطي"): $(t "not set up (docs/DEPLOYMENT.md §9)" "غير مُعدّ (docs/DEPLOYMENT.md §9)")"
  fi
  if [ -f data/update/available.json ] && command -v jq >/dev/null 2>&1; then
    say "  $(t "Update checker" "فاحص التحديثات"): $(jq -r '"last check \(.checked_at // "?")" + (if (.target_sha // "") != "" then ", update available " + .target_sha[0:7] else ", up to date" end)' data/update/available.json)"
  else
    say "  $(t "Update checker" "فاحص التحديثات"): $(t "has not run yet" "لم يعمل بعد")"
  fi
  if systemctl --user is-active birrcare-deploy.timer >/dev/null 2>&1; then
    say "  $(t "Timers" "المؤقتات"): systemd ($(t "active" "نشطة"))"
  elif crontab -l 2>/dev/null | grep -q 'scripts/auto-deploy.sh'; then
    say "  $(t "Timers" "المؤقتات"): cron"
  else
    say "  $(mark_warn) $(t "Timers: none (run: sudo ./scripts/install-deps.sh)" "المؤقتات: لا يوجد (شغّل: sudo ./scripts/install-deps.sh)")"
  fi
  local last
  last="$({ ls -1t data/logs/deploy-*.log 2>/dev/null || true; } | head -n 1)"
  [ -n "$last" ] && say "  $(t "Last deploy log:" "آخر سجل نشر:") $last"
  return 0
}

cmd_logs() {
  # shellcheck disable=SC2086
  docker compose logs --tail=200 ${LOG_FOLLOW:+-f} ${LOG_SERVICE}
}

cmd_restart() {
  take_lock
  TOTAL=2
  step "$(t "Restarting" "إعادة التشغيل")"
  # Recreate (not "docker compose restart"), so changes in .env take effect.
  run docker compose up -d --force-recreate --remove-orphans || { fail_step; show_log_tail; exit 1; }
  done_step
  step "$(t "Health check" "فحص السلامة")"
  if wait_healthy ""; then done_step; else fail_step "$HEALTH_PROBLEM"; show_log_tail; exit 1; fi
}

cmd_rollback() {
  take_lock
  local prev cur ans
  prev="$(image_version "$PREV_BACKEND")"
  if ! docker image inspect "$PREV_BACKEND" >/dev/null 2>&1 || ! docker image inspect "$PREV_FRONTEND" >/dev/null 2>&1; then
    err "$(t "There is no previous version kept to go back to." "لا يوجد إصدار سابق محفوظ للرجوع إليه.")"
    exit 1
  fi
  cur="$(curl -fsS --max-time 5 "${BACKEND_URL}/api/v1/health" 2>/dev/null | sed -n 's/.*"version":"\([^"]*\)".*/\1/p' || true)"
  say "$(t "Go back from version ${cur:-?} to ${prev:-?}." "الرجوع من الإصدار ${cur:-?} إلى ${prev:-?}.")"
  say "$(t "The database is not changed. If the newer version changed it and the old one doesn't start, see docs/DEPLOYMENT.md §11." "قاعدة البيانات لا تتغير. إذا غيّرها الإصدار الأحدث ولم يعمل القديم، راجع docs/DEPLOYMENT.md §11.")"
  if [ -z "$ASSUME_YES" ]; then
    if [ ! -t 0 ]; then
      err "$(t "Add --yes to confirm without a terminal." "أضف --yes للتأكيد دون طرفية.")"
      exit 1
    fi
    read -r -p "$(t "Continue? [y/N] " "متابعة؟ [y/N] ")" ans
    case "$ans" in y* | Y* | ن*) ;; *) say "$(t "Nothing changed." "لم يتغير شيء.")"; exit 0 ;; esac
  fi
  TOTAL=2
  step "$(t "Switching back" "الرجوع")"
  run docker tag "$PREV_BACKEND" "$IMG_BACKEND" && run docker tag "$PREV_FRONTEND" "$IMG_FRONTEND" &&
    run docker compose up -d --no-build --force-recreate || { fail_step; show_log_tail; exit 1; }
  done_step
  step "$(t "Health check" "فحص السلامة")"
  if wait_healthy ""; then done_step "$(t "version ${prev:-?}" "الإصدار ${prev:-?}")"; else fail_step "$HEALTH_PROBLEM"; show_log_tail; exit 1; fi
}

case "$CMD" in
  deploy) cmd_deploy ;;
  status) cmd_status ;;
  logs) cmd_logs ;;
  restart) cmd_restart ;;
  rollback) cmd_rollback ;;
esac
