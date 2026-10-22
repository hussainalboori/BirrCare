# Birrcare Developer & Contributor Guide

Welcome to the Birrcare contributor community! This guide walks you through setting up a local development environment, running test suites, and understanding the core architectural patterns.

---

## Technical Stack & Architecture

Birrcare uses a clean, decoupled architecture optimized for reliability and zero-maintenance self-hosting:

- **Backend (`backend/`):** Pure Go (Go 1.26) utilizing the lightweight [`chi`](https://github.com/go-chi/chi) router.
- **Database:** Pure Go SQLite via `modernc.org/sqlite` (zero CGO dependencies). Initialized in Write-Ahead Logging (WAL) mode with a single pooled connection to prevent file locking.
- **Real-Time Sync:** In-memory Server-Sent Events (SSE) broker broadcasting state changes across all connected devices in under 100 milliseconds.
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
# Run all bash test suites
./scripts/tests/setup-env.test.sh
./scripts/tests/env-schema.test.sh
./scripts/tests/deploy.test.sh
./scripts/tests/backup.test.sh
./scripts/tests/auto-deploy.test.sh
```

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
Every database mutation must record the acting caregiver's ID and stamp an entry into `audit_logs`. Health measurements (such as blood pressure readings) are append-only; corrections supersede previous records rather than overwriting them in place.

---

## Continuous Integration & Deployment (CI/CD)

Every pull request and push to `main` triggers our GitHub Actions pipeline (`.github/workflows/ci-cd.yml`):
1. **Lint & Vet:** Checks formatting, TypeScript types, and Go vetting.
2. **Unit Tests:** Executes Vitest and Go race-detection suites.
3. **E2E Tests:** Runs Playwright across mobile viewport profiles (iPhone 13 & Pixel 7).
4. **Container Builds:** Builds and verifies no-CGO production Docker images.
5. **Image Publishing:** Releases verified container images to GitHub Container Registry (GHCR).
