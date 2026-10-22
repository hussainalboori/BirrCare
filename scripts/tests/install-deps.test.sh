#!/usr/bin/env bash
# Tests for scripts/install-deps.sh with stub apt-get / loginctl / crontab /
# runuser… (nothing is really installed). Answers come on stdin through the
# BIRRCARE_SETUP_TTY=1 hook; the question order is: [Docker] [restic+rclone]
# [sqlite3] [go ahead] [docker group] [linger] [timers].
# Usage: bash scripts/tests/install-deps.test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="${ROOT}/scripts/install-deps.sh"
fails=0
ALL="git curl jq update-ca-certificates docker restic rclone sqlite3 apt-get loginctl systemctl crontab"

setup() {
  W="$(mktemp -d)"
  mkdir -p "$W/bin" "$W/home" "$W/apt" "$W/run"
  : >"$W/calls"
  printf 'ID=ubuntu\nVERSION_CODENAME=noble\n' >"$W/os-release"
  echo no >"$W/linger"
  echo yes >"$W/linger-allowed"
  printf 'runner sudo\n' >"$W/groups"
  printf '*/5 * * * * /home/runner/birrcare/scripts/auto-deploy.sh >> x 2>&1\n0 4 * * * /usr/bin/my-own-job\n' >"$W/crontab"
  local s
  for s in apt-get install usermod runuser chown chmod; do
    printf '#!/usr/bin/env bash\necho "%s $*" >>"%s/calls"\n' "$s" "$W" >"$W/bin/$s"
  done
  cat >"$W/bin/curl" <<EOF
#!/usr/bin/env bash
echo "curl \$*" >>"$W/calls"
EOF
  printf '#!/usr/bin/env bash\necho amd64\n' >"$W/bin/dpkg"
  printf '#!/usr/bin/env bash\n[ "$1 $2" = "compose version" ] && exit 0\nexit 0\n' >"$W/bin/docker"
  cat >"$W/bin/id" <<EOF
#!/usr/bin/env bash
case "\$1" in
  -nG) cat "$W/groups" ;;
  *) echo 1000 ;;
esac
EOF
  cat >"$W/bin/getent" <<EOF
#!/usr/bin/env bash
case "\$1" in
  passwd) echo "runner:x:1000:1000::$W/home:/bin/bash" ;;
  group) [ -f "$W/has-docker-group" ] && echo "docker:x:999:" || exit 2 ;;
esac
EOF
  cat >"$W/bin/loginctl" <<EOF
#!/usr/bin/env bash
case "\$1" in
  show-user) echo "Linger=\$(cat "$W/linger")" ;;
  enable-linger) echo "loginctl \$*" >>"$W/calls"; [ "\$(cat "$W/linger-allowed")" = yes ] ;;
esac
EOF
  cat >"$W/bin/crontab" <<EOF
#!/usr/bin/env bash
if [ "\$3" = "-l" ]; then cat "$W/crontab"; else cat >"$W/crontab"; fi
EOF
  chmod +x "$W/bin/"*
}

# run "answers" [extra env…]
run() {
  local answers="$1"
  shift
  (printf '%s' "$answers" | env PATH="$W/bin:$PATH" SUDO_USER=runner BIRRCARE_DEPS_FAKE_ROOT=1 BIRRCARE_SETUP_TTY=1 \
    OS_RELEASE="$W/os-release" BIRRCARE_APT_DIR="$W/apt" BIRRCARE_RUNTIME_DIR="$W/run" "$@" \
    bash "$SCRIPT" --lang en >"$W/out" 2>&1) && echo 0 >"$W/rc" || echo $? >"$W/rc"
}

check() {
  local name="$1" got="$2" want="$3"
  if [ "$got" = "$want" ]; then
    echo "ok   - $name"
  else
    echo "FAIL - $name: got '$got', want '$want'"
    sed 's/^/       | /' "$W/out"
    sed 's/^/       > /' "$W/calls"
    fails=$((fails + 1))
  fi
}
calls() { grep -c "$1" "$W/calls" || true; }

# 1. Not root: asks for sudo, does nothing.
setup
(env PATH="$W/bin:$PATH" bash -c 'id() { echo 1000; }; export -f id; bash "$0" --lang en' "$SCRIPT" </dev/null >"$W/out" 2>&1) && echo 0 >"$W/rc" || echo $? >"$W/rc"
check "not root: exit 1" "$(cat "$W/rc")" "1"
check "not root: tells to use sudo" "$(grep -c 'sudo ./scripts/install-deps.sh' "$W/out")" "1"
rm -rf "$W"

# 2. No terminal and no --yes: refuses to install silently.
setup
(env PATH="$W/bin:$PATH" SUDO_USER=runner BIRRCARE_DEPS_FAKE_ROOT=1 OS_RELEASE="$W/os-release" bash "$SCRIPT" </dev/null >"$W/out" 2>&1) && echo 0 >"$W/rc" || echo $? >"$W/rc"
check "no terminal: exit 1" "$(cat "$W/rc")" "1"
check "no terminal: nothing installed" "$(calls apt-get)" "0"
rm -rf "$W"

