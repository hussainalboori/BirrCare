#!/usr/bin/env bash
set -euo pipefail

# Birrcare (رعاية بر) — Install what the server needs (run once, with sudo)
#
#   sudo ./scripts/install-deps.sh
#
# Asks once for your password (sudo), then:
#   - checks what is already installed and offers only what is missing;
#   - required: git, curl, jq, and Docker (official Docker packages);
#   - optional, each asked: restic + rclone (encrypted off-site backups),
#     sqlite3 (safe database copy when the app is stopped);
#   - shows the full list and asks before installing anything;
#   - then, each asked: let your user run Docker without sudo, keep your timers
#     running when nobody is logged in (loginctl enable-linger), and install the
#     update and backup timers (replacing any old cron lines).
# After this, deploy.sh, updates from the app and backups run as your normal
# user and never need sudo.
#
# Debian, Ubuntu and Raspberry Pi OS (apt). Other systems get a list of what
# to install by hand.
#
# Options: --yes (accept every suggestion), --lang ar|en.
# Test hooks: BIRRCARE_DEPS_FAKE_ROOT=1, OS_RELEASE=FILE, BIRRCARE_SETUP_TTY=1,
# BIRRCARE_DEPS_PRESENT="git curl …" (pretend only these are installed),
# BIRRCARE_APT_DIR, BIRRCARE_RUNTIME_DIR.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OS_RELEASE="${OS_RELEASE:-/etc/os-release}"
YES=""
LANG_SEL=""

while [ $# -gt 0 ]; do
  case "$1" in
    --yes | -y) YES=1 ;;
    --lang) LANG_SEL="$2"; shift ;;
    -h | --help) sed -n '3,26p' "$0"; exit 0 ;;
    *) echo "install-deps: unknown option $1" >&2; exit 1 ;;
  esac
  shift
done
if [ -z "$LANG_SEL" ]; then
  case "${LANG:-}${LC_ALL:-}" in ar*) LANG_SEL=ar ;; *) LANG_SEL=en ;; esac
fi

t() { if [ "$LANG_SEL" = "ar" ]; then printf '%s' "$2"; else printf '%s' "$1"; fi; }
say() { printf '%s\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
have() {
  if [ -n "${BIRRCARE_DEPS_PRESENT+x}" ]; then
    case " $BIRRCARE_DEPS_PRESENT " in *" $1 "*) return 0 ;; *) return 1 ;; esac
  fi
  command -v "$1" >/dev/null 2>&1
}

# ask_yn QUESTION DEFAULT(Y|N): 0 = yes. Without a terminal (or --yes), the default.
ask_yn() {
  local q="$1" def="$2" ans hint="[Y/n]"
  [ "$def" = "N" ] && hint="[y/N]"
  if [ -z "$INTERACTIVE" ]; then
    [ "$def" = "Y" ]
    return
  fi
  if ! IFS= read -r -p "$q $hint " ans; then
    say ""
    ans=""
  fi
  [ -z "$ans" ] && ans="$def"
  case "$ans" in y* | Y* | ن*) return 0 ;; *) return 1 ;; esac
}

# --- Root, once ---------------------------------------------------------------
if [ "$(id -u)" != "0" ] && [ -z "${BIRRCARE_DEPS_FAKE_ROOT:-}" ]; then
  red "$(t "Run this once with sudo (it asks for your password one time):" "شغّل هذا مرة واحدة باستخدام sudo (يطلب كلمة المرور مرة واحدة):")"
  say "  sudo ./scripts/install-deps.sh"
  exit 1
fi

INTERACTIVE=""
if [ -z "$YES" ]; then
  if [ -t 0 ] || [ -n "${BIRRCARE_SETUP_TTY:-}" ]; then
    INTERACTIVE=1
  else
    # Nobody to ask: don't install things silently.
    echo "install-deps: this asks questions; run it in a terminal, or add --yes to accept every suggestion." >&2
    exit 1
  fi
