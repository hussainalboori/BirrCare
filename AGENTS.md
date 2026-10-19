
# AGENTS.md — Birrcare (رعاية بر): Family Care Coordination Platform

## 1. Project Overview & System Topology
**Birrcare (رعاية بر)** is an open-source, mobile-first, self-hosted web application built to help siblings coordinate 24/7 home care for a parent recovering from a stroke. Rooted in *Birr al-Walidayn* (بر الوالدين), the platform serves as a single source of truth for medication schedules (with double-dose locking), stroke vitals (blood pressure, pulse, cognitive alertness), shift handovers, and doctor-ready export sheets.

### Decoupled Stack Architecture
- **Backend:** Pure Go (Go 1.23+) using standard library `net/http` or lightweight router (e.g., `chi`), pure Go SQLite (`modernc.org/sqlite` — zero CGO), and an in-memory pub/sub broker broadcasting Server-Sent Events (SSE) to connected devices.
- **Frontend:** Next.js (App Router, React 19, TypeScript, Tailwind CSS) with native Right-to-Left (RTL) / Left-to-Right (LTR) bidirectional layout support, PWA service workers for offline caching, and responsive touch-first UI for iOS Safari and Android Chrome.
- **Database:** SQLite running in Write-Ahead Logging (WAL) mode, stored on a persistent host mount.
- **Deployment & Networking:** Both services run in a local Docker Compose setup bound strictly to `127.0.0.1`, proxied externally via an existing host-level Cloudflare Tunnel protected by Cloudflare Access.


```

[Sibling Phone (iOS/Android)]
│
▼  (HTTPS via Subdomain)
[Cloudflare Edge: Zero Trust Access] ──► Email Whitelist (OTP)
│
▼  (Encrypted Ingress via Host Tunnel)
┌─────────────────────────────────────────────────────────────┐
│ Host: 127.0.0.1                                             │
│                                                             │
│   ┌────────────────────────┐      ┌─────────────────────┐   │
│   │   Next.js Frontend     │      │     Go Backend      │   │
│   │   (:3000)              │─────►│     (:8080)         │   │
│   │   - App Router         │      │     - SQLite (WAL)  │   │
│   │   - RTL/LTR i18n       │◄─────│     - SSE Stream    │   │
│   │   - PWA Service Worker │ (SSE)│     - PIN/CF Auth   │   │
│   └────────────────────────┘      └──────────┬──────────┘   │
│                                              │              │
│                                   ┌──────────▼──────────┐   │
│                                   │ /data/birrcare.db   │   │
│                                   │ (WAL Mode, Single)  │   │
│                                   └─────────────────────┘   │
└─────────────────────────────────────────────────────────────┘

```

---

## 2. Agent Manager Topology & Roles

```yaml
topology_version: "3.0"
orchestration: "hierarchical"
router: "architect_agent"
agents:
  - id: "architect_agent"
    role: "System & Backend Engineer (Go)"
    responsibilities: 
      - "Go HTTP API with modernc.org/sqlite"
      - "WAL pragma initialization & migration runner"
      - "In-memory SSE event bus for multi-device sync"
      - "Cloudflare Access header & PIN verification middleware"
    allowed_tools: ["file_editor", "terminal", "codebase_search"]

  - id: "frontend_agent"
    role: "Full-Stack Frontend Engineer (Next.js & Tailwind)"
    responsibilities:
      - "Next.js App Router bilingual routing & context (AR/EN)"
      - "RTL/LTR Tailwind layout mirroring with logical CSS"
      - "Fast Caregiver PIN switcher modal with numeric keypad"
      - "EventSource SSE listener for live zero-refresh updates"
      - "Printable / exportable doctor summary view"
    allowed_tools: ["file_editor", "browser_preview"]

  - id: "clinical_logic_agent"
    role: "Clinical Safety & QA Auditor"
    responsibilities:
      - "Double-dose prevention constraints & atomic checks"
      - "Stroke blood pressure threshold styling & alerts"
      - "Cognitive state tracking taxonomy"
      - "Audit log schema & validation"
    allowed_tools: ["file_editor", "unit_tester"]

```