# 3. Everything present, linger on, in the docker group: nothing to install;
#    timers via systemd, the old cron line removed, own cron jobs kept.
setup
echo yes >"$W/linger"
printf 'runner sudo docker\n' >"$W/groups"
touch "$W/has-docker-group"
mkdir -p "$W/run"
run $'\n' BIRRCARE_DEPS_PRESENT="$ALL"
check "complete: exit 0" "$(cat "$W/rc")" "0"
check "complete: nothing installed" "$(calls apt-get)" "0"
check "complete: says so" "$(grep -c 'already installed' "$W/out")" "1"
check "complete: timers enabled with systemd" "$(calls 'runuser -u runner -- env XDG_RUNTIME_DIR=.* systemctl --user enable --now birrcare-deploy.timer birrcare-deploy.path birrcare-backup.timer birrcare-backup.path')" "1"
check "complete: units point at this checkout" "$(grep -c "ExecStart=${ROOT}/scripts/backup.sh --if-due" "$W/home/.config/systemd/user/birrcare-backup.service")" "1"
check "complete: old updater cron line removed" "$(grep -c 'auto-deploy.sh' "$W/crontab" || true)" "0"
check "complete: own cron job kept" "$(grep -c 'my-own-job' "$W/crontab")" "1"
rm -rf "$W"

# 4. jq, restic, rclone, sqlite3 missing: yes to backups, no to sqlite3.
setup
run $'y\nn\ny\nn\nn\nn\n' BIRRCARE_DEPS_PRESENT="git curl update-ca-certificates docker apt-get loginctl systemctl crontab"
check "choice: exit 0" "$(cat "$W/rc")" "0"
check "choice: shows what's missing" "$(grep -c '✖ jq (missing)' "$W/out")" "1"
check "choice: one install with exactly the chosen tools" "$(grep '^apt-get install' "$W/calls" | sed 's/.*--no-install-recommends//')" " jq restic rclone"
check "choice: no sqlite3" "$(grep -c sqlite3 "$W/calls" || true)" "0"
rm -rf "$W"

# 5. Docker missing: Docker's own signed apt repository, then its packages.
setup
run $'y\ny\ny\ny\nn\nn\n' BIRRCARE_DEPS_PRESENT="git curl jq update-ca-certificates restic rclone sqlite3 apt-get loginctl systemctl crontab"
check "docker: exit 0" "$(cat "$W/rc")" "0"
check "docker: key from download.docker.com" "$(calls 'curl -fsSL https://download.docker.com/linux/ubuntu/gpg')" "1"
check "docker: signed source for this release" "$(cat "$W/apt/sources.list.d/docker.list")" "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu noble stable"
check "docker: compose plugin installed" "$(calls 'apt-get install -y -q docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin')" "1"
rm -rf "$W"

# 6. Not in the docker group, linger refused (a container): added to the group,
#    timers fall back to cron with the backup and updater lines.
setup
touch "$W/has-docker-group"
echo no >"$W/linger-allowed"
run $'y\ny\ny\n' BIRRCARE_DEPS_PRESENT="$ALL"
check "cron: exit 0" "$(cat "$W/rc")" "0"
check "cron: added to docker group" "$(calls 'usermod -aG docker runner')" "1"
check "cron: linger tried" "$(calls 'loginctl enable-linger runner')" "1"
check "cron: explains the fallback" "$(grep -c 'will use cron instead' "$W/out")" "1"
check "cron: updater line" "$(grep -c "^\*/5 \* \* \* \* ${ROOT}/scripts/auto-deploy.sh" "$W/crontab")" "1"
check "cron: backup line" "$(grep -c "${ROOT}/scripts/backup.sh --if-due" "$W/crontab")" "1"
check "cron: Back up now line" "$(grep -c "${ROOT}/scripts/backup.sh --if-requested" "$W/crontab")" "1"
check "cron: old line replaced, not duplicated" "$(grep -c 'auto-deploy.sh' "$W/crontab")" "2"
check "cron: own cron job kept" "$(grep -c 'my-own-job' "$W/crontab")" "1"
rm -rf "$W"

# 7. "No" at "Go ahead?": nothing installed.
setup
run $'y\ny\nn\n' BIRRCARE_DEPS_PRESENT="git curl update-ca-certificates docker apt-get"
check "declined: exit 0" "$(cat "$W/rc")" "0"
check "declined: nothing installed" "$(calls 'apt-get install')" "0"
rm -rf "$W"

# 8. Not an apt system: a list to install by hand.
setup
printf 'ID=fedora\n' >"$W/os-release"
run $'y\ny\n' BIRRCARE_DEPS_PRESENT="git curl docker"
check "no apt: exit 2" "$(cat "$W/rc")" "2"
check "no apt: lists what to install" "$(grep -c 'Install these yourself' "$W/out")" "1"
rm -rf "$W"

# 9. --yes accepts every suggestion without questions.
setup
(env PATH="$W/bin:$PATH" SUDO_USER=runner BIRRCARE_DEPS_FAKE_ROOT=1 OS_RELEASE="$W/os-release" BIRRCARE_APT_DIR="$W/apt" \
  BIRRCARE_RUNTIME_DIR="$W/run" BIRRCARE_DEPS_PRESENT="git curl update-ca-certificates docker apt-get loginctl systemctl crontab" \
  bash "$SCRIPT" --yes </dev/null >"$W/out" 2>&1) && echo 0 >"$W/rc" || echo $? >"$W/rc"
check "--yes: exit 0" "$(cat "$W/rc")" "0"
check "--yes: everything suggested" "$(grep '^apt-get install' "$W/calls" | sed 's/.*--no-install-recommends//')" " jq restic rclone sqlite3"
rm -rf "$W"

if [ "$fails" -gt 0 ]; then
  echo "$fails check(s) failed"
  exit 1
fi
echo "all install-deps checks passed"
