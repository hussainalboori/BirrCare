# Birrcare Developer & Contributor Guide

Welcome to the Birrcare contributor community! This guide walks you through setting up a local development environment, running test suites, and understanding the core architectural patterns.

---

## Technical Stack & Architecture

Birrcare uses a clean, decoupled architecture optimized for reliability and zero-maintenance self-hosting:

- **Backend (`backend/`):** Pure Go (Go 1.26) utilizing the lightweight [`chi`](https://github.com/go-chi/chi) router.
- **Database:** Pure Go SQLite via `modernc.org/sqlite` (zero CGO dependencies). Initialized in Write-Ahead Logging (WAL) mode with a single pooled connection to prevent file locking.
- **Real-Time Sync:** In-memory Server-Sent Events (SSE) broker broadcasting each change to every connected device as soon as it is saved.
- **Frontend (`frontend/`):** Next.js 16 (App Router), React 19, TypeScript, and Tailwind CSS 4 with native Right-to-Left (RTL) and Left-to-Right (LTR) bidirectional layout support.
- **Proxy Architecture:** The browser communicates strictly with the Next.js frontend. The Next.js API route (`src/app/api/v1/[...path]/route.ts`) securely forwards `/api/v1/*` requests (including SSE) to the Go backend, keeping session cookies same-origin.

```
Browser (Mobile Phone / Tablet)
       │
       ▼ (HTTPS :3000)
Next.js Frontend (App Router)
       │
       ▼ (Private HTTP Proxy :8080)
Go Backend (Chi Router)
  ├── In-Memory SSE Broker ──► Live Event Stream
  └── SQLite (WAL Mode, Single Conn) ──► ./data/birrcare.db
```

---

## Local Development (Without Docker)

If you prefer running services directly on your host machine:

### Prerequisites
- **Go:** 1.26 or later
- **Node.js:** 24 or later
- **npm:** 10 or later

### 1. Start the Go Backend
```bash
cd backend

# Run with your local timezone (e.g., Asia/Riyadh, Asia/Bahrain)
APP_TIMEZONE=Asia/Riyadh go run .
```
The backend initializes the database in `backend/data/birrcare.db`, runs all database migrations automatically, and begins listening on `http://localhost:8080`.

> **Note on Auth in Dev Mode:** When `APP_ENV` is not set to `production`, the backend allows `http://localhost` cookies and enables `AUTH_DEV_FALLBACK`. To test real PIN authentication, set `AUTH_DEV_FALLBACK=false`.

### 2. Start the Next.js Frontend
In a separate terminal window:
```bash
cd frontend

# Install dependencies
npm install

# Point the frontend proxy to your local Go backend
INTERNAL_BACKEND_URL=http://localhost:8080 npm run dev
```
Open [http://localhost:3000](http://localhost:3000) in your browser. Changes in `frontend/src/` will hot-reload automatically.

---

## Running Automated Tests

We take testing seriously: medication locking and stroke thresholds must be 100% dependable.

### 1. Backend Tests (Go)
```bash
cd backend

# Run static analysis
go vet ./...

# Run unit and integration tests with data race detection
go test -race ./...
```

### 2. Frontend Unit Tests (Vitest)
```bash
cd frontend

# Run unit tests for clinical calculations, badges, and state hooks
npm test
```

### 3. End-to-End Tests (Playwright)
Playwright tests cover key mobile scenarios, including RTL mirroring, double-dose interval locks, 10-second undo recovery, and low-stock alerts:
```bash
cd frontend

# Install browser binaries (first run only)
npx playwright install chromium webkit

# Run full end-to-end test suite
npm run test:e2e
```
*Note: Playwright tests mock API responses internally, so you do not need the Go backend running to run end-to-end tests.*

### 4. Host Script & Environment Tests
Run tests for the deployment, backup, and environment scripts:
```bash
# Run every host-script suite (git, curl, docker, restic… are stubbed; nothing real is touched)
for t in scripts/tests/*.test.sh; do bash "$t" | tail -1; done
```

They also run on macOS (bash 3.2), so keep the scripts free of bash-4-only features (no associative arrays).

---

## Key Development Principles

### 1. Bilingual & RTL Layout Rules
Birrcare supports both Arabic and English seamlessly. Always use logical CSS utility classes:
- **Spacing:** Use `ms-*` (margin-start) and `me-*` (margin-end) instead of `ml-*` and `mr-*`. Use `ps-*` and `pe-*` instead of `pl-*` and `pr-*`.
- **Positioning:** Use `start-*` and `end-*` rather than `left-*` and `right-*`.
- **Text Alignment:** Use `text-start` and `text-end`.

### 2. Zero-CGO SQLite WAL Concurrency
Because SQLite is a file-based database, write safety is paramount:
- SQLite is opened in WAL mode with `_pragma=busy_timeout(5000)`.
- The connection pool is strictly capped at `db.SetMaxOpenConns(1)`. All writes and reads share this connection cleanly.
- Never use CGO sqlite drivers. We use pure-Go `modernc.org/sqlite`.

### 3. Immediate-Lock Transactions for Medications
To prevent race conditions where two siblings simultaneously record the same dose:
- Dose creation uses an immediate-lock transaction.
- The partial unique index `uq_med_dose_active` guarantees at most one active record per `(medication_id, scheduled_date, slot)`.
- If a collision occurs, the second request fails cleanly with `409 DOSE_ALREADY_LOGGED` and returns who recorded the dose and at what exact time.

### 4. Auditing & Clinical Safety
Every database mutation must record the acting caregiver's ID and stamp an entry into `audit_logs`. Health measurements (such as blood pressure readings) are append-only; corrections supersede previous records rather than overwriting them in place. The audit log itself is hash-chained and protected by append-only triggers (`GET /audit/verify` checks it).

---

## Rules for Contributors

These rules come from real bugs. Please follow them in every change.

**Backend (Go)**
- **One SQLite connection:** never call `h.DB` while a transaction is open (it waits for the only connection and deadlocks); use the `tx`. Close a `rows` cursor before running the next query.
- **Audit inside the transaction:** `audit.Record(tx, …)` and handle its error; no audit row means no write.
- **Times:** most tables store UTC as `2006-01-02 15:04:05`; some newer ones store RFC3339, so compare with `datetime(col)` in SQL. "Today" comes from `APP_TIMEZONE`.
- **New alert type:** add it to the base `CREATE TABLE alerts` CHECK list **and** to a new rebuild migration (`alerts_vN`, guarded by a check for the new kind, listing every kind), then render it in `frontend/src/components/AlertCenter.tsx`.
- **Migrations** must be idempotent: run them twice on a copy of a real database before committing.
- **Retries:** write endpoints accept a `client_request_id` and return the existing row on a retry (the frontend sends `newRequestId()`).

**Frontend (Next.js)**
- **Bilingual:** Arabic and English for every string. Logical CSS only (`ms-/me-/ps-/pe-/start-/end-`), enforced by `rtl-lint.test.ts`.
- **Readable:** no text smaller than 12 px (enforced by `typography-lint.test.ts`), form fields at 16 px (stops iOS zooming), tap targets at least 48 px.
- **Plain words:** every button shows text; never icon-only. The users are tired family members, not technicians.
- **Dark mode:** give every surface a `dark:` background *and* text colour, including focus states (`focus:bg-white` needs `dark:focus:bg-…`).
- **React Compiler purity:** no `Date.now()` or `Math.random()` during render, and don't read refs during render.
- **No browser dialogs** (`alert`, `confirm`, `prompt`) for important actions; confirm inside the page instead.
- **No household data in the build:** anything per-household (like the emergency number) is read at runtime, because one image serves every household.

**Settings**: adding an environment variable means:
1. a line in `scripts/env.schema` (its question in English and Arabic, and a validator);
2. an entry in `.env.example`;
3. passing it to the container in `docker-compose.yml`;
4. reading it by name with `os.Getenv("KEY")`.

`scripts/tests/env-schema.test.sh` fails the build otherwise (see [CONFIGURATION.md](CONFIGURATION.md)).

**Clinical safety**: never let the app suggest a dose amount or a re-dose. Keep the F.A.S.T. check one tap from the home screen. Crisis call buttons must never depend on the network. See [CLINICAL.md](CLINICAL.md).

**Every fix gets a test that fails without it.** For UI changes, check a phone (375 px) and a desktop width, in Arabic and English, light and dark.

---

## Continuous Integration & Deployment (CI/CD)

Every pull request and push to `main` triggers our GitHub Actions pipeline (`.github/workflows/ci-cd.yml`):
1. **Backend:** `go mod verify`, `go vet`, `go test -race`, `govulncheck`, and a static no-CGO build.
2. **Frontend:** ESLint, Vitest (including the RTL and typography lints), and a production build.
3. **Docker:** `docker compose config` plus both images built, and the host-script suites (updater, backups, settings, installer, deploy, setup).
4. **Publish** (pushes to `main` only): images go to GitHub Container Registry as `ghcr.io/<owner>/birrcare-backend|frontend:sha-<commit>` and `:latest`, stamped with the commit, so servers can download exactly the version they install.

Playwright end-to-end tests are **not** run in CI yet: run `npm run test:e2e` locally before pushing UI changes (the `mobile-chrome` project needs only Chromium).
