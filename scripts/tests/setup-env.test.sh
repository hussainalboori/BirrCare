#!/usr/bin/env bash
# Tests for scripts/setup-env.sh (issue #52). Answers are fed on stdin with the
# BIRRCARE_SETUP_TTY=1 hook; running out of answers ends the script with exit 2,
# so "asked exactly N questions" is checked by giving exactly N answers.
# Usage: bash scripts/tests/setup-env.test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="${ROOT}/scripts/setup-env.sh"
fails=0

setup() {
  W="$(mktemp -d)"
  : >"$W/out"
}

# run [args…] with answers on stdin (one per line)
run() {
  local answers="$1"
  shift
  (printf '%s' "$answers" | BIRRCARE_SETUP_TTY=1 bash "$SCRIPT" --env "$W/.env" --db "$W/birrcare.db" --lang en "$@" >"$W/stdout" 2>"$W/out") &&
    echo 0 >"$W/rc" || echo $? >"$W/rc"
}

check() {
  local name="$1" got="$2" want="$3"
  if [ "$got" = "$want" ]; then
    echo "ok   - $name"
  else
    echo "FAIL - $name: got '$got', want '$want'"
    sed 's/^/       | /' "$W/out" "$W/stdout"
    fails=$((fails + 1))
  fi
}

val() { grep -m1 "^$1=" "$W/.env" | cut -d= -f2-; }
mode() { stat -c %a "$1" 2>/dev/null || stat -f %Lp "$1"; }

# A complete, valid .env for an existing install.
full_env() {
  cat >"$W/.env" <<'EOF'
# my notes
SUPER_ADMIN_USER=Hussain
SUPER_ADMIN_PIN=4829
APP_TIMEZONE=Asia/Bahrain
EMERGENCY_NUMBER=999
DOCTOR_NAME=
DOCTOR_PHONE=
CF_ACCESS_TEAM_DOMAIN=
CF_ACCESS_AUD=
CF_ACCESS_TRUST_EMAIL_HEADER=false
PUSH_CONTACT=
SESSION_SECRET=0123456789abcdefghij0123456789abcdefghij0123456789abcdefghij
DISABLE_2FA_FOR=
AUTH_DEV_FALLBACK=false
CORS_ALLOWED_ORIGINS=
TRUSTED_HOME_IP=
TRUSTED_HOME_SUBNETS=
MY_OWN_SETTING=keep me
EOF
  chmod 600 "$W/.env"
  echo db >"$W/birrcare.db"
}

# Answers for a fresh install: time zone, admin name, PIN twice, emergency
# (Enter = suggestion) and its confirmation, doctor name and phone, then Enter
# for the Cloudflare domain, push contact and home IP.
FRESH_ANSWERS=$'Asia/Bahrain\nHussain\n4829\n4829\n\ny\nDr Ahmed\n+973 3912-3456\n\n\n\n'

# 1. Fresh install.
setup
run "$FRESH_ANSWERS"
check "fresh: exit code" "$(cat "$W/rc")" "0"
check "fresh: .env mode 600" "$(mode "$W/.env")" "600"
missing=""
while IFS='|' read -r key _; do
  case "$key" in "" | \#*) continue ;; esac
  grep -q "^${key}=" "$W/.env" || missing="$missing $key"
done <"${ROOT}/scripts/env.schema"
check "fresh: every schema key present" "${missing:-none}" "none"
check "fresh: session secret >= 48 chars" "$([ "$(val SESSION_SECRET | wc -c)" -gt 48 ] && echo yes || echo no)" "yes"
check "fresh: comments from .env.example kept" "$(grep -c '^#' "$W/.env")" "$(grep -c '^#' "${ROOT}/.env.example")"
check "fresh: suggested emergency number for Bahrain" "$(val EMERGENCY_NUMBER)" "999"
check "fresh: doctor phone cleaned" "$(val DOCTOR_PHONE)" "+97339123456"
check "fresh: example placeholders not kept" "$(val CF_ACCESS_TEAM_DOMAIN)" ""
check "fresh: locked values safe" "$(val AUTH_DEV_FALLBACK)/$(val CF_ACCESS_TRUST_EMAIL_HEADER)" "false/false"
check "fresh: PIN never printed" "$(cat "$W/out" "$W/stdout" | grep -c 4829 || true)" "0"
check "fresh: summary hides secrets" "$(grep -c 'SESSION_SECRET=•••• (generated)' "$W/out")" "1"
rm -rf "$W"