---

## 3. Go Backend Specifications (`backend/`)

### 3.1 SQLite WAL Configuration

The Go service must connect using `modernc.org/sqlite` without CGO. The database pool must be initialized with serialized write safety:

```go
dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)", dbPath)
db, err := sql.Open("sqlite", dsn)
db.SetMaxOpenConns(1) // Mandatory for SQLite in concurrent web environments

```

### 3.2 Real-Time Event Bus (Server-Sent Events)

An in-memory Go channel broker must stream database write events immediately to all open SSE connections (`GET /api/v1/events/stream`):

* Message format: `data: {"type": "MED_LOGGED", "payload": {...}}\n\n`
* Supported event types: `MED_LOGGED`, `VITAL_LOGGED`, `NOTE_ADDED`, `CAREGIVER_SWITCHED`.

### 3.3 Auth & Attribution Middleware

1. **Cloudflare Header Trust:** Read `Cf-Access-Authenticated-User-Email`. If present and valid, match with `caregivers.email`.
2. **PIN Fallback / Switcher:** Endpoint `POST /api/v1/auth/verify-pin` accepts `{ caregiver_id, pin }` and returns an `HttpOnly; Secure; SameSite=Strict` session cookie.
3. **Audit Injection:** Every `POST` request must extract `caregiver_id` from the context and stamp the database row and `audit_logs` table.

---

## 4. Next.js Frontend Specifications (`frontend/`)

### 4.1 Bilingual & RTL Architecture

* **State & Storage:** Active language (`ar` or `en`) stored in local cookie and context.
* **Root Layout:** Dynamically injects `dir="rtl"` / `lang="ar"` or `dir="ltr"` / `lang="en"` on `<html>`.
* **CSS Standard:** Use strictly logical Tailwind classes:
* Spacing: `ms-*` (margin-start), `me-*` (margin-end), `ps-*` (padding-start), `pe-*` (padding-end).
* Positioning: `start-0`, `end-0` instead of `left-0`, `right-0`.


* **Font Stack:**
* Arabic: `Tajawal` or `Cairo` via `next/font/google`.
* English: `Inter` or system sans-serif.



### 4.2 Sibling Coordination & Mobile UX

* **Sticky Caregiver Header:** Shows the active caregiver's avatar and name with a 1-tap "Switch Profile" button that opens an oversized 4-digit numeric keypad.
* **Live SSE Listener:** Custom `useLiveSync()` React hook maintaining an `EventSource` connection to the Go backend. When Sibling A marks a dose on Android, Sibling B's iPhone updates the UI instantly without a page refresh.
* **Double-Dosing UI Lock:** Once marked, a dose button turns green and displays:
* AR: `أُعطي بواسطة {name} في {time}`
* EN: `Given by {name} at {time}`



### 4.3 Stroke Clinical Indicators

Blood pressure logging must automatically calculate and display the stroke warning badge:

* `< 120/80 mmHg`: Green (Normal / طبيعي)
* `120–139 / 80–89 mmHg`: Yellow (Elevated / مرتفع قليلاً)
* `140–179 / 90–119 mmHg`: Orange (Hypertension Stage 2 / ارتفاع ملحوظ)
* `≥ 180 or ≥ 120 mmHg`: Red blinking alert (Crisis — Call Emergency / طوارئ)

---

## 5. Unified Database Schema (SQLite)

