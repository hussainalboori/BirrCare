# Birrcare (رعاية بر)

A self-hosted, mobile-first web app that helps siblings coordinate 24/7 home care for a parent recovering from a stroke.

## Overview

Birrcare is the family's single source of truth for medication doses, stroke vitals, insulin, shift handovers and who is on duty. Every phone stays in sync live: when one sibling marks a dose, every other open phone updates without a refresh, and the same dose can never be recorded twice.

Main capabilities:

- **Medication schedule with a double-dose lock.** Doses are scheduled per slot (`night`, `morning`, `afternoon`, `evening`, `bedtime`), clock time and day of week. A database `UNIQUE (medication_id, scheduled_date, slot)` constraint is the lock; a second sibling gets `409` with who gave it and when.
- **Stroke vitals.** Blood pressure, pulse, blood sugar (mmol/L) and alertness (`alert`, `confused`, `restless`, `drowsy`), with a colour-coded BP badge and a crisis screen with one-tap call buttons.
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
- For development without Docker: Go 1.26 (`backend/go.mod`) and Node.js (the frontend image uses Node 20, CI uses Node 24).

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
| `DOCTOR_NAME`, `DOCTOR_PHONE` | empty | Doctor call button on a BP crisis. |
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

PIN login is rate limited per caregiver: after 5 wrong PINs the account is locked for 30 seconds, doubling up to 15 minutes, with `429` and `Retry-After`. An account with 2FA gets `{"status":"2fa_required","challenge":…}` from `verify-pin` and finishes with `POST /api/v1/auth/verify-2fa`. Changing or resetting a PIN, or turning 2FA on or off, signs that caregiver out on every other device.

| Role | Can do |
|---|---|
| `super_admin` | Everything, including users, shifts and 2FA. |
| `admin` | Manage medications, schedules and insulin plans; override an insulin interval lock (with a reason); log care. |
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

`verify-pin` returns `{"status":"success","caregiver":{…}}` and sets the cookie. The schedule is a JSON array of scheduled doses, each with the medication, its `slot`, scheduled `time`, and the `log` if it was already given or skipped.

## API reference

Base path `/api/v1`. Derived from `backend/internal/server/router.go` and the handlers; there is no OpenAPI spec. All endpoints except the public ones need an authenticated caregiver (else `401`). Role checks happen in the handlers (`403`).

**Public**

| Method | Path | Description |
|---|---|---|
| GET | `/health` (also `/health` at the root) | Health check. |
| GET | `/caregivers` | Active caregivers, for the profile switcher. |
| POST | `/auth/verify-pin` | `{caregiver_id, pin}` → session cookie, or a 2FA challenge. `401` wrong PIN, `404` unknown caregiver, `429` locked out. |
| POST | `/auth/verify-2fa` | Second login step: the challenge plus an authenticator or recovery code. |
| POST | `/auth/logout` | Clear the session cookie. |
| GET | `/contacts` | `{emergency_number, doctor_name, doctor_phone}` from the environment. |

**Account**

| Method | Path | Description |
|---|---|---|
| GET | `/auth/me` | The signed-in caregiver. |
| POST | `/auth/change-pin` | Change your own PIN (current PIN required). |
| POST | `/auth/2fa/setup` · `/auth/2fa/enable` · `/auth/2fa/disable` | Authenticator-app 2FA (Super Admin). Enabling returns 8 one-time recovery codes. |

**Users** (Super Admin)

| Method | Path | Description |
|---|---|---|
| GET | `/users` | All users, including inactive. |
| POST | `/users` | Create a user with a role and 4-digit PIN. |
| PUT | `/users/{id}` | Update profile and role. |
| POST | `/users/{id}/reset-pin` | Set a new PIN; signs that user out everywhere. |
| DELETE | `/users/{id}` | Toggle active / inactive. |

**Medications**

| Method | Path | Description |
|---|---|---|
| GET | `/medications` | Medication catalogue. |
| POST | `/medications` · PUT `/medications/{id}` · DELETE `/medications/{id}` | Manage medications (admin). |
| PUT | `/medications/{id}/schedule` | Days of week and clock time per slot (admin). |
| GET | `/medications/schedule?date=YYYY-MM-DD` | Doses for a day (default: today in `APP_TIMEZONE`) with their log. |
| POST | `/medications/log` | Record a dose, see below. |

