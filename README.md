# Birrcare (رعاية بر)

A self-hosted, mobile-first web app that helps siblings coordinate 24/7 home care for a parent recovering from a stroke.

## Overview

Birrcare is the family's single source of truth for medication doses, stroke vitals, insulin, shift handovers and who is on duty. Every phone stays in sync live: when one sibling marks a dose, every other open phone updates without a refresh, and the same dose can never be recorded twice.

Main capabilities:

- **Medication schedule with two double-dose locks.** Doses are scheduled per slot (`night`, `morning`, `afternoon`, `evening`, `bedtime`), clock time and day of week.
  - **Same card:** a partial unique index allows one active entry per `(medication_id, scheduled_date, slot)`. A second sibling gets `409` with who gave it and when.
  - **Same medicine across cards:** a minimum interval blocks, for example, last night's bedtime dose being repeated at 03:45.
- **Honest dose records.** A dose can be recorded as taken, partly taken, refused or skipped with a reason, and "vomited N min later" can be added afterwards. Partial and vomited doses show a "call the doctor or pharmacist" warning; the app never suggests a re-dose.
- **Mis-tap recovery.** A 10-second Undo after every tap. Later, the person who recorded it can cancel within 10 minutes, or an admin any time with a reason. Cancelled entries are kept, struck through, and never count as given.
- **Stroke vitals.** Blood pressure, pulse, blood sugar (mmol/L) and alertness (`alert`, `confused`, `restless`, `drowsy`), with a colour-coded BP badge and a crisis screen with one-tap call buttons. Nothing is pre-filled: an unmeasured sugar or unassessed alertness is stored as empty, never as a default.
- **Insulin.** An admin enters the doctor's correction scale or fixed dose; the app looks it up but never calculates a dose, refuses correction insulin below 3.9 mmol/L, and locks a second injection inside the plan's minimum interval.
- **Shift handovers and duty roster.** Handover notes (with an urgent flag), shift presets (24h, 4h, 8h, 12h or custom) and daily assignments.
- **Doctor summary.** A printable report of the last 7–90 days: BP/pulse/sugar averages, crisis episodes, adherence, alertness distribution, insulin vs. scale, and recent notes.
- **Arabic and English**, with right-to-left / left-to-right layout chosen per device.
- **PIN profiles with roles**, optional Cloudflare Access sign-in, and authenticator-app 2FA for the Super Admin.

## Architecture

```
Phone ──HTTPS──► Cloudflare Access ──tunnel──► 127.0.0.1:3000  Next.js frontend
                                                   │  /api/v1/* proxy (incl. SSE)
                                                   ▼
                                               127.0.0.1:8080  Go backend ──► SQLite (WAL)
                                                                              ./data/birrcare.db
```

