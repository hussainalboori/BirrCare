#!/usr/bin/env bash
set -euo pipefail

# Birrcare (رعاية بر) — Settings (.env) setup (issue #52)
#
# Asks only the questions that matter, in English or Arabic, checks every
# answer, fills in the rest, and writes .env safely. Later runs ask only for
# settings that are required but missing or invalid (e.g. a new one added by an
# update); existing values are never asked again or changed. scripts/deploy.sh
# runs it first.
#
# The questions come from scripts/env.schema; suggested ambulance numbers from
# scripts/emergency-numbers.
#
# Usage:
#   scripts/setup-env.sh                    ask what's missing (needs a terminal)
#   scripts/setup-env.sh --reconfigure [KEY…]  change values on purpose
#   scripts/setup-env.sh --check            only check: exit 0 ok, 2 needs input,
#                                           1 unsafe value; prints JSON on stdout
#   scripts/setup-env.sh --non-interactive  use defaults where there are some;
#                                           exit 2 (JSON on stdout) for the rest
# Options: --lang ar|en, --env FILE, --schema FILE, --db FILE (tests, updater).
# Test hook: BIRRCARE_SETUP_TTY=1 answers from stdin as if it were a terminal.
#
# Exit codes: 0 done, 1 unsafe value or error, 2 needs an answer.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCHEMA="${ROOT}/scripts/env.schema"
ENV_FILE="${ROOT}/.env"
EXAMPLE="${ROOT}/.env.example"
EMERGENCY_FILE="${ROOT}/scripts/emergency-numbers"
DB_FILE="${ROOT}/data/birrcare.db"
TZ_DIR="${TZ_DIR:-/usr/share/zoneinfo}"
MODE="interactive"
LANG_SEL=""
RECONFIGURE=""   # space-separated keys, or "*" for every question
RECONFIGURE_ON=""

while [ $# -gt 0 ]; do
  case "$1" in
    --check) MODE="check" ;;
    --non-interactive) MODE="non-interactive" ;;
    --reconfigure)
      RECONFIGURE_ON=1
      while [ $# -gt 1 ] && [ "${2#--}" = "$2" ]; do
        RECONFIGURE="${RECONFIGURE} $2"
        shift
      done
      [ -n "$RECONFIGURE" ] || RECONFIGURE="*"
      ;;
    --lang) LANG_SEL="$2"; shift ;;
    --env) ENV_FILE="$2"; shift ;;
    --schema) SCHEMA="$2"; shift ;;
    --db) DB_FILE="$2"; shift ;;
    -h | --help) sed -n '3,30p' "$0"; exit 0 ;;
    *) echo "setup-env: unknown option $1" >&2; exit 1 ;;
  esac
  shift
done

if [ -z "$LANG_SEL" ]; then
  case "${LANG:-}${LC_ALL:-}" in ar*) LANG_SEL=ar ;; *) LANG_SEL=en ;; esac
fi
# Questions need someone at a terminal.
INTERACTIVE=""
if [ "$MODE" = "interactive" ]; then
  if [ -t 0 ] || [ -n "${BIRRCARE_SETUP_TTY:-}" ]; then
    INTERACTIVE=1
  else
    MODE="non-interactive"
  fi
fi

t() { if [ "$LANG_SEL" = "ar" ]; then printf '%s' "$2"; else printf '%s' "$1"; fi; }
say() { printf '%s\n' "$*" >&2; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }

# ---------------------------------------------------------------------------
# The working copy of .env
# ---------------------------------------------------------------------------
WORK="$(mktemp "${TMPDIR:-/tmp}/birrcare-env.XXXXXX")"
NEW_FILE=""
trap 'rm -f "$WORK" ${NEW_FILE:+"$NEW_FILE"}' EXIT
FRESH=""
if [ -f "$ENV_FILE" ]; then
  cat "$ENV_FILE" >"$WORK"
else
  FRESH=1
  # Start from .env.example to keep its explanations, with every value empty:
  # the example values (Asia/Riyadh, 1234…) are placeholders, not answers.
  if [ -f "$EXAMPLE" ]; then
    sed -E 's/^([A-Z][A-Z0-9_]*)=.*/\1=/' "$EXAMPLE" >"$WORK"
  fi