`POST /medications/log` body: `medication_id`, `slot`, `scheduled_date` (`YYYY-MM-DD`, not in the future), `status` (`taken` or `skipped`), optional `notes`. Responses: `201` recorded; `409 DOSE_ALREADY_LOGGED` with who recorded it and when; `400` invalid input or slot not scheduled; `403` viewer; `404` medication not found or inactive.

**Vitals**

| Method | Path | Description |
|---|---|---|
| GET | `/vitals?limit=` | Latest readings (default 30, max 100). |
| POST | `/vitals` | Log any of: `systolic` + `diastolic` (both together), `pulse`, `blood_sugar` (1–45 mmol/L), `alertness_level`, `notes`, optional `vital_type` (`bp`, `sugar`, `alertness`, `full`). At least BP, sugar or alertness is required. |

**Insulin**

| Method | Path | Description |
|---|---|---|
| GET | `/insulin/plans?all=1` | Plans (`all=1` includes inactive). |
| POST | `/insulin/plans` · PUT `/insulin/plans/{id}` | Doctor's scale or fixed dose (admin). |
| GET | `/insulin/status` | Per plan: last injection and whether the next one is locked. |
| POST | `/insulin/log` | Record an injection (`given` or `held`) with the reading taken now. `409` inside the minimum interval. |

**Handovers, export, shifts**

| Method | Path | Description |
|---|---|---|
| GET | `/handovers?limit=` | Latest notes (default 20, max 100). |
| POST | `/handovers` | `{shift_type, summary, urgent_flag}`; `shift_type` is a shift id or label (default `morning`). |
| GET | `/export/summary?days=` | Doctor summary for the last N days (default 7, max 90). |
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

Event types: `MED_LOGGED`, `VITAL_LOGGED`, `NOTE_ADDED`, `CAREGIVER_SWITCHED`, `CAREGIVERS_UPDATED`, `MEDICATIONS_UPDATED`, `INSULIN_LOGGED`, `INSULIN_PLANS_UPDATED`, `SHIFT_UPDATED`, `DUTY_UPDATED`. A client that falls behind is disconnected so it reconnects and refetches; the frontend's `useLiveSync` hook does this automatically.

### Errors

Most errors are JSON `{"error": "CODE", "message": "…"}`, e.g. `UNAUTHENTICATED`, `FORBIDDEN`, `INCORRECT_PIN`, `TOO_MANY_ATTEMPTS`, `DOSE_ALREADY_LOGGED`. Some older handlers return only `{"error": "…"}`.

## Clinical thresholds

Blood pressure badge (`frontend/src/lib/clinical.ts`, backend `CalculateBPStatus` in `backend/internal/handlers/vitals.go`):

| Reading (mmHg) | Status |
|---|---|
| ≥ 180 systolic or ≥ 120 diastolic | Crisis: call emergency (red) |
| ≥ 140 or ≥ 90 | Stage 2 hypertension (orange) |
| ≥ 120 or ≥ 80 | Elevated (yellow) |
| below that | Normal (green) |

Blood sugar (mmol/L): below 3.9 low, 3.9–7.8 normal, above 7.8 elevated, 13.9 and above very high.

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
  e2e/                     Playwright RTL/LTR checks
scripts/                   deploy.sh, auto-deploy.sh, systemd units
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

- **`scripts/deploy.sh`**, run from the repo on the host: snapshots `data/birrcare.db` into `data/backups/` (keeps the last 10), runs `docker compose build --pull` and `up -d`, then waits for the backend health check and the frontend to respond.
- **`scripts/auto-deploy.sh`** with the systemd user units in `scripts/systemd/` (install to `~/.config/systemd/user/`; the service expects the checkout at `~/birrcare`): every 5 minutes it fetches `main` and, if the CI run for the new commit succeeded, checks it out and runs `deploy.sh`. It needs `git`, `curl`, `jq` and Docker Compose. Deployment is pull-based on purpose, so nothing on GitHub can run code on the host.
- Expose `127.0.0.1:3000` through a Cloudflare Tunnel and protect it with a Cloudflare Access policy. Set `CF_ACCESS_TEAM_DOMAIN` and `CF_ACCESS_AUD` if you want Access sign-in to map to caregivers by email.

## License

_To be confirmed_ (no LICENSE file in the repository).