- **Backend** (`backend/`): Go, [chi](https://github.com/go-chi/chi) router, pure-Go SQLite (`modernc.org/sqlite`, no CGO) in WAL mode with a single pooled connection, and an in-memory Server-Sent Events broker. Migrations run automatically at startup.
- **Frontend** (`frontend/`): Next.js 16 (App Router), React 19, TypeScript, Tailwind CSS 4. The browser only talks to the frontend; `src/app/api/v1/[...path]/route.ts` forwards `/api/v1/*` (including the SSE stream) to the backend, so cookies stay same-origin.
- Both containers publish their ports on `127.0.0.1` only. Public access is expected to go through a Cloudflare Tunnel on the host.

## Requirements

- Docker with Docker Compose (the normal way to run it).
- For development without Docker: Go 1.26 (`backend/go.mod`) and Node.js 24 (same as the frontend image and CI).

## Quick start (Docker Compose)

```bash
git clone <this repository>
cd BirrCare
cp .env.example .env      # then edit it, see Configuration
docker compose up -d --build
```

- Frontend: <http://127.0.0.1:3000>
- Backend health check: `curl http://127.0.0.1:8080/api/v1/health` → `{"status":"healthy","service":"birrcare-backend"}`

On first start, with an empty database, the backend creates one Super Admin (id `cg_superadmin`) from `SUPER_ADMIN_USER` / `SUPER_ADMIN_PIN` (defaults `admin` / `1234`). Sign in with that PIN, change it (the app refuses `1234` and repeated digits as a new PIN), then add the family under user management.

The database lives in `./data/birrcare.db` on the host (mounted at `/data`).

## Configuration

Copy `.env.example` to `.env`; Docker Compose reads it and passes the values to the backend container.

| Variable | Default | Description |
|---|---|---|
| `SUPER_ADMIN_USER` | `admin` | Name of the Super Admin seeded when the database is empty. |
| `SUPER_ADMIN_PIN` | `1234` | 4-digit PIN for that seeded account. |
| `APP_TIMEZONE` | `UTC` in Compose (process local zone otherwise) | IANA zone of the household, e.g. `Asia/Riyadh`. Decides which day a dose belongs to and the export's date range. |
| `EMERGENCY_NUMBER` | empty | Call button shown on a BP crisis. `.env.example` suggests `997`. Empty hides the button. |
| `DOCTOR_NAME`, `DOCTOR_PHONE` | empty | Doctor call button on a BP crisis and on partial or vomited doses. |
| `CF_ACCESS_TEAM_DOMAIN` | empty | Cloudflare Zero Trust team domain, e.g. `yourteam.cloudflareaccess.com`. |
| `CF_ACCESS_AUD` | empty | Access application AUD tag. Cloudflare sign-in is enabled only when both CF values are set. |
| `CF_ACCESS_TRUST_EMAIL_HEADER` | `false` | Trust the raw `Cf-Access-Authenticated-User-Email` header without verifying the JWT. Spoofable; leave off. |
| `SESSION_SECRET` | empty | Key for signing session cookies, at least 32 characters. If empty, a random key is generated on first boot and stored in the database. Changing it signs everyone out. |
| `DISABLE_2FA_FOR` | empty | Break-glass: a caregiver id whose 2FA is cleared on startup. Set, restart once, then remove it. |
| `AUTH_DEV_FALLBACK` | `false` in Compose; on when `APP_ENV` is not `production` | Treat anonymous requests as the first active caregiver. Never enable in production. |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:3000,http://127.0.0.1:3000` | Comma-separated origins that may call the backend directly. Not needed behind the frontend proxy. |

Set by `docker-compose.yml` (relevant when running outside Docker):

| Variable | Service | Value in Compose | Default in code |
|---|---|---|---|
| `PORT` | backend | `8080` | `8080` |
| `DB_PATH` | backend | `/data/birrcare.db` | `./data/birrcare.db` |
| `APP_ENV` | backend | `production` (Secure cookies, no dev fallback) | unset |
| `INTERNAL_BACKEND_URL` | frontend | `http://birrcare-backend:8080` | same |
| `NEXT_PUBLIC_API_URL` | frontend | `/api/v1` | `/api/v1` |

## Running locally without Docker

```bash
# Backend (http://localhost:8080, database in backend/data/birrcare.db)
cd backend
APP_TIMEZONE=Asia/Riyadh go run .

# Frontend (http://localhost:3000), in another terminal
cd frontend
npm install
INTERNAL_BACKEND_URL=http://localhost:8080 npm run dev
```

Without `APP_ENV=production` the backend sets non-Secure cookies (so they work over plain `http://localhost`) and turns on `AUTH_DEV_FALLBACK`, which acts as the first active caregiver for anonymous requests. Set `AUTH_DEV_FALLBACK=false` to test the real PIN flow.

## Authentication and roles

The backend identifies the caregiver for each request, in this order:

1. **Session cookie** `birrcare_session` (HttpOnly, `SameSite=Strict`, Secure in production, valid 30 days), issued by `POST /api/v1/auth/verify-pin`.
2. **Cloudflare Access JWT** from the `Cf-Access-Jwt-Assertion` header or `CF_Authorization` cookie, verified against the team's keys, matched to `caregivers.email`. Only when `CF_ACCESS_TEAM_DOMAIN` and `CF_ACCESS_AUD` are set.
3. **Raw `Cf-Access-Authenticated-User-Email` header**, only if `CF_ACCESS_TRUST_EMAIL_HEADER=true`.
4. **Dev fallback**, only if `AUTH_DEV_FALLBACK` is on.

PIN login is rate limited per caregiver **and device**. After 5 wrong PINs the device is locked for 30 seconds, doubling up to 15 minutes, with `429` and `Retry-After`.
- A browser that has entered that caregiver's PIN correctly before (`birrcare_device` cookie) has its own allowance, so someone guessing elsewhere can't lock the family's phones out.
- All other browsers share one allowance per caregiver.

An account with 2FA gets `{"status":"2fa_required","challenge":…}` from `verify-pin` and finishes with `POST /api/v1/auth/verify-2fa`.

Sessions can be revoked on the server:
- **Logging out or switching profile** revokes that exact cookie.
- **Changing or resetting a PIN, a role change, deactivation, or turning 2FA on or off** signs the caregiver out on every other device.
- **"Sign out my other devices"** (`POST /auth/sign-out-others`) does the same on request.

| Role | Can do |
|---|---|
| `super_admin` | Everything, including users, shifts and 2FA. |
| `admin` | Manage medications, schedules and insulin plans; override an insulin or medication interval lock and cancel any dose entry (with a reason); log care. |
| `caregiver` | Log doses, vitals, insulin and handover notes. |
| `viewer` | Read only. |

## Your first API call

From a shell, sign in with the seeded Super Admin and read today's schedule (`-c`/`-b` keep the session cookie). Against the Compose setup, call the backend on port 8080; in production the cookie is `Secure`, so curl over plain HTTP won't send it back, which makes this example suited to a local backend started without `APP_ENV=production`.

```bash
# 1. Pick a caregiver id (public endpoint)
curl -s http://localhost:8080/api/v1/caregivers

# 2. Sign in with the PIN
curl -s -c cookies.txt -H "Content-Type: application/json" \
     -d '{"caregiver_id":"cg_superadmin","pin":"1234"}' \
     http://localhost:8080/api/v1/auth/verify-pin

# 3. Today's doses with their logged status
curl -s -b cookies.txt "http://localhost:8080/api/v1/medications/schedule"
```

`verify-pin` returns `{"status":"success","caregiver":{…}}` and sets the cookie. The schedule is `{"date":"…","schedule":[…]}`. Each entry has the medication, its `slot` and scheduled `time`, the active `log` if one was recorded, any `voided_logs`, and the interval-lock fields `last_taken`, `interval_hours` and `next_allowed_at`.

## API reference

Base path `/api/v1`. Derived from `backend/internal/server/router.go` and the handlers; there is no OpenAPI spec. All endpoints except the public ones need an authenticated caregiver (else `401`). Role checks happen in the handlers (`403`). Every API response carries `Cache-Control: no-store`, `X-Content-Type-Options: nosniff` and a `frame-ancestors 'none'` CSP.

**Public**

| Method | Path | Description |
|---|---|---|
| GET | `/health` (also `/health` at the root) | Health check. |
| GET | `/caregivers` | Active caregivers for the profile switcher: `id`, `name`, `avatar_color` only (no email, role or phone). |
| POST | `/auth/verify-pin` | `{caregiver_id, pin}` → session cookie, or a 2FA challenge. `401` wrong PIN, `404` unknown caregiver, `429` locked out. |
| POST | `/auth/verify-2fa` | Second login step: the challenge plus an authenticator or recovery code. |
| POST | `/auth/logout` | Revoke this session cookie and clear it. |
| GET | `/contacts` | `{emergency_number, doctor_name, doctor_phone}` from the environment. |

**Account**

| Method | Path | Description |
|---|---|---|
| GET | `/auth/me` | The signed-in caregiver. |
| POST | `/auth/change-pin` | Change your own PIN (current PIN required); signs out your other devices. |
| POST | `/auth/sign-out-others` | Keep this device signed in, sign out every other one. |
| POST | `/auth/2fa/setup` · `/auth/2fa/enable` · `/auth/2fa/disable` | Authenticator-app 2FA (Super Admin). Enabling returns 8 one-time recovery codes. |

**Users** (Super Admin)

| Method | Path | Description |
|---|---|---|
| GET | `/users` | All users, including inactive, with email and role. |
| POST | `/users` | Create a user with a role and 4-digit PIN. |
| PUT | `/users/{id}` | Update profile and role. A role change signs that user out everywhere. |
| POST | `/users/{id}/reset-pin` | Set a new PIN; signs that user out everywhere and forgets their trusted devices. |
| DELETE | `/users/{id}` | Toggle active / inactive (signs them out). |

**Medications**

| Method | Path | Description |
|---|---|---|
| GET | `/medications` | Medication catalogue. |
| POST | `/medications` · PUT `/medications/{id}` · DELETE `/medications/{id}` | Manage medications (admin). Accepts `min_hours_between`: `null` = automatic, `0` = no interval lock, up to `72`. |
| PUT | `/medications/{id}/schedule` | Days of week and clock time per slot (admin). |
| GET | `/medications/schedule?date=YYYY-MM-DD` | Doses for a day (default: today in `APP_TIMEZONE`), see [Your first API call](#your-first-api-call). |
| POST | `/medications/log` | Record a dose, see below. |
| POST | `/medications/log/{id}/void` | `{reason}`: cancel an entry. The person who recorded it may within 10 minutes (reason optional); otherwise admin only, with a reason. `403 VOID_NOT_ALLOWED`, `400 REASON_REQUIRED`, `409 ALREADY_VOIDED`. Nothing is deleted. |
| POST | `/medications/log/{id}/event` | `{type: "vomited", minutes_after}`: follow-up on a `taken`/`partial` dose. `400 NOT_GIVEN` / `LOG_VOIDED`, `409 EVENT_ALREADY_LOGGED`. |

`POST /medications/log` body:

| Field | Required | Notes |
|---|---|---|
| `medication_id`, `slot` | yes | The slot must be scheduled for that medication. |
| `scheduled_date` | yes | `YYYY-MM-DD`, at most tomorrow. |
| `status` | no | `taken` (default), `partial`, `refused` or `skipped`. |
| `notes` | no | e.g. the skip reason. |
| `given_at` | no | RFC3339, at most 36 h back: when a dose ticked late was really given. |
| `override_interval`, `override_reason` | no | Admin only: give a dose inside the minimum interval (doctor's order). |

Responses:

| Status | Meaning |
|---|---|
| `201` | Recorded; the body includes `log_id`. |
| `409 DOSE_ALREADY_LOGGED` | This card already has an active entry; the body says who recorded it and when. |
| `409 DOSE_TOO_SOON` | `taken`/`partial` within the interval of another given dose of that medication; the body has `caregiver_name`, `last_taken_at`, `hours_since`, `min_hours`, `next_allowed_at`. |
| `400` | Invalid input or slot not scheduled. `USE_EVENT` for `status: "vomited"`. |
| `403` | Viewer, or a non-admin override. |
| `404` | Medication not found or inactive. |

The interval check runs inside the same immediate-lock transaction as the insert. The default interval is ¾ of the shortest gap between the medication's scheduled times: once daily → 18 h, 08:00 and 20:00 → 9 h.

**Vitals**

| Method | Path | Description |
|---|---|---|
| GET | `/vitals?limit=` | Latest readings (default 30, max 100). `alertness_level` is `null` when not assessed; `suspect_default_sugar: true` marks an old reading that may be the former form's pre-filled 6.4. |
| POST | `/vitals` | Log any of: `systolic` + `diastolic` (both together), `pulse`, `blood_sugar` (1–45 mmol/L), `alertness_level`, `notes`, optional `vital_type` (`bp`, `sugar`, `alertness`, `full`). At least BP, sugar or alertness is required. A missing alertness is stored as `NULL`, never as `alert`. |

**Insulin**

| Method | Path | Description |
|---|---|---|
| GET | `/insulin/plans?all=1` | Plans (`all=1` includes inactive). |
| POST | `/insulin/plans` · PUT `/insulin/plans/{id}` | Doctor's scale or fixed dose (admin). |
| GET | `/insulin/status` | Per plan: last injection and whether the next one is locked. |
| POST | `/insulin/log` | Record an injection (`given` or `held`) with the reading taken now. `409 INSULIN_TOO_SOON` inside the minimum interval; `HYPO_NO_INSULIN` below 3.9 mmol/L on a scale plan. |

**Handovers, export, shifts**

| Method | Path | Description |
|---|---|---|
| GET | `/handovers?limit=` | Latest notes (default 20, max 100). |
| POST | `/handovers` | `{shift_type, summary, urgent_flag}`; `shift_type` is a shift id or label (default `morning`). |
| GET | `/export/summary?days=` | Doctor summary for the last N days (default 7, max 90). Includes `medication_counts` per medication (taken, partial, vomited, refused, skipped), an alertness distribution with a `not_assessed` bucket, and `unverified_sugar_count`; cancelled doses and unverified sugar readings are left out of the figures. |
| GET | `/shifts` | Shift definitions (everyone). |
| POST | `/shifts` · PUT `/shifts/{id}` · DELETE `/shifts/{id}` | Manage shifts (Super Admin; delete is a soft delete). |
| POST | `/shifts/presets` | `{preset}`: `24h`, `4h`, `8h` or `12h` (Super Admin). |
| GET | `/shifts/assignments?date=` | Who is assigned on a day. |
| POST | `/shifts/assignments` · DELETE `/shifts/assignments/{id}` | Assign / unassign (Super Admin). |
| GET | `/shifts/current-duty` | Who is on duty right now. |

### Live updates (SSE)

`GET /api/v1/events/stream` (authenticated) is a Server-Sent Events stream. It starts with a `connected` event, sends a `: ping` comment every 15 s, and then one `message` event per change:

```
event: message
data: {"id":"…","type":"MED_LOGGED","timestamp":"…","actor":{"id":"…","name":"…","avatar_color":"…"},"payload":{…}}
```

| Event | Sent when |
|---|---|
| `MED_LOGGED` | A dose was recorded. The payload has `status` and `interval_override`. |
| `MED_VOIDED` | A dose entry was cancelled, with the reason. |
| `MED_EVENT` | A follow-up on a dose, e.g. `vomited` with `minutes_after`. |
| `MEDICATIONS_UPDATED` | The catalogue or a schedule changed. |
| `VITAL_LOGGED` | A reading was saved. |
| `INSULIN_LOGGED`, `INSULIN_PLANS_UPDATED` | An injection was recorded, or a plan changed. |
| `NOTE_ADDED` | A handover note was added. |
| `CAREGIVER_SWITCHED`, `CAREGIVERS_UPDATED` | Someone switched profile, or users changed. |
| `SHIFT_UPDATED`, `DUTY_UPDATED` | Shifts or assignments changed. |

A client that falls behind is disconnected so it reconnects and refetches; the frontend's `useLiveSync` hook does this automatically.

### Errors

Most errors are JSON `{"error": "CODE", "message": "…"}`. Common codes:

| Area | Codes |
|---|---|
| Sign-in | `UNAUTHENTICATED`, `FORBIDDEN`, `INCORRECT_PIN`, `TOO_MANY_ATTEMPTS`, `WEAK_PIN`, `SAME_PIN` |
| Doses | `DOSE_ALREADY_LOGGED`, `DOSE_TOO_SOON`, `REASON_REQUIRED`, `VOID_NOT_ALLOWED`, `ALREADY_VOIDED`, `USE_EVENT`, `NOT_GIVEN` |
| Insulin | `INSULIN_TOO_SOON`, `HYPO_NO_INSULIN` |
| Input | `INVALID_PAYLOAD`, `INVALID_DATE`, `INVALID_STATUS`, `INVALID_GIVEN_AT` |

Some older handlers return only `{"error": "…"}`.

## Clinical thresholds

Blood pressure badge (`frontend/src/lib/clinical.ts`, backend `CalculateBPStatus` in `backend/internal/handlers/vitals.go`):

| Reading (mmHg) | Status |
|---|---|
| ≥ 180 systolic or ≥ 120 diastolic | Crisis: call emergency (red) |
| ≥ 140 or ≥ 90 | Stage 2 hypertension (orange) |
| ≥ 120 or ≥ 80 | Elevated (yellow) |
| below that | Normal (green) |

Blood sugar (mmol/L): below 3.9 low, 3.9–7.8 normal, above 7.8 elevated, 13.9 and above very high.

Doses: a `taken` or `partial` dose blocks another of the same medication within its minimum interval, the doctor's value or ¾ of the shortest scheduled gap. Partial and vomited doses tell the family to call the doctor or pharmacist before giving any more; the app never suggests a re-dose or an amount.

These thresholds and the in-app crisis steps are the project's own defaults, not medical advice; follow the treating doctor's instructions.

## Project structure

```
backend/
  main.go                  startup, graceful shutdown
  internal/server/         router and middleware
  internal/handlers/       HTTP handlers (+ tests)
  internal/auth/           sessions, PIN limiter, Cloudflare Access, TOTP
  internal/db/             SQLite setup and migrations
  internal/sse/            event broker
frontend/
  src/app/                 pages: / (doses), /vitals, /insulin, /handovers, /export; /api/v1 proxy
  src/components/          UI components and modals
  src/hooks/useLiveSync.ts SSE client with reconnect/backoff
  e2e/                     Playwright: RTL/LTR, dose interval lock, undo across devices, partial/vomited
scripts/                   deploy.sh, auto-deploy.sh, systemd units
docs/DEPLOYMENT.md         production deployment guide
```

## Development

```bash
# Backend
cd backend
go vet ./...
go test -race ./...

# Frontend
cd frontend
npm run lint
npm test            # Vitest
npm run test:e2e    # Playwright on iPhone 13 / Pixel 7 profiles; API mocked, no backend needed
                    # first time: npx playwright install chromium webkit
```

CI (`.github/workflows/ci-cd.yml`) runs these checks plus a no-CGO static build and both Docker builds on every push and pull request to `main`, and publishes `birrcare-backend` / `birrcare-frontend` images to GHCR from `main`.

## Deployment

Step-by-step production guide: **[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)**. It covers requirements, `.env`, Cloudflare Tunnel and Access, first login and hardening, automatic updates, backups, restore, rollback and troubleshooting.

In short:

- **`scripts/deploy.sh`** snapshots `data/birrcare.db` into `data/backups/` (keeps 10), rebuilds and restarts both containers, and waits for the health checks.
- **`scripts/auto-deploy.sh`** with the systemd user units in `scripts/systemd/` checks `main` every 5 minutes and deploys a new commit only after its CI run passed. It is pull-based, so nothing on GitHub can run code on the host.
- Expose only `127.0.0.1:3000` through a Cloudflare Tunnel, protected by a Cloudflare Access policy.

## License

_To be confirmed_ (no LICENSE file in the repository).