fi

env_has() { grep -q "^$1=" "$WORK"; }
env_get() {
  local v
  v="$(grep -m1 "^$1=" "$WORK" | cut -d= -f2- || true)"
  v="${v%$'\r'}"
  case "$v" in \"*\") v="${v#\"}"; v="${v%\"}" ;; \'*\') v="${v#\'}"; v="${v%\'}" ;; esac
  printf '%s' "$v"
}
env_set() {
  local tmp
  tmp="$(mktemp "${TMPDIR:-/tmp}/birrcare-env.XXXXXX")"
  if env_has "$1"; then
    KEY="$1" VAL="$2" awk 'BEGIN { k = ENVIRON["KEY"]; v = ENVIRON["VAL"]; done = 0 }
      !done && index($0, k "=") == 1 { print k "=" v; done = 1; next } { print }' "$WORK" >"$tmp"
  else
    cat "$WORK" >"$tmp"
    [ -s "$tmp" ] && [ -n "$(tail -c 1 "$tmp")" ] && printf '\n' >>"$tmp"
    printf '%s=%s\n' "$1" "$2" >>"$tmp"
  fi
  cat "$tmp" >"$WORK"
  rm -f "$tmp"
}

# ---------------------------------------------------------------------------
# Defaults and validators
# ---------------------------------------------------------------------------
gen_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 48 | tr -d '\n'
  else
    head -c 48 /dev/urandom | base64 | tr -d '\n'
  fi
}