fi


# The user who will run Birrcare: whoever called sudo, else the owner of the checkout.
TARGET_USER="${SUDO_USER:-}"
if [ -z "$TARGET_USER" ] || [ "$TARGET_USER" = "root" ]; then
  TARGET_USER="$(stat -c %U "$ROOT" 2>/dev/null || stat -f %Su "$ROOT")"
fi
TARGET_HOME="$(getent passwd "$TARGET_USER" | cut -d: -f6)"
TARGET_UID="$(id -u "$TARGET_USER")"
APT_DIR="${BIRRCARE_APT_DIR:-/etc/apt}"
RUNTIME_DIR="${BIRRCARE_RUNTIME_DIR:-/run/user/${TARGET_UID}}"

# --- System -------------------------------------------------------------------
OS_ID=""
OS_LIKE=""
CODENAME=""
if [ -f "$OS_RELEASE" ]; then
  # shellcheck disable=SC1090
  OS_ID="$(. "$OS_RELEASE" && echo "${ID:-}")"
  OS_LIKE="$(. "$OS_RELEASE" && echo "${ID_LIKE:-}")"
  CODENAME="$(. "$OS_RELEASE" && echo "${UBUNTU_CODENAME:-${VERSION_CODENAME:-}}")"
fi
DOCKER_DISTRO=""
case "$OS_ID" in
  ubuntu | debian | raspbian) DOCKER_DISTRO="$OS_ID" ;;
  *)
    case " $OS_LIKE " in
      *" ubuntu "*) DOCKER_DISTRO=ubuntu ;;
      *" debian "*) DOCKER_DISTRO=debian ;;
    esac
    ;;
esac

say "$(t "Birrcare: checking what this server needs (user: ${TARGET_USER})" "رعاية بر: فحص ما يحتاجه الخادم (المستخدم: ${TARGET_USER})")"

# --- What is missing ----------------------------------------------------------
PKGS=""
want() { case " $PKGS " in *" $1 "*) ;; *) PKGS="${PKGS} $1" ;; esac; }
status_line() { # status_line NAME OK?
  if [ "$2" = yes ]; then say "  ✔ $1"; else say "  ✖ $1 $(t "(missing)" "(غير مثبت)")"; fi
}

for tool in git curl jq; do
  if have "$tool"; then status_line "$tool" yes; else status_line "$tool" no; want "$tool"; fi
done
have update-ca-certificates || want ca-certificates

NEED_DOCKER=""
if have docker && docker compose version >/dev/null 2>&1; then # (Compose v2 plugin)
  status_line "Docker + Compose" yes
else
  status_line "Docker + Compose" no
  NEED_DOCKER=1
fi
for tool in restic rclone sqlite3; do
  if have "$tool"; then status_line "$tool" yes; else status_line "$tool" no; fi
done
say ""

INSTALL_DOCKER=""
if [ -n "$NEED_DOCKER" ]; then
  if ask_yn "$(t "Install Docker from Docker's official packages? (required to run Birrcare)" "تثبيت Docker من حزم Docker الرسمية؟ (مطلوب لتشغيل رعاية بر)")" Y; then
    INSTALL_DOCKER=1
    want ca-certificates
    want curl
    want gnupg
  else
    red "$(t "Without Docker, Birrcare can't run. Install it yourself, then run ./scripts/deploy.sh." "بدون Docker لا يعمل رعاية بر. ثبّته بنفسك ثم شغّل ./scripts/deploy.sh.")"
  fi
fi
if ! have restic || ! have rclone; then
  if ask_yn "$(t "Install restic and rclone for encrypted off-site backups (Google Drive, R2, B2)?" "تثبيت restic و rclone للنسخ الاحتياطي المشفر خارج الخادم (Google Drive وغيرها)؟")" Y; then
    have restic || want restic
    have rclone || want rclone
  fi