# 2. PIN rules: 1234, 11111, 7777 refused with a reason; 4829 twice accepted.
setup
run $'Asia/Bahrain\nHussain\n1234\n1234\n11111\n11111\n7777\n7777\n4829\n4829\n\ny\n\n\n\n\n\n'
check "pin: exit code" "$(cat "$W/rc")" "0"
check "pin: 1234 refused" "$(grep -c '1234 is the first PIN anyone tries' "$W/out")" "1"
check "pin: 5 digits refused" "$(grep -c 'exactly 4 digits' "$W/out")" "1"
check "pin: same digit refused" "$(grep -c 'four of the same digit' "$W/out")" "1"
check "pin: 4829 stored" "$(val SUPER_ADMIN_PIN)" "4829"
check "pin: no PIN in output" "$(cat "$W/out" "$W/stdout" | grep -cE '1234|11111|7777|4829' | tr -d ' ')" "1"
rm -rf "$W"
# (the one match above is the reason text "1234 is the first PIN anyone tries")

# 3. Time zone: a typo is refused, the right name accepted.
setup
run $'Asia/Riyad\nAsia/Riyadh\nHussain\n4829\n4829\n\ny\n\n\n\n\n\n'
check "tz: typo refused" "$(grep -c 'not a known time zone' "$W/out")" "1"
check "tz: accepted" "$(val APP_TIMEZONE)" "Asia/Riyadh"
check "tz: Saudi number suggested" "$(val EMERGENCY_NUMBER)" "997"
rm -rf "$W"

# 4. Phone: "abc" refused, then accepted with spaces and dashes removed.
setup
run $'Asia/Bahrain\nHussain\n4829\n4829\n\ny\n\nabc\n+973 3912-3456\n\n\n\n'
check "phone: abc refused" "$(grep -c 'use digits only' "$W/out")" "1"
check "phone: stored" "$(val DOCTOR_PHONE)" "+97339123456"
rm -rf "$W"

# 5. Existing complete .env: no questions, file byte-for-byte the same.
setup
full_env
cp "$W/.env" "$W/before"
run ""
check "unchanged: exit code" "$(cat "$W/rc")" "0"
check "unchanged: byte-for-byte" "$(cmp -s "$W/.env" "$W/before" && echo same || echo changed)" "same"
check "unchanged: no backup" "$(ls -a "$W" | grep -c 'bak-' || true)" "0"
rm -rf "$W"

# 6. An update adds a required setting: exactly one question, nothing else changes.
setup
full_env
grep -v '^EMERGENCY_NUMBER=' "$W/.env" >"$W/tmp" && cat "$W/tmp" >"$W/.env"
cp "$W/.env" "$W/before"
run $'\ny\n'
check "new key: exit code" "$(cat "$W/rc")" "0"
check "new key: answered" "$(val EMERGENCY_NUMBER)" "999"
check "new key: only that line added" "$(diff "$W/before" "$W/.env" | grep -c '^[<>]')" "1"
check "new key: own settings kept" "$(val MY_OWN_SETTING)" "keep me"
check "new key: backup made" "$(ls -a "$W" | grep -c '^\.env\.bak-' || true)" "1"
check "new key: backup is private" "$(mode "$(ls -d "$W"/.env.bak-* | head -n 1)")" "600"
rm -rf "$W"

# 7. Unsafe AUTH_DEV_FALLBACK=true: stop with an explanation.
setup
full_env
sed -i.orig 's/^AUTH_DEV_FALLBACK=false/AUTH_DEV_FALLBACK=true/' "$W/.env" && rm -f "$W/.env.orig"
cp "$W/.env" "$W/before"
(bash "$SCRIPT" --env "$W/.env" --db "$W/birrcare.db" --non-interactive </dev/null >"$W/stdout" 2>"$W/out") && echo 0 >"$W/rc" || echo $? >"$W/rc"
check "unsafe, no terminal: exit 1" "$(cat "$W/rc")" "1"
check "unsafe: explained" "$(grep -c 'AUTH_DEV_FALLBACK=true is unsafe' "$W/out")" "1"
check "unsafe: file untouched" "$(cmp -s "$W/.env" "$W/before" && echo same || echo changed)" "same"
run $'n\n'
check "unsafe, answered no: exit 1" "$(cat "$W/rc")" "1"
run $'y\n'
check "unsafe, answered yes: fixed" "$(cat "$W/rc")/$(val AUTH_DEV_FALLBACK)" "0/false"
rm -rf "$W"