detect_tz() {
  local tz=""
  if command -v timedatectl >/dev/null 2>&1; then
    tz="$(timedatectl show -p Timezone --value 2>/dev/null || true)"
  fi
  [ -z "$tz" ] && [ -f /etc/timezone ] && tz="$(head -n 1 /etc/timezone)"
  [ -z "$tz" ] && [ -L /etc/localtime ] && tz="$(readlink /etc/localtime | sed 's#.*/zoneinfo/##')"
  case "$tz" in "" | UTC | Etc/*) tz="" ;; esac
  printf '%s' "$tz"
}

emergency_for_tz() {
  local tz n=""
  tz="$(env_get APP_TIMEZONE)"
  [ -f "$EMERGENCY_FILE" ] && n="$(grep -v '^#' "$EMERGENCY_FILE" | awk -F'|' -v tz="$tz" '$1 == tz { print $2; exit }')"
  printf '%s' "${n:-112}"
}

resolve_default() {
  case "$1" in
    @detect-tz) detect_tz ;;
    @emergency-for-tz) emergency_for_tz ;;
    @random48) gen_secret ;;
    *) printf '%s' "$1" ;;
  esac
}

# validate VALIDATOR VALUE → sets NORM (value to store) or REASON; returns 1 if invalid.
NORM=""
REASON=""
valid_ipv4() {
  local ip="$1" o
  [[ "$ip" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || return 1
  for o in ${ip//./ }; do [ "$o" -le 255 ] || return 1; done
}
valid_ip() { valid_ipv4 "$1" || [[ "$1" =~ ^[0-9a-fA-F:]+$ && "$1" == *:* ]]; }
validate() {
  local kind="$1" v="$2" x
  NORM="$v"
  REASON=""
  case "$kind" in
    tz)
      if [[ "$v" =~ ^[A-Za-z_]+(/[A-Za-z0-9_+-]+)+$ ]] && [ -f "${TZ_DIR}/${v}" ]; then return 0; fi
      REASON="$(t "not a known time zone (examples: Asia/Bahrain, Asia/Riyadh, Europe/London)" "ليست منطقة زمنية معروفة (أمثلة: Asia/Bahrain، Asia/Riyadh)")" ;;
    pin4)
      if ! [[ "$v" =~ ^[0-9]{4}$ ]]; then
        REASON="$(t "the PIN must be exactly 4 digits" "الرمز يجب أن يكون 4 أرقام بالضبط")"
      elif [ "$v" = "1234" ]; then
        REASON="$(t "1234 is the first PIN anyone tries; choose another" "1234 أول رمز يجربه أي شخص؛ اختر غيره")"
      elif [ "$v" = "${v:0:1}${v:0:1}${v:0:1}${v:0:1}" ]; then
        REASON="$(t "four of the same digit is too easy to guess" "تكرار الرقم نفسه أربع مرات سهل التخمين")"
      else
        return 0
      fi ;;
    phone)
      x="$(printf '%s' "$v" | tr -d ' ().-')"
      if [[ "$x" =~ ^\+?[0-9]{3,15}$ ]]; then NORM="$x"; return 0; fi
      REASON="$(t "use digits only, optionally starting with + (3 to 15 digits)" "أرقام فقط، ويمكن أن يبدأ بـ + (من 3 إلى 15 رقماً)")" ;;
    name | text)
      local max=40
      [ "$kind" = "text" ] && max=80
      local bad_chars='[#|"$`\\]'
      if [ -n "$v" ] && [ "${#v}" -le "$max" ] && ! [[ "$v" =~ $bad_chars ]]; then return 0; fi
      REASON="$(t "use plain letters, at most $max characters, without # | \" \$ \` or \\" "أحرف عادية فقط، بحد أقصى $max حرفاً، بدون الرموز # | \" \$ \` \\")" ;;
    domain)
      x="$(printf '%s' "$v" | tr '[:upper:]' '[:lower:]')"
      x="${x#https://}"
      x="${x%/}"
      if [[ "$x" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?\.cloudflareaccess\.com$ ]]; then NORM="$x"; return 0; fi
      REASON="$(t "must look like yourteam.cloudflareaccess.com" "يجب أن يكون مثل yourteam.cloudflareaccess.com")" ;;
    aud)
      x="$(printf '%s' "$v" | tr '[:upper:]' '[:lower:]')"
      if [[ "$x" =~ ^[a-f0-9]{64}$ ]]; then NORM="$x"; return 0; fi
      REASON="$(t "the AUD tag is 64 letters and digits (Access → Applications → your app → Overview)" "رمز AUD مكوّن من 64 حرفاً ورقماً")" ;;
    email-or-url)
      if [[ "$v" =~ ^mailto:[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$ ]] || [[ "$v" =~ ^https://[^[:space:]]+$ ]]; then return 0; fi
      if [[ "$v" =~ ^[^@[:space:]:]+@[^@[:space:]]+\.[^@[:space:]]+$ ]]; then NORM="mailto:$v"; return 0; fi
      REASON="$(t "give an email address or an https:// link" "أدخل بريداً إلكترونياً أو رابط https://")" ;;
    ips | cidrs)
      local part ok=1 ip bits
      for part in ${v//,/ }; do
        ip="${part%%/*}"
        bits=""
        [ "$part" != "$ip" ] && bits="${part#*/}"
        valid_ip "$ip" || ok=""
        [ "$kind" = "cidrs" ] && ! [[ "$bits" =~ ^[0-9]{1,3}$ ]] && ok=""
      done
      [ -n "$ok" ] && return 0
      REASON="$(t "give IP addresses separated by commas, e.g. 203.0.113.7" "أدخل عناوين IP مفصولة بفواصل، مثل 203.0.113.7")" ;;
    origins)
      local o2 ok2=1
      for o2 in ${v//,/ }; do [[ "$o2" =~ ^https?://[A-Za-z0-9.-]+(:[0-9]+)?$ ]] || ok2=""; done
      [ -n "$ok2" ] && return 0
      REASON="$(t "give origins like https://care.example.com, separated by commas" "أدخل عناوين مثل https://care.example.com مفصولة بفواصل")" ;;
    secret48)
      if [ "${#v}" -ge 48 ] && ! [[ "$v" =~ [[:space:]] ]]; then return 0; fi
      REASON="too short" ;;
    false-only)
      [ "$v" = "false" ] && return 0
      REASON="must be false" ;;
    empty-in-normal-runs | "") return 0 ;;
    *) REASON="unknown validator $kind" ;;
  esac
  return 1
}