```sql
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA synchronous = NORMAL;

-- 1. Caregivers
CREATE TABLE IF NOT EXISTS caregivers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT UNIQUE,
    role TEXT NOT NULL DEFAULT 'caregiver' CHECK (role IN ('admin', 'caregiver')),
    pin_hash TEXT NOT NULL,
    avatar_color TEXT DEFAULT '#2563eb',
    preferred_lang TEXT DEFAULT 'ar' CHECK (preferred_lang IN ('ar', 'en')),
    phone TEXT,
    is_active INTEGER DEFAULT 1,
    created_at TEXT DEFAULT (datetime('now'))
);

-- 2. Prescriptions
CREATE TABLE IF NOT EXISTS medications (
    id TEXT PRIMARY KEY,
    name_ar TEXT NOT NULL,
    name_en TEXT NOT NULL,
    dosage TEXT NOT NULL,
    instructions_ar TEXT,
    instructions_en TEXT,
    frequency_slots TEXT NOT NULL, -- JSON string: '["morning", "evening"]'
    is_active INTEGER DEFAULT 1,
    created_by TEXT REFERENCES caregivers(id),
    created_at TEXT DEFAULT (datetime('now'))
);

-- 3. Medication Intake Logs (Double-Dose Prevention)
CREATE TABLE IF NOT EXISTS medication_logs (
    id TEXT PRIMARY KEY,
    medication_id TEXT NOT NULL REFERENCES medications(id) ON DELETE CASCADE,
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
    slot TEXT NOT NULL,
    scheduled_date TEXT NOT NULL,  -- 'YYYY-MM-DD'
    taken_at TEXT DEFAULT (datetime('now')),
    status TEXT NOT NULL CHECK (status IN ('taken', 'skipped')),
    notes TEXT,
    CONSTRAINT uq_med_dose UNIQUE (medication_id, scheduled_date, slot)
);

-- 4. Stroke Vitals & Mental Alertness Logs
CREATE TABLE IF NOT EXISTS vitals_logs (
    id TEXT PRIMARY KEY,
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
    recorded_at TEXT DEFAULT (datetime('now')),
    systolic INTEGER NOT NULL CHECK (systolic BETWEEN 50 AND 300),
    diastolic INTEGER NOT NULL CHECK (diastolic BETWEEN 30 AND 200),
    pulse INTEGER NOT NULL CHECK (pulse BETWEEN 30 AND 220),
    blood_sugar REAL,
    alertness_level TEXT NOT NULL CHECK (alertness_level IN ('alert', 'confused', 'restless', 'drowsy')),
    notes TEXT
);

-- 5. Shift Handover Notes
CREATE TABLE IF NOT EXISTS handover_notes (
    id TEXT PRIMARY KEY,
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
    created_at TEXT DEFAULT (datetime('now')),
    shift_type TEXT CHECK (shift_type IN ('morning', 'evening', 'night')),
    summary TEXT NOT NULL,
    urgent_flag INTEGER DEFAULT 0
);

-- 6. Audit Trail
CREATE TABLE IF NOT EXISTS audit_logs (
    id TEXT PRIMARY KEY,
    caregiver_id TEXT REFERENCES caregivers(id),
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT,
    details TEXT,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_med_logs_date ON medication_logs(scheduled_date);
CREATE INDEX IF NOT EXISTS idx_vitals_date ON vitals_logs(recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_handover_date ON handover_notes(created_at DESC);

```

---

## 6. Docker Compose Service Layout

```yaml
services:
  birrcare-backend:
    build:
      context: ./backend
      dockerfile: Dockerfile
    container_name: birrcare_backend
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      - PORT=8080
      - DB_PATH=/data/birrcare.db
      - APP_ENV=production
    volumes:
      - ./data:/data

  birrcare-frontend:
    build:
      context: ./frontend
      dockerfile: Dockerfile
    container_name: birrcare_frontend
    restart: unless-stopped
    ports:
      - "127.0.0.1:3000:3000"
    environment:
      - NEXT_PUBLIC_API_URL=[https://care.yourdomain.com/api](https://care.yourdomain.com/api)
      - NODE_ENV=production
    depends_on:
      - birrcare-backend

```

---

## 7. Execution Roadmap for Antigravity

1. **Step 1: Go Backend Engine**
* Implement `main.go`, pure Go SQLite connection pool with WAL mode, and migrations.
* Build `/api/v1/events/stream` SSE hub and CRUD endpoints for meds, vitals, and handovers.


2. **Step 2: Next.js Bilingual Shell**
* Initialize Next.js App Router with Tailwind RTL configuration and Tajawal font.
* Build the active caregiver switcher and live SSE listener hook.


3. **Step 3: Clinical Modules & Export**
* Connect the medication checklist and stroke BP logger.
* Build the printable doctor consultation view.