fi
if ! have sqlite3; then
  if ask_yn "$(t "Install sqlite3 (a safe database copy before an update when the app is stopped)?" "تثبيت sqlite3 (نسخة آمنة لقاعدة البيانات قبل التحديث عندما يكون التطبيق متوقفاً)؟")" Y; then
    want sqlite3
  fi
fi

# --- Install ------------------------------------------------------------------
DOCKER_PKGS="docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin"
if [ -n "${PKGS# }" ] || [ -n "$INSTALL_DOCKER" ]; then
  if ! have apt-get || { [ -n "$INSTALL_DOCKER" ] && [ -z "$DOCKER_DISTRO" ]; }; then
    red "$(t "This system doesn't use apt (Debian/Ubuntu/Raspberry Pi OS). Install these yourself:" "هذا النظام لا يستخدم apt. ثبّت هذه بنفسك:")"
    say " ${PKGS}${INSTALL_DOCKER:+ docker docker-compose}"
    exit 2
  fi
  say "$(t "Will install:" "سيتم تثبيت:")${PKGS}${INSTALL_DOCKER:+ ${DOCKER_PKGS}}"
  if ! ask_yn "$(t "Go ahead?" "متابعة؟")" Y; then
    say "$(t "Nothing installed." "لم يُثبَّت شيء.")"
    exit 0
  fi
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -q
  # shellcheck disable=SC2086
  [ -n "${PKGS# }" ] && apt-get install -y -q --no-install-recommends $PKGS
  if [ -n "$INSTALL_DOCKER" ]; then
    # Docker's documented apt repository, signed with its published key.
    install -m 0755 -d "${APT_DIR}/keyrings"
    curl -fsSL "https://download.docker.com/linux/${DOCKER_DISTRO}/gpg" -o "${APT_DIR}/keyrings/docker.asc"
    chmod a+r "${APT_DIR}/keyrings/docker.asc"
    mkdir -p "${APT_DIR}/sources.list.d"
    printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/%s %s stable\n' \
      "$(dpkg --print-architecture)" "$DOCKER_DISTRO" "$CODENAME" >"${APT_DIR}/sources.list.d/docker.list"
    apt-get update -q
    # shellcheck disable=SC2086
    apt-get install -y -q $DOCKER_PKGS
  fi
  green "$(t "Installed." "تم التثبيت.")"
else
  green "$(t "All the tools you chose are already installed." "كل الأدوات التي اخترتها مثبتة مسبقاً.")"
fi

# --- Docker without sudo --------------------------------------------------------
if getent group docker >/dev/null 2>&1 && ! id -nG "$TARGET_USER" | tr ' ' '\n' | grep -qx docker; then
  if ask_yn "$(t "Let ${TARGET_USER} run Docker without sudo (adds them to the docker group)?" "السماح لـ ${TARGET_USER} بتشغيل Docker دون sudo (إضافته لمجموعة docker)؟")" Y; then
    usermod -aG docker "$TARGET_USER"
    say "  $(t "Log out and back in (or run: newgrp docker) for this to take effect." "سجّل الخروج ثم الدخول (أو شغّل: newgrp docker) ليسري ذلك.")"
  fi
fi

# --- Timers keep running when nobody is logged in ----------------------------------
LINGER=""
if have loginctl; then
  if loginctl show-user "$TARGET_USER" -p Linger 2>/dev/null | grep -q 'Linger=yes'; then
    LINGER=1
  elif ask_yn "$(t "Keep ${TARGET_USER}'s update and backup timers running when nobody is logged in (loginctl enable-linger)?" "إبقاء مؤقتات التحديث والنسخ الاحتياطي تعمل دون تسجيل دخول (loginctl enable-linger)؟")" Y; then
    if loginctl enable-linger "$TARGET_USER" 2>/dev/null; then
      LINGER=1
    else
      red "$(t "This system refused it (common in containers); the timers will use cron instead." "رفض النظام ذلك (شائع في الحاويات)؛ ستستخدم المؤقتات cron بدلاً من ذلك.")"
    fi
  fi