required_now() {
  case "$1" in
    yes) return 0 ;;
    first-run) [ ! -f "$DB_FILE" ] ;;
    if:*) [ -n "$(env_get "${1#if:}")" ] ;;
    *) return 1 ;;
  esac
}

wants_reconfigure() {
  [ -n "$RECONFIGURE_ON" ] || return 1
  [ "$RECONFIGURE" = "*" ] && return 0
  case " $RECONFIGURE " in *" $1 "*) return 0 ;; esac
  return 1
}

# ---------------------------------------------------------------------------
# Asking
# ---------------------------------------------------------------------------
read_line() { # read_line VAR PROMPT
  local __ans
  if ! IFS= read -r -p "$2" __ans; then
    say ""
    red "$(t "No answer given; stopping without changing anything." "لم تُدخل إجابة؛ توقف دون تغيير شيء.")"
    exit 2
  fi
  printf -v "$1" '%s' "$__ans"
}

read_secret() { # read_secret VAR PROMPT
  local __a
  if ! IFS= read -rs -p "$2" __a; then
    say ""
    red "$(t "No answer given; stopping without changing anything." "لم تُدخل إجابة؛ توقف دون تغيير شيء.")"
    exit 2
  fi
  say ""
  printf -v "$1" '%s' "$__a"
}

CHANGED=""
note_change() { CHANGED="${CHANGED}$1|$2"$'\n'; }

ask() { # ask KEY KIND REQUIRED DEFAULT VALIDATOR QEN QAR
  local key="$1" kind="$2" req="$3" def="$4" val="$5" q a b suggestion needed=""
  q="$(t "$6" "$7")"
  required_now "$req" && needed=1
  suggestion=""
  if [ "$kind" != "secret" ]; then
    suggestion="$(env_get "$key")"
    [ -z "$suggestion" ] && suggestion="$(resolve_default "$def")"
  fi
  while :; do
    if [ "$kind" = "secret" ]; then
      read_secret a "$q: "
      if [ -z "$a" ] && [ -z "$needed" ]; then return 0; fi
      read_secret b "$(t "Type it again" "اكتبه مرة أخرى"): "
      if [ "$a" != "$b" ]; then
        red "$(t "The two entries don't match; try again." "الإدخالان غير متطابقين؛ حاول مرة أخرى.")"
        continue
      fi
    else
      if [ -n "$suggestion" ]; then
        read_line a "$q [$suggestion]: "
        [ -z "$a" ] && a="$suggestion"
      else
        read_line a "$q: "
      fi
    fi
    if [ -z "$a" ]; then
      if [ -z "$needed" ]; then
        env_set "$key" ""
        return 0
      fi
      red "$(t "This one is needed." "هذا مطلوب.")"
      continue
    fi
    if ! validate "$val" "$a"; then
      red "$(t "Not accepted" "غير مقبول"): ${REASON}"
      continue
    fi
    if [ "$key" = "EMERGENCY_NUMBER" ]; then
      local yn
      read_line yn "$(t "Is ${NORM} the number for an ambulance where the patient lives? [Y/n]" "هل ${NORM} هو رقم الإسعاف حيث يعيش المريض؟ [Y/n]") "
      case "$yn" in n* | N* | ل*) suggestion=""; continue ;; esac
    fi
    if [ "$(env_get "$key")" != "$NORM" ] || ! env_has "$key"; then
      env_set "$key" "$NORM"
      note_change "$key" "$kind"
    fi
    return 0
  done
}