# 8. Interrupted write: the original .env is intact, no half file left.
setup
full_env
grep -v '^EMERGENCY_NUMBER=' "$W/.env" >"$W/tmp" && cat "$W/tmp" >"$W/.env"
cp "$W/.env" "$W/before"
(printf '\ny\n' | BIRRCARE_SETUP_TTY=1 BIRRCARE_SETUP_FAIL_BEFORE_RENAME=1 bash "$SCRIPT" --env "$W/.env" --db "$W/birrcare.db" >"$W/stdout" 2>"$W/out") || true
check "interrupted: original intact" "$(cmp -s "$W/.env" "$W/before" && echo same || echo changed)" "same"
check "interrupted: no temp file left" "$(ls -a "$W" | grep -c '\.env\.new' || true)" "0"
rm -rf "$W"

# 9. --check: nothing asked, nothing written, JSON names the missing key.
setup
full_env
grep -v '^APP_TIMEZONE=' "$W/.env" >"$W/tmp" && cat "$W/tmp" >"$W/.env"
cp "$W/.env" "$W/before"
(bash "$SCRIPT" --env "$W/.env" --db "$W/birrcare.db" --check </dev/null >"$W/stdout" 2>"$W/out") && echo 0 >"$W/rc" || echo $? >"$W/rc"
check "check: exit 2" "$(cat "$W/rc")" "2"
check "check: JSON names the key" "$(jq -r '.missing[0].key' "$W/stdout")" "APP_TIMEZONE"
check "check: question included" "$(jq -r '.missing[0].question_en' "$W/stdout" | grep -c 'time zone')" "1"
check "check: nothing written" "$(cmp -s "$W/.env" "$W/before" && echo same || echo changed)" "same"
rm -rf "$W"

# 10. --non-interactive fills what has a default and generates secrets.
setup
full_env
grep -vE '^(SESSION_SECRET|CORS_ALLOWED_ORIGINS|AUTH_DEV_FALLBACK)=' "$W/.env" >"$W/tmp" && cat "$W/tmp" >"$W/.env"
(bash "$SCRIPT" --env "$W/.env" --db "$W/birrcare.db" --non-interactive </dev/null >"$W/stdout" 2>"$W/out") && echo 0 >"$W/rc" || echo $? >"$W/rc"
check "non-interactive: exit 0" "$(cat "$W/rc")" "0"
check "non-interactive: secret generated" "$([ "$(val SESSION_SECRET | wc -c)" -gt 48 ] && echo yes || echo no)" "yes"
check "non-interactive: locked default" "$(val AUTH_DEV_FALLBACK)" "false"
rm -rf "$W"

# 11. Database exists: first-account questions are not asked.
setup
full_env
grep -vE '^SUPER_ADMIN_(USER|PIN)=' "$W/.env" >"$W/tmp" && cat "$W/tmp" >"$W/.env"
cp "$W/.env" "$W/before"
run ""
check "existing db: no first-run questions" "$(cat "$W/rc")" "0"
rm -rf "$W"

# 12. --reconfigure KEY changes only that value.
setup
full_env
run $'+97317000000\n' --reconfigure DOCTOR_PHONE
check "reconfigure: exit 0" "$(cat "$W/rc")" "0"
check "reconfigure: changed" "$(val DOCTOR_PHONE)" "+97317000000"
check "reconfigure: others kept" "$(val APP_TIMEZONE)/$(val EMERGENCY_NUMBER)" "Asia/Bahrain/999"
rm -rf "$W"

# 13. Arabic questions.
setup
run "$FRESH_ANSWERS" --lang ar
check "arabic: exit code" "$(cat "$W/rc")" "0"
check "arabic: summary in Arabic" "$(grep -c 'حُفظ في .env' "$W/out")" "1"
rm -rf "$W"

if [ "$fails" -gt 0 ]; then
  echo "$fails check(s) failed"
  exit 1
fi
echo "all setup-env checks passed"