fi

# --- Update and backup timers ---------------------------------------------------
as_user() { runuser -u "$TARGET_USER" -- env XDG_RUNTIME_DIR="$RUNTIME_DIR" "$@"; }
CRON_TAG="# BirrCare"
if ask_yn "$(t "Install the update check and backup timers for ${TARGET_USER}?" "تثبيت مؤقتات فحص التحديثات والنسخ الاحتياطي لـ ${TARGET_USER}؟")" Y; then
  installed=""
  if [ -n "$LINGER" ] && have systemctl; then
    unit_dir="${TARGET_HOME}/.config/systemd/user"
    mkdir -p "$unit_dir"
    for u in "$ROOT"/scripts/systemd/birrcare-{deploy,backup}.{service,timer,path}; do
      # The units assume ~/birrcare; point them at this checkout.
      sed "s#%h/birrcare#${ROOT}#g" "$u" >"${unit_dir}/$(basename "$u")"
    done
    chown -R "$TARGET_USER" "${TARGET_HOME}/.config"
    for _ in 1 2 3 4 5; do [ -d "$RUNTIME_DIR" ] && break; sleep 1; done
    if as_user systemctl --user daemon-reload &&
      as_user systemctl --user enable --now birrcare-deploy.timer birrcare-deploy.path birrcare-backup.timer birrcare-backup.path; then
      installed=systemd
    fi
  fi
  # Drop old BirrCare cron lines (systemd replaces them, or they're rewritten below).
  current="$(crontab -u "$TARGET_USER" -l 2>/dev/null || true)"
  cleaned="$(printf '%s\n' "$current" | grep -vE 'birrcare/scripts/(auto-deploy|backup)\.sh|^# BirrCare' || true)"
  if [ -z "$installed" ] && have crontab; then
    cleaned="$(printf '%s\n%s\n%s\n%s\n%s\n%s\n' "$cleaned" "$CRON_TAG: updates and backups (see docs/DEPLOYMENT.md §8-9)" \
      "*/5 * * * * ${ROOT}/scripts/auto-deploy.sh >> ${TARGET_HOME}/.local/state/birrcare/updater.log 2>&1" \
      "* * * * * [ -f ${ROOT}/data/update/request.json ] && ${ROOT}/scripts/auto-deploy.sh >> ${TARGET_HOME}/.local/state/birrcare/updater.log 2>&1" \
      "*/5 * * * * ${ROOT}/scripts/backup.sh --if-due >> ${TARGET_HOME}/.local/state/birrcare/backup.log 2>&1" \
      "* * * * * ${ROOT}/scripts/backup.sh --if-requested >> ${TARGET_HOME}/.local/state/birrcare/backup.log 2>&1")"
    installed=cron
  fi
  if [ "$cleaned" != "$current" ]; then
    mkdir -p "${TARGET_HOME}/.local/state/birrcare"
    chown -R "$TARGET_USER" "${TARGET_HOME}/.local"
    printf '%s\n' "$cleaned" | awk 'NF || prev_nf { print } { prev_nf = NF }' | crontab -u "$TARGET_USER" -
  fi
  case "$installed" in
    systemd) green "$(t "Timers installed (systemd)." "ثُبّتت المؤقتات (systemd).")" ;;
    cron) green "$(t "Timers installed (cron)." "ثُبّتت المؤقتات (cron).")" ;;
    *) red "$(t "Could not install the timers; see docs/DEPLOYMENT.md §8." "تعذر تثبيت المؤقتات؛ راجع docs/DEPLOYMENT.md §8.")" ;;
  esac
fi

say ""
green "$(t "Done. Next, as ${TARGET_USER} (no sudo): ./scripts/deploy.sh" "تم. بعد ذلك، كمستخدم ${TARGET_USER} (دون sudo): ./scripts/deploy.sh")"