# ---------------------------------------------------------------------------
# Locked settings: these must keep their safe value
# ---------------------------------------------------------------------------
FATAL=""
check_locked() { # check_locked KEY DEFAULT VALIDATOR
  local key="$1" def="$2" val="$3" cur
  cur="$(env_get "$key")"
  if [ "$val" = "empty-in-normal-runs" ]; then
    if [ -n "$cur" ]; then
      red "$(t "${key} is set: it turns off two-step login for that account on every restart. It is only for a one-time recovery." "${key} مضبوط: يعطّل تسجيل الدخول بخطوتين لذلك الحساب عند كل تشغيل. هو للاسترداد لمرة واحدة فقط.")"
      if [ -n "$INTERACTIVE" ]; then
        local yn
        read_line yn "$(t "Clear it now? [Y/n]" "مسحه الآن؟ [Y/n]") "
        case "$yn" in n* | N* | ل*) ;; *) env_set "$key" ""; note_change "$key" locked ;; esac
      fi
    fi
    return 0
  fi
  if [ -z "$cur" ] || ! env_has "$key"; then
    [ "$MODE" != "check" ] && env_set "$key" "$def"
    return 0
  fi
  validate "$val" "$cur" && return 0
  red "$(t "${key}=${cur} is unsafe: anyone could act as a caregiver without a PIN. It must be ${def}." "${key}=${cur} غير آمن: يمكن لأي شخص التصرف كمقدم رعاية دون رمز. يجب أن يكون ${def}.")"
  if [ -n "$INTERACTIVE" ]; then
    local yn
    read_line yn "$(t "Set it back to ${def}? [Y/n]" "إعادته إلى ${def}؟ [Y/n]") "
    case "$yn" in
      n* | N* | ل*) FATAL=1 ;;
      *) env_set "$key" "$def"; note_change "$key" locked ;;
    esac
  else
    FATAL=1
  fi
}

# ---------------------------------------------------------------------------
# Go through the schema
# ---------------------------------------------------------------------------
[ -f "$SCHEMA" ] || { echo "setup-env: schema not found: $SCHEMA" >&2; exit 1; }

MISSING=""   # KEY|question en|question ar lines
if [ -n "$INTERACTIVE" ] && [ -n "$FRESH" ]; then
  say "$(t "Birrcare setup: a few questions. Press Enter to accept the suggestion in [brackets]." "إعداد رعاية بر: بعض الأسئلة. اضغط Enter لقبول الاقتراح بين [القوسين].")"
fi

# The schema is read on fd 3, so the questions read answers from the terminal.
while IFS='|' read -r -u 3 key kind req def val qen qar; do
  case "$key" in "" | \#*) continue ;; esac
  cur="$(env_get "$key")"
  case "$kind" in
    locked)
      check_locked "$key" "$def" "$val"
      ;;
    generate)
      if ! validate "$val" "$cur"; then
        if [ "$MODE" != "check" ]; then
          env_set "$key" "$(resolve_default "$def")"
          note_change "$key" generate
        fi
      fi
      ;;
    default)
      if [ -n "$INTERACTIVE" ] && wants_reconfigure "$key"; then
        ask "$key" "$kind" "$req" "$def" "$val" "${qen:-$key}" "${qar:-$key}"
      elif ! env_has "$key" && [ "$MODE" != "check" ]; then
        env_set "$key" "$def"
      fi
      ;;
    ask | secret)
      bad=""
      if [ "$req" = "first-run" ] && [ -f "$DB_FILE" ] && ! wants_reconfigure "$key"; then
        continue # only used to create the first account; the database exists
      fi
      if [ -n "$cur" ] && ! validate "$val" "$cur"; then
        bad=1
        if [ "$kind" = "secret" ]; then
          say "$(t "The current value of ${key} is not valid: ${REASON}" "القيمة الحالية لـ ${key} غير صالحة: ${REASON}")"
        else
          say "$(t "The current value of ${key} (${cur}) is not valid: ${REASON}" "القيمة الحالية لـ ${key} (${cur}) غير صالحة: ${REASON}")"
        fi
      fi
      # Optional questions are asked on the first run only, and an "if:" one
      # only when the setting it depends on was given.
      optional_now=""
      [ -n "$FRESH" ] && { [ "${req#if:}" = "$req" ] || required_now "$req"; } && optional_now=1
      if [ -n "$INTERACTIVE" ] && { wants_reconfigure "$key" || [ -n "$bad" ] || { [ -z "$cur" ] && { required_now "$req" || [ -n "$optional_now" ]; }; }; }; then
        ask "$key" "$kind" "$req" "$def" "$val" "$qen" "$qar"
      elif [ -n "$bad" ] || { [ -z "$cur" ] && required_now "$req"; }; then
        # Nobody to ask: use the default if there is one and it's valid.
        d=""
        [ -n "$def" ] && [ -z "$bad" ] && d="$(resolve_default "$def")"
        if [ -n "$d" ] && validate "$val" "$d" && [ "$MODE" != "check" ]; then
          env_set "$key" "$NORM"
          note_change "$key" default
        else
          MISSING="${MISSING}${key}|${qen}|${qar}"$'\n'
        fi
      elif ! env_has "$key" && [ "$MODE" != "check" ]; then
        env_set "$key" "" # list it, empty (optional)
      fi
      ;;
  esac
done 3<"$SCHEMA"

json_escape() { printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'; }
print_missing_json() {
  local first=1 k qe qa
  printf '{"missing":['
  while IFS='|' read -r k qe qa; do
    [ -z "$k" ] && continue
    [ -z "$first" ] && printf ','
    first=""
    printf '{"key":"%s","question_en":"%s","question_ar":"%s"}' "$k" "$(json_escape "$qe")" "$(json_escape "$qa")"
  done <<<"$MISSING"
  printf '],"unsafe":%s}\n' "$([ -n "$FATAL" ] && echo true || echo false)"
}

if [ -n "$FATAL" ]; then
  [ "$MODE" != "interactive" ] && print_missing_json
  red "$(t "Stopping: fix the unsafe setting above, then run ./scripts/deploy.sh again. Nothing was changed." "توقف: أصلح الإعداد غير الآمن أعلاه، ثم شغّل ./scripts/deploy.sh مجدداً. لم يتغير شيء.")"
  exit 1
fi

if [ "$MODE" = "check" ]; then
  print_missing_json
  [ -z "$MISSING" ] && exit 0
  exit 2
fi
if [ -n "$MISSING" ]; then
  print_missing_json
  say "$(t "These settings need an answer. Run ./scripts/deploy.sh on the server (in a terminal) to answer them:" "هذه الإعدادات تحتاج إجابة. شغّل ./scripts/deploy.sh على الخادم (في الطرفية) للإجابة:")"
  while IFS='|' read -r k qe qa; do [ -n "$k" ] && say "  - $(t "$qe" "$qa") ($k)"; done <<<"$MISSING"
  exit 2
fi

# ---------------------------------------------------------------------------
# Write safely: backup, temp file in the same folder, then rename
# ---------------------------------------------------------------------------
if [ -f "$ENV_FILE" ] && cmp -s "$WORK" "$ENV_FILE"; then
  exit 0 # nothing to change; the file stays byte-for-byte the same
fi
dir="$(dirname "$ENV_FILE")"
if [ -f "$ENV_FILE" ]; then
  backup="${ENV_FILE}.bak-$(date +%Y%m%d-%H%M%S)"
  (umask 077 && cp -p "$ENV_FILE" "$backup")
  chmod 600 "$backup"
  # Keep the last 5 backups.
  ls -1t "${ENV_FILE}".bak-* 2>/dev/null | tail -n +6 | while IFS= read -r old; do rm -f -- "$old"; done
fi
NEW_FILE="$(mktemp "${dir}/.env.new.XXXXXX")"
new="$NEW_FILE"
cat "$WORK" >"$new"
chmod 600 "$new"
if [ -n "${BIRRCARE_SETUP_FAIL_BEFORE_RENAME:-}" ]; then
  # Test hook: an interrupted run must leave the original .env untouched.
  exit 1
fi
mv -f "$new" "$ENV_FILE"
NEW_FILE=""

# Summary, without secret values.
if [ -n "$CHANGED" ]; then
  say "$(t "Saved to .env:" "حُفظ في .env:")"
  while IFS='|' read -r k how; do
    [ -z "$k" ] && continue
    case "$how" in
      secret) say "  ${k}=•••• ($(t set ضُبط))" ;;
      generate) say "  ${k}=•••• ($(t generated "أُنشئ تلقائياً"))" ;;
      *) say "  ${k}=$(env_get "$k")" ;;
    esac
  done <<<"$CHANGED"
fi
exit 0
