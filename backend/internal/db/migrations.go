package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"birrcare/backend/internal/audit"
)

const SchemaSQL = `
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA synchronous = NORMAL;

-- 1. Caregivers
CREATE TABLE IF NOT EXISTS caregivers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT UNIQUE,
    role TEXT NOT NULL DEFAULT 'caregiver' CHECK (role IN ('super_admin', 'admin', 'caregiver', 'viewer')),
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
    days_of_week TEXT DEFAULT '["sun","mon","tue","wed","thu","fri","sat"]',
    slot_times TEXT DEFAULT '{"morning":"08:00","afternoon":"14:00","evening":"20:00","bedtime":"22:30","night":"02:00"}',
    image_url TEXT,
    photo_urls TEXT NOT NULL DEFAULT '[]', -- every uploaded photo (box, back, pill); image_url is the main one
    is_prn INTEGER NOT NULL DEFAULT 0,
    prn_min_hours REAL,
    prn_max_per_24h INTEGER,
    is_active INTEGER DEFAULT 1,
    created_by TEXT REFERENCES caregivers(id),
    created_at TEXT DEFAULT (datetime('now'))
);

-- 3. Medication Intake Logs (Double-Dose Prevention)
-- One ACTIVE row per dose: enforced by the partial unique index uq_med_dose_active
-- (created in Migrate). A mis-tap is voided, never deleted, and stays as history.
CREATE TABLE IF NOT EXISTS medication_logs (
    id TEXT PRIMARY KEY,
    medication_id TEXT NOT NULL REFERENCES medications(id) ON DELETE CASCADE,
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
    slot TEXT NOT NULL,
    scheduled_date TEXT NOT NULL,  -- 'YYYY-MM-DD'
    taken_at TEXT DEFAULT (datetime('now')),      -- when the dose was given
    recorded_at TEXT DEFAULT (datetime('now')),   -- when it was tapped (undo window)
    -- partial/refused (issue #9). "vomited" is recorded as a follow-up event on a
    -- taken/partial dose (medication_log_events); the API never logs it as a status.
    status TEXT NOT NULL CHECK (status IN ('taken', 'skipped', 'partial', 'vomited', 'refused')),
    notes TEXT,
    voided_at TEXT,
    voided_by TEXT REFERENCES caregivers(id),
    void_reason TEXT,
    dosage_snapshot TEXT  -- the medication's dosage when this dose was recorded (issue #14)
);

-- Follow-ups on a given dose, e.g. vomited 10 minutes later (issue #9).
CREATE TABLE IF NOT EXISTS medication_log_events (
    id TEXT PRIMARY KEY,
    log_id TEXT NOT NULL REFERENCES medication_logs(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('vomited')),
    minutes_after INTEGER CHECK (minutes_after IS NULL OR minutes_after BETWEEN 0 AND 1440),
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
    notes TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    UNIQUE (log_id, type)
);

-- 3b. PRN (As-Needed) Medication Intake Logs (Issue #25)
CREATE TABLE IF NOT EXISTS prn_logs (
    id TEXT PRIMARY KEY,
    medication_id TEXT NOT NULL REFERENCES medications(id) ON DELETE CASCADE,
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
    given_at TEXT DEFAULT (datetime('now')),
    reason_tag TEXT NOT NULL CHECK (reason_tag IN ('headache', 'pain', 'fever', 'other')),
    notes TEXT,
    voided_at TEXT,
    voided_by TEXT REFERENCES caregivers(id),
    void_reason TEXT,
    dosage_snapshot TEXT
);
CREATE INDEX IF NOT EXISTS idx_prn_logs_med_given ON prn_logs(medication_id, given_at DESC);
CREATE INDEX IF NOT EXISTS idx_prn_logs_given ON prn_logs(given_at DESC);

-- Alerts that must reach every sibling (issue #12): a BP crisis, a dose given
-- inside its minimum interval, vomiting after a dose. Written in the same
-- transaction as the event; replayed until each caregiver acknowledges it.
CREATE TABLE IF NOT EXISTS alerts (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('bp_crisis', 'bp_low', 'alertness', 'fast_positive', 'dose_override', 'vomited', 'correction', 'duty_handover', 'duty_unaccepted', 'emergency_card', 'low_stock', 'spo2_low', 'reposition_overdue', 'bowel_overdue', 'low_fluid', 'backup_failed', 'bleeding')),
    ref_id TEXT,
    created_by TEXT REFERENCES caregivers(id),
    payload TEXT NOT NULL DEFAULT '{}',
    created_at TEXT DEFAULT (datetime('now')),
    -- NULL: for every caregiver; otherwise only this one sees it (issue #18)
    target_caregiver_id TEXT REFERENCES caregivers(id)
);
CREATE INDEX IF NOT EXISTS idx_alerts_created ON alerts(created_at DESC);

CREATE TABLE IF NOT EXISTS alert_acks (
    alert_id TEXT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
    acked_at TEXT DEFAULT (datetime('now')),
    PRIMARY KEY (alert_id, caregiver_id)
);

-- Web Push subscriptions, one per installed browser/app.
CREATE TABLE IF NOT EXISTS push_subscriptions (
    id TEXT PRIMARY KEY,
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id) ON DELETE CASCADE,
    endpoint TEXT NOT NULL UNIQUE,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    created_at TEXT DEFAULT (datetime('now'))
);

-- 4. Stroke Vitals & Mental Alertness Logs
CREATE TABLE IF NOT EXISTS vitals_logs (
    id TEXT PRIMARY KEY,
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
    recorded_at TEXT DEFAULT (datetime('now')),
    systolic INTEGER CHECK (systolic IS NULL OR systolic BETWEEN 50 AND 300),
    diastolic INTEGER CHECK (diastolic IS NULL OR diastolic BETWEEN 30 AND 200),
    pulse INTEGER CHECK (pulse IS NULL OR pulse BETWEEN 30 AND 220),
    blood_sugar REAL,
    -- NULL = not assessed. Never default to 'alert': that records a cognitive state nobody checked.
    alertness_level TEXT CHECK (alertness_level IS NULL OR alertness_level IN ('alert', 'confused', 'restless', 'drowsy')),
    vital_type TEXT DEFAULT 'full',
    notes TEXT,
    -- 1 = a 'full' reading saved with 6.4, the value the old form pre-filled (issue #10).
    suspect_default_sugar INTEGER NOT NULL DEFAULT 0,
    -- F.A.S.T. stroke check (vital_type 'fast', issue #13): {"face":bool,"arm":bool,"speech":bool}
    fast_result TEXT,
    -- When she was last seen normal, for the paramedics.
    last_normal_at TEXT,
    -- Corrections (issue #15): a corrected reading is a new row; the original keeps
    -- its values and gets superseded_by. Values are never updated in place.
    supersedes_id TEXT REFERENCES vitals_logs(id),
    superseded_by TEXT,
    correction_reason TEXT,
    corrected_at TEXT
);

-- 5. Shift Handover Notes
CREATE TABLE IF NOT EXISTS handover_notes (
    id TEXT PRIMARY KEY,
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
    created_at TEXT DEFAULT (datetime('now')),
    shift_type TEXT NOT NULL DEFAULT 'morning',
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
    created_at TEXT DEFAULT (datetime('now')),
    prev_hash TEXT,
    row_hash TEXT
);

-- 7. Server-side settings (e.g. persisted session signing secret)
CREATE TABLE IF NOT EXISTS app_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    created_at TEXT DEFAULT (datetime('now'))
);

-- Signed-out session cookies, refused until they would have expired anyway.
CREATE TABLE IF NOT EXISTS revoked_sessions (
    token_hash TEXT PRIMARY KEY,
    expires_at INTEGER NOT NULL
);

-- Devices that entered a caregiver's PIN correctly; each gets its own PIN-attempt
-- budget, so failures from elsewhere cannot lock the family's phones out.
CREATE TABLE IF NOT EXISTS trusted_devices (
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id) ON DELETE CASCADE,
    device_hash TEXT NOT NULL,
    last_seen INTEGER NOT NULL,
    PRIMARY KEY (caregiver_id, device_hash)
);

-- 8. Caregiver Work Shifts (Configured by Super Admin)
CREATE TABLE IF NOT EXISTS shifts (
    id TEXT PRIMARY KEY,
    name_ar TEXT NOT NULL,
    name_en TEXT NOT NULL,
    start_time TEXT NOT NULL, -- '08:00'
    end_time TEXT NOT NULL,   -- '08:00' or '12:00'
    duration_hours REAL NOT NULL DEFAULT 24.0,
    color TEXT DEFAULT '#2563eb',
    display_order INTEGER DEFAULT 0,
    is_active INTEGER DEFAULT 1,
    created_at TEXT DEFAULT (datetime('now'))
);

-- 9. Caregiver Shift Assignments
CREATE TABLE IF NOT EXISTS shift_assignments (
    id TEXT PRIMARY KEY,
    shift_id TEXT NOT NULL REFERENCES shifts(id) ON DELETE CASCADE,
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id) ON DELETE CASCADE,
    date TEXT NOT NULL, -- 'YYYY-MM-DD'
    notes TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    CONSTRAINT uq_shift_date UNIQUE (shift_id, date)
);

-- 10. Insulin plans: the doctor's correction (sliding) scale or fixed dose, entered by an admin.
-- The app never computes a dose; it only looks up the doctor's table.
CREATE TABLE IF NOT EXISTS insulin_plans (
    id TEXT PRIMARY KEY,
    name_ar TEXT NOT NULL,
    name_en TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('scale', 'fixed')),
    fixed_units REAL,
    scale TEXT NOT NULL DEFAULT '[]', -- JSON [{"min":4,"max":8,"units":0},...] mmol/L, min inclusive, max exclusive
    min_hours_between REAL NOT NULL DEFAULT 4,
    frequency_slots TEXT NOT NULL DEFAULT '[]',
    slot_times TEXT NOT NULL DEFAULT '{}',
    prescribed_by TEXT,
    notes_ar TEXT,
    notes_en TEXT,
    is_active INTEGER DEFAULT 1,
    updated_by TEXT REFERENCES caregivers(id),
    updated_at TEXT DEFAULT (datetime('now'))
);

-- 11. Insulin injections (and held doses), with the reading and scale used at the time
CREATE TABLE IF NOT EXISTS insulin_logs (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES insulin_plans(id),
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
    given_at TEXT DEFAULT (datetime('now')),
    status TEXT NOT NULL CHECK (status IN ('given', 'held')),
    units REAL NOT NULL CHECK (units >= 0 AND units <= 100),
    scale_units REAL,       -- what the doctor's scale said for this reading (NULL = outside the scale)
    blood_sugar REAL,
    vital_id TEXT REFERENCES vitals_logs(id),
    reason TEXT,            -- required when units differ from the scale or the interval lock is overridden
    scale_snapshot TEXT,    -- the plan as it was, so later scale edits never rewrite history
    voided_at TEXT,
    voided_by TEXT REFERENCES caregivers(id),
    void_reason TEXT
);

CREATE TABLE IF NOT EXISTS care_events (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('reposition', 'fluid', 'bowel', 'hygiene', 'physio')),
    value TEXT NOT NULL,
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
    skin_condition TEXT CHECK (skin_condition IN ('ok', 'red_area', '')),
    notes TEXT,
    thickened INTEGER NOT NULL DEFAULT 0,
    created_at TEXT DEFAULT (datetime('now')),
    client_request_id TEXT UNIQUE
);

CREATE INDEX IF NOT EXISTS idx_care_events_kind_time ON care_events(kind, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_care_events_time ON care_events(created_at DESC);

-- Bruises and bleeding signs (she takes a strong blood thinner): where on the
-- body, size, how it changes, an optional photo, and bleeding signs to watch.
CREATE TABLE IF NOT EXISTS bruise_logs (
    id TEXT PRIMARY KEY,
    caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    body_part TEXT,
    size TEXT CHECK (size IN ('small', 'medium', 'large')),
    change TEXT CHECK (change IN ('new', 'same', 'bigger', 'smaller')),
    bleeding_signs TEXT NOT NULL DEFAULT '[]',
    photo_url TEXT,
    notes TEXT,
    client_request_id TEXT UNIQUE
);
CREATE INDEX IF NOT EXISTS idx_bruise_logs_time ON bruise_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_insulin_logs_plan ON insulin_logs(plan_id, given_at DESC);
CREATE INDEX IF NOT EXISTS idx_insulin_logs_date ON insulin_logs(given_at DESC);
CREATE INDEX IF NOT EXISTS idx_med_logs_date ON medication_logs(scheduled_date);
CREATE INDEX IF NOT EXISTS idx_vitals_date ON vitals_logs(recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_handover_date ON handover_notes(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_shift_assignments_date ON shift_assignments(date);

-- Paired bedside / home devices (issue #29): persistent paired-device token
-- allows instant 1-tap switcher on home devices without PIN, resilient to dynamic IP rotation.
CREATE TABLE IF NOT EXISTS paired_devices (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    paired_by TEXT REFERENCES caregivers(id),
    device_token_hash TEXT NOT NULL UNIQUE,
    created_at TEXT DEFAULT (datetime('now')),
    last_used_at TEXT DEFAULT (datetime('now')),
    is_revoked INTEGER DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_paired_devices_hash ON paired_devices(device_token_hash);
`

// Migrate runs schema migrations and seeds initial data if tables are empty.
func Migrate(database *sql.DB) error {
	if os.Getenv("APP_ENV") == "production" {
		tz := strings.TrimSpace(os.Getenv("APP_TIMEZONE"))
		if tz == "" {
			return fmt.Errorf("APP_TIMEZONE is required when APP_ENV=production (e.g. APP_TIMEZONE=Asia/Riyadh)")
		}
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Errorf("invalid APP_TIMEZONE %q: %w", tz, err)
		}
	}
	// Whether medication_logs is created by this run (fresh database): only then, or
	// right after rebuilding it, may Migrate create the dose-lock index. If it later
	// goes missing or is replaced, boot is refused rather than silently "repaired".
	var medLogsExisted int
	_ = database.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='medication_logs'").Scan(&medLogsExisted)

	if _, err := database.Exec(SchemaSQL); err != nil {
		return fmt.Errorf("failed executing schema: %w", err)
	}

	// 1. Safe migration for existing SQLite databases where caregivers table had CHECK (role IN ('admin', 'caregiver'))
	var sqlDef string
	err := database.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='caregivers'").Scan(&sqlDef)
	if err == nil && (!strings.Contains(sqlDef, "super_admin") || !strings.Contains(sqlDef, "viewer")) {
		log.Println("Migrating caregivers table schema to support super_admin and viewer roles...")
		migrationSQL := `
			PRAGMA foreign_keys = OFF;
			CREATE TABLE IF NOT EXISTS caregivers_v2 (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				email TEXT UNIQUE,
				role TEXT NOT NULL DEFAULT 'caregiver' CHECK (role IN ('super_admin', 'admin', 'caregiver', 'viewer')),
				pin_hash TEXT NOT NULL,
				avatar_color TEXT DEFAULT '#2563eb',
				preferred_lang TEXT DEFAULT 'ar' CHECK (preferred_lang IN ('ar', 'en')),
				phone TEXT,
				is_active INTEGER DEFAULT 1,
				created_at TEXT DEFAULT (datetime('now'))
			);
			INSERT OR IGNORE INTO caregivers_v2 (id, name, email, role, pin_hash, avatar_color, preferred_lang, phone, is_active, created_at)
			SELECT id, name, email, role, pin_hash, avatar_color, preferred_lang, phone, is_active, created_at FROM caregivers;
			DROP TABLE caregivers;
			ALTER TABLE caregivers_v2 RENAME TO caregivers;
			PRAGMA foreign_keys = ON;
		`
		if _, err := database.Exec(migrationSQL); err != nil {
			return fmt.Errorf("failed migrating caregivers table: %w", err)
		}
	}

	// 2. Ensure at least one super_admin exists (promote existing admin or cg_hussain)
	var superAdminCount int
	_ = database.QueryRow("SELECT COUNT(*) FROM caregivers WHERE role = 'super_admin'").Scan(&superAdminCount)
	if superAdminCount == 0 {
		// Promote exactly ONE account (cg_hussain if present, else the oldest admin).
		// Promoting every admin would silently escalate privileges on each restart.
		_, _ = database.Exec(`
			UPDATE caregivers SET role = 'super_admin'
			WHERE id = (
				SELECT id FROM caregivers
				WHERE id = 'cg_hussain' OR role = 'admin'
				ORDER BY CASE WHEN id = 'cg_hussain' THEN 0 ELSE 1 END, created_at ASC
				LIMIT 1
			)`)
	}

	// 3. Migrate medications table: add image_url, days_of_week, and slot_times columns if missing
	var colCount int
	_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('medications') WHERE name='image_url'").Scan(&colCount)
	if colCount == 0 {
		log.Println("Migrating medications table: adding image_url column...")
		_, _ = database.Exec("ALTER TABLE medications ADD COLUMN image_url TEXT")
	}

	var daysColCount int
	_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('medications') WHERE name='days_of_week'").Scan(&daysColCount)
	if daysColCount == 0 {
		log.Println("Migrating medications table: adding days_of_week column...")
		_, _ = database.Exec(`ALTER TABLE medications ADD COLUMN days_of_week TEXT DEFAULT '["sun","mon","tue","wed","thu","fri","sat"]'`)
		_, _ = database.Exec(`UPDATE medications SET days_of_week = '["sun","mon","tue","wed","thu","fri","sat"]' WHERE days_of_week IS NULL OR days_of_week = ''`)
	}

	var timesColCount int
	_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('medications') WHERE name='slot_times'").Scan(&timesColCount)
	if timesColCount == 0 {
		log.Println("Migrating medications table: adding slot_times column...")
		_, _ = database.Exec(`ALTER TABLE medications ADD COLUMN slot_times TEXT DEFAULT '{"morning":"08:00","afternoon":"14:00","evening":"20:00","bedtime":"22:30","night":"02:00"}'`)
		_, _ = database.Exec(`UPDATE medications SET slot_times = '{"morning":"08:00","afternoon":"14:00","evening":"20:00","bedtime":"22:30","night":"02:00"}' WHERE slot_times IS NULL OR slot_times = ''`)
	}

	// Voidable dose logs (issue #8): rebuild medication_logs without the table-level
	// UNIQUE, add void/recorded_at columns, and enforce one ACTIVE row per dose with a
	// partial unique index. Existing rows are kept as-is (recorded_at = taken_at).
	var medLogsSQL string
	_ = database.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='medication_logs'").Scan(&medLogsSQL)
	createDoseIndex := medLogsExisted == 0
	if !strings.Contains(medLogsSQL, "voided_at") || !strings.Contains(medLogsSQL, "'partial'") {
		log.Println("Migrating medication_logs: voidable doses, partial/refused statuses, partial unique index...")
		// Copy whatever columns this database already has (pre-#8 tables lack the void columns).
		has := func(col string) bool {
			var n int
			_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('medication_logs') WHERE name = ?", col).Scan(&n)
			return n > 0
		}
		pick := func(col, fallback string) string {
			if has(col) {
				return col
			}
			return fallback
		}
		migrationSQL := `
			PRAGMA foreign_keys = OFF;
			DROP TABLE IF EXISTS medication_logs_v2;
			CREATE TABLE medication_logs_v2 (
				id TEXT PRIMARY KEY,
				medication_id TEXT NOT NULL REFERENCES medications(id) ON DELETE CASCADE,
				caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
				slot TEXT NOT NULL,
				scheduled_date TEXT NOT NULL,
				taken_at TEXT DEFAULT (datetime('now')),
				recorded_at TEXT DEFAULT (datetime('now')),
				status TEXT NOT NULL CHECK (status IN ('taken', 'skipped', 'partial', 'vomited', 'refused')),
				notes TEXT,
				voided_at TEXT,
				voided_by TEXT REFERENCES caregivers(id),
				void_reason TEXT
			);
			INSERT INTO medication_logs_v2 (id, medication_id, caregiver_id, slot, scheduled_date, taken_at, recorded_at, status, notes, voided_at, voided_by, void_reason)
			SELECT id, medication_id, caregiver_id, slot, scheduled_date, taken_at, ` + pick("recorded_at", "taken_at") + `, status, notes, ` +
			pick("voided_at", "NULL") + `, ` + pick("voided_by", "NULL") + `, ` + pick("void_reason", "NULL") + ` FROM medication_logs;
			DROP TABLE medication_logs;
			ALTER TABLE medication_logs_v2 RENAME TO medication_logs;
			CREATE INDEX IF NOT EXISTS idx_med_logs_date ON medication_logs(scheduled_date);
			PRAGMA foreign_keys = ON;
		`
		if _, err := database.Exec(migrationSQL); err != nil {
			return fmt.Errorf("failed migrating medication_logs table: %w", err)
		}
		createDoseIndex = true
	}
	if createDoseIndex {
		// Fails (and so does boot) if legacy data already holds duplicate active doses.
		if _, err := database.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_med_dose_active
			ON medication_logs(medication_id, scheduled_date, slot) WHERE voided_at IS NULL`); err != nil {
			return fmt.Errorf("SAFETY: cannot create the double-dose lock uq_med_dose_active: %w", err)
		}
	}

	// Issue #14: dose recorded with its dosage at the time; critical medications
	// (anticoagulants, antiplatelets) listed first in the doctor report.
	for _, c := range []struct{ table, col, def string }{
		{"medication_logs", "dosage_snapshot", "TEXT"},
		{"medications", "is_critical", "INTEGER NOT NULL DEFAULT 0"},
	} {
		var n int
		_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", c.table, c.col).Scan(&n)
		if n == 0 {
			if _, err := database.Exec("ALTER TABLE " + c.table + " ADD COLUMN " + c.col + " " + c.def); err != nil {
				return fmt.Errorf("failed adding %s.%s: %w", c.table, c.col, err)
			}
		}
	}

	// Minimum interval between doses (issue #7): NULL = derived from the schedule.
	var minHoursCol int
	_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('medications') WHERE name='min_hours_between'").Scan(&minHoursCol)
	if minHoursCol == 0 {
		if _, err := database.Exec(`ALTER TABLE medications ADD COLUMN min_hours_between REAL`); err != nil {
			return fmt.Errorf("failed adding medications.min_hours_between: %w", err)
		}
	}
	if _, err := database.Exec(`CREATE INDEX IF NOT EXISTS idx_med_logs_med_taken ON medication_logs(medication_id, taken_at DESC)`); err != nil {
		return fmt.Errorf("failed creating idx_med_logs_med_taken: %w", err)
	}

	// Two-factor authentication (authenticator app) and session revocation.
	for _, col := range []struct{ name, def string }{
		{"totp_secret", "TEXT"},                                // base32, set once 2FA is enabled
		{"totp_pending_secret", "TEXT"},                        // during setup, until the first code is confirmed
		{"totp_enabled", "INTEGER NOT NULL DEFAULT 0"},         // 1 = PIN alone never signs in
		{"totp_last_step", "INTEGER NOT NULL DEFAULT 0"},       // last accepted time step (replay guard)
		{"recovery_codes", "TEXT"},                             // JSON array of SHA-256 hashes, removed when used
		{"sessions_valid_after", "INTEGER NOT NULL DEFAULT 0"}, // unix time; older session cookies are refused
	} {
		var n int
		_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('caregivers') WHERE name = ?", col.name).Scan(&n)
		if n == 0 {
			if _, err := database.Exec("ALTER TABLE caregivers ADD COLUMN " + col.name + " " + col.def); err != nil {
				return fmt.Errorf("failed adding caregivers.%s: %w", col.name, err)
			}
		}
	}

	var operatorResetID string
	// Break-glass: an operator with server access can clear 2FA for a caregiver who
	// lost both their phone and recovery codes (DISABLE_2FA_FOR=<caregiver id>).
	if id := strings.TrimSpace(os.Getenv("DISABLE_2FA_FOR")); id != "" {
		res, err := database.Exec(`UPDATE caregivers SET totp_enabled = 0, totp_secret = NULL, totp_pending_secret = NULL,
			recovery_codes = NULL, totp_last_step = 0, sessions_valid_after = CAST(strftime('%s','now') AS INTEGER) WHERE id = ?`, id)
		if err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				log.Printf("[SECURITY] Two-factor authentication DISABLED for %s via DISABLE_2FA_FOR. Remove the variable and restart.", id)
				// Audited below, through audit.Record, once the hash-chain columns exist.
				operatorResetID = id
			}
		}
	}

	// 4. Migrate vitals_logs table to allow selective metrics (systolic/diastolic/pulse can be NULL when logging only sugar or alertness)
	var notNullSystolic int
	_ = database.QueryRow("SELECT notnull FROM pragma_table_info('vitals_logs') WHERE name='systolic'").Scan(&notNullSystolic)
	var hasVitalType int
	_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('vitals_logs') WHERE name='vital_type'").Scan(&hasVitalType)
	if notNullSystolic == 1 || hasVitalType == 0 {
		log.Println("Migrating vitals_logs schema to allow selective vital types (BP, sugar, alertness, full)...")
		migrationSQL := `
			PRAGMA foreign_keys = OFF;
			CREATE TABLE IF NOT EXISTS vitals_logs_v2 (
				id TEXT PRIMARY KEY,
				caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
				recorded_at TEXT DEFAULT (datetime('now')),
				systolic INTEGER CHECK (systolic IS NULL OR systolic BETWEEN 50 AND 300),
				diastolic INTEGER CHECK (diastolic IS NULL OR diastolic BETWEEN 30 AND 200),
				pulse INTEGER CHECK (pulse IS NULL OR pulse BETWEEN 30 AND 220),
				blood_sugar REAL,
				alertness_level TEXT DEFAULT 'alert' CHECK (alertness_level IN ('alert', 'confused', 'restless', 'drowsy')),
				vital_type TEXT DEFAULT 'full',
				notes TEXT
			);
			INSERT OR IGNORE INTO vitals_logs_v2 (id, caregiver_id, recorded_at, systolic, diastolic, pulse, blood_sugar, alertness_level, vital_type, notes)
			SELECT id, caregiver_id, recorded_at, systolic, diastolic, pulse, blood_sugar, alertness_level, 'full', notes FROM vitals_logs;
			DROP TABLE vitals_logs;
			ALTER TABLE vitals_logs_v2 RENAME TO vitals_logs;
			CREATE INDEX IF NOT EXISTS idx_vitals_date ON vitals_logs(recorded_at DESC);
			PRAGMA foreign_keys = ON;
		`
		if _, err := database.Exec(migrationSQL); err != nil {
			return fmt.Errorf("failed migrating vitals_logs table: %w", err)
		}
	}

	// 4b. Issue #10: alertness is NULL when not assessed (drop DEFAULT 'alert'), and
	// flag - never rewrite - full readings whose 6.4 sugar may be the old form's
	// pre-filled value. The flag is set once, during this rebuild only, so a real
	// 6.4 measured later is never marked.
	var vitalsSQL string
	_ = database.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='vitals_logs'").Scan(&vitalsSQL)
	if !strings.Contains(vitalsSQL, "suspect_default_sugar") {
		log.Println("Migrating vitals_logs: alertness NULL when not assessed, flag pre-filled 6.4 sugar readings...")
		migrationSQL := `
			PRAGMA foreign_keys = OFF;
			DROP TABLE IF EXISTS vitals_logs_v3;
			CREATE TABLE vitals_logs_v3 (
				id TEXT PRIMARY KEY,
				caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
				recorded_at TEXT DEFAULT (datetime('now')),
				systolic INTEGER CHECK (systolic IS NULL OR systolic BETWEEN 50 AND 300),
				diastolic INTEGER CHECK (diastolic IS NULL OR diastolic BETWEEN 30 AND 200),
				pulse INTEGER CHECK (pulse IS NULL OR pulse BETWEEN 30 AND 220),
				blood_sugar REAL,
				alertness_level TEXT CHECK (alertness_level IS NULL OR alertness_level IN ('alert', 'confused', 'restless', 'drowsy')),
				vital_type TEXT DEFAULT 'full',
				notes TEXT,
				suspect_default_sugar INTEGER NOT NULL DEFAULT 0
			);
			INSERT INTO vitals_logs_v3 (id, caregiver_id, recorded_at, systolic, diastolic, pulse, blood_sugar, alertness_level, vital_type, notes, suspect_default_sugar)
			SELECT id, caregiver_id, recorded_at, systolic, diastolic, pulse, blood_sugar, alertness_level, vital_type, notes,
			       CASE WHEN blood_sugar = 6.4 AND vital_type = 'full' THEN 1 ELSE 0 END
			FROM vitals_logs;
			DROP TABLE vitals_logs;
			ALTER TABLE vitals_logs_v3 RENAME TO vitals_logs;
			CREATE INDEX IF NOT EXISTS idx_vitals_date ON vitals_logs(recorded_at DESC);
			PRAGMA foreign_keys = ON;
		`
		if _, err := database.Exec(migrationSQL); err != nil {
			return fmt.Errorf("failed migrating vitals_logs (alertness/suspect sugar): %w", err)
		}
	}

	// Issue #13: F.A.S.T. results and "last seen normal"; issue #15: corrections.
	for _, col := range []string{"fast_result", "last_normal_at", "supersedes_id", "superseded_by", "correction_reason", "corrected_at"} {
		var n int
		_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('vitals_logs') WHERE name = ?", col).Scan(&n)
		if n == 0 {
			if _, err := database.Exec("ALTER TABLE vitals_logs ADD COLUMN " + col + " TEXT"); err != nil {
				return fmt.Errorf("failed adding vitals_logs.%s: %w", col, err)
			}
		}
	}
	var alertsSQL string
	_ = database.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='alerts'").Scan(&alertsSQL)
	if !strings.Contains(alertsSQL, "'correction'") {
		log.Println("Migrating alerts: low BP, alertness, F.A.S.T. and correction kinds...")
		if _, err := database.Exec(`
			PRAGMA foreign_keys = OFF;
			DROP TABLE IF EXISTS alerts_v2;
			CREATE TABLE alerts_v2 (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL CHECK (kind IN ('bp_crisis', 'bp_low', 'alertness', 'fast_positive', 'dose_override', 'vomited', 'correction')),
				ref_id TEXT,
				created_by TEXT REFERENCES caregivers(id),
				payload TEXT NOT NULL DEFAULT '{}',
				created_at TEXT DEFAULT (datetime('now'))
			);
			INSERT INTO alerts_v2 SELECT id, kind, ref_id, created_by, payload, created_at FROM alerts;
			DROP TABLE alerts;
			ALTER TABLE alerts_v2 RENAME TO alerts;
			CREATE INDEX IF NOT EXISTS idx_alerts_created ON alerts(created_at DESC);
			PRAGMA foreign_keys = ON;
		`); err != nil {
			return fmt.Errorf("failed migrating alerts: %w", err)
		}
	}

	// Issue #18: duty handover alerts, addressed to one caregiver.
	_ = database.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='alerts'").Scan(&alertsSQL)
	if !strings.Contains(alertsSQL, "'duty_handover'") {
		log.Println("Migrating alerts: duty handover kinds and per-caregiver alerts...")
		if _, err := database.Exec(`
			PRAGMA foreign_keys = OFF;
			DROP TABLE IF EXISTS alerts_v3;
			CREATE TABLE alerts_v3 (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL CHECK (kind IN ('bp_crisis', 'bp_low', 'alertness', 'fast_positive', 'dose_override', 'vomited', 'correction', 'duty_handover', 'duty_unaccepted')),
				ref_id TEXT,
				created_by TEXT REFERENCES caregivers(id),
				payload TEXT NOT NULL DEFAULT '{}',
				created_at TEXT DEFAULT (datetime('now')),
				target_caregiver_id TEXT REFERENCES caregivers(id)
			);
			INSERT INTO alerts_v3 (id, kind, ref_id, created_by, payload, created_at) SELECT id, kind, ref_id, created_by, payload, created_at FROM alerts;
			DROP TABLE alerts;
			ALTER TABLE alerts_v3 RENAME TO alerts;
			CREATE INDEX IF NOT EXISTS idx_alerts_created ON alerts(created_at DESC);
			PRAGMA foreign_keys = ON;
		`); err != nil {
			return fmt.Errorf("failed migrating alerts for duty handover: %w", err)
		}
	}

	// Issue #19: "emergency card opened" alerts.
	_ = database.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='alerts'").Scan(&alertsSQL)
	if !strings.Contains(alertsSQL, "'emergency_card'") {
		log.Println("Migrating alerts: emergency card kind...")
		if _, err := database.Exec(`
			PRAGMA foreign_keys = OFF;
			DROP TABLE IF EXISTS alerts_v4;
			CREATE TABLE alerts_v4 (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL CHECK (kind IN ('bp_crisis', 'bp_low', 'alertness', 'fast_positive', 'dose_override', 'vomited', 'correction', 'duty_handover', 'duty_unaccepted', 'emergency_card')),
				ref_id TEXT,
				created_by TEXT REFERENCES caregivers(id),
				payload TEXT NOT NULL DEFAULT '{}',
				created_at TEXT DEFAULT (datetime('now')),
				target_caregiver_id TEXT REFERENCES caregivers(id)
			);
			INSERT INTO alerts_v4 (id, kind, ref_id, created_by, payload, created_at, target_caregiver_id)
				SELECT id, kind, ref_id, created_by, payload, created_at, target_caregiver_id FROM alerts;
			DROP TABLE alerts;
			ALTER TABLE alerts_v4 RENAME TO alerts;
			CREATE INDEX IF NOT EXISTS idx_alerts_created ON alerts(created_at DESC);
			PRAGMA foreign_keys = ON;
		`); err != nil {
			return fmt.Errorf("failed migrating alerts for the emergency card: %w", err)
		}
	}

	// Issue #20: low-stock alerts.
	_ = database.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='alerts'").Scan(&alertsSQL)
	if !strings.Contains(alertsSQL, "'low_stock'") {
		log.Println("Migrating alerts: low stock kind...")
		if _, err := database.Exec(`
			PRAGMA foreign_keys = OFF;
			DROP TABLE IF EXISTS alerts_v5;
			CREATE TABLE alerts_v5 (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL CHECK (kind IN ('bp_crisis', 'bp_low', 'alertness', 'fast_positive', 'dose_override', 'vomited', 'correction', 'duty_handover', 'duty_unaccepted', 'emergency_card', 'low_stock')),
				ref_id TEXT,
				created_by TEXT REFERENCES caregivers(id),
				payload TEXT NOT NULL DEFAULT '{}',
				created_at TEXT DEFAULT (datetime('now')),
				target_caregiver_id TEXT REFERENCES caregivers(id)
			);
			INSERT INTO alerts_v5 (id, kind, ref_id, created_by, payload, created_at, target_caregiver_id)
				SELECT id, kind, ref_id, created_by, payload, created_at, target_caregiver_id FROM alerts;
			DROP TABLE alerts;
			ALTER TABLE alerts_v5 RENAME TO alerts;
			CREATE INDEX IF NOT EXISTS idx_alerts_created ON alerts(created_at DESC);
			PRAGMA foreign_keys = ON;
		`); err != nil {
			return fmt.Errorf("failed migrating alerts for low stock: %w", err)
		}
	}

	// Issue #20: medicine stock. stock_units NULL = not tracked. The alert fires
	// once when the days left drop below the threshold (low_stock_alerted_at),
	// and re-arms when a refill brings it back above.
	for _, c := range []struct{ col, def string }{
		{"stock_units", "REAL"},
		{"units_per_dose", "REAL NOT NULL DEFAULT 1"},
		{"refill_threshold_days", "INTEGER NOT NULL DEFAULT 4"},
		{"low_stock_alerted_at", "TEXT"},
	} {
		var n int
		_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('medications') WHERE name = ?", c.col).Scan(&n)
		if n == 0 {
			if _, err := database.Exec("ALTER TABLE medications ADD COLUMN " + c.col + " " + c.def); err != nil {
				return fmt.Errorf("failed adding medications.%s: %w", c.col, err)
			}
		}
	}
	if _, err := database.Exec(`
		CREATE TABLE IF NOT EXISTS stock_events (
			id TEXT PRIMARY KEY,
			medication_id TEXT NOT NULL REFERENCES medications(id) ON DELETE CASCADE,
			delta REAL NOT NULL,
			stock_after REAL NOT NULL,
			reason TEXT NOT NULL CHECK (reason IN ('refill', 'dose', 'partial', 'correction', 'count')),
			log_id TEXT,
			caregiver_id TEXT REFERENCES caregivers(id),
			created_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_stock_events_med ON stock_events(medication_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_stock_events_log ON stock_events(log_id) WHERE log_id IS NOT NULL;
	`); err != nil {
		return fmt.Errorf("failed creating stock_events: %w", err)
	}

	// Issue #19: the one patient's profile for the paramedics' emergency card.
	// emergency_token is kept so signed-in family can show the QR again; the
	// public lookup compares SHA-256 hashes in constant time.
	if _, err := database.Exec(`
		CREATE TABLE IF NOT EXISTS patient_profile (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			name TEXT NOT NULL DEFAULT '',
			dob TEXT NOT NULL DEFAULT '',
			diagnosis_ar TEXT NOT NULL DEFAULT '',
			diagnosis_en TEXT NOT NULL DEFAULT '',
			stroke_date TEXT NOT NULL DEFAULT '',
			allergies_ar TEXT NOT NULL DEFAULT '',
			allergies_en TEXT NOT NULL DEFAULT '',
			blood_type TEXT NOT NULL DEFAULT '',
			baseline_bp TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			emergency_token TEXT,
			emergency_token_hash TEXT,
			token_rotated_at TEXT,
			updated_at TEXT,
			updated_by TEXT REFERENCES caregivers(id)
		);
		INSERT OR IGNORE INTO patient_profile (id) VALUES (1);
	`); err != nil {
		return fmt.Errorf("failed creating patient_profile: %w", err)
	}

	// Issue #18: quick-tap tags on handover notes, and "pass duty" records.
	var tagsCol int
	_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('handover_notes') WHERE name = 'tags'").Scan(&tagsCol)
	if tagsCol == 0 {
		if _, err := database.Exec("ALTER TABLE handover_notes ADD COLUMN tags TEXT"); err != nil {
			return fmt.Errorf("failed adding handover_notes.tags: %w", err)
		}
	}
	if _, err := database.Exec(`
		CREATE TABLE IF NOT EXISTS duty_handovers (
			id TEXT PRIMARY KEY,
			note_id TEXT NOT NULL REFERENCES handover_notes(id),
			assignment_id TEXT NOT NULL,
			shift_id TEXT NOT NULL,
			date TEXT NOT NULL,
			from_caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
			to_caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
			created_at TEXT NOT NULL,
			accepted_at TEXT,
			reminded_at TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_duty_handovers_pending ON duty_handovers(created_at) WHERE accepted_at IS NULL AND reminded_at IS NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS uq_duty_handovers_note ON duty_handovers(note_id);
	`); err != nil {
		return fmt.Errorf("failed creating duty_handovers: %w", err)
	}

	// Issue #16: idempotent retries from the offline outbox. A repeated
	// client_request_id returns the first row instead of saving a duplicate.
	for _, table := range []string{"vitals_logs", "handover_notes"} {
		var n int
		_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'client_request_id'", table).Scan(&n)
		if n == 0 {
			if _, err := database.Exec("ALTER TABLE " + table + " ADD COLUMN client_request_id TEXT"); err != nil {
				return fmt.Errorf("failed adding %s.client_request_id: %w", table, err)
			}
		}
		if _, err := database.Exec("CREATE UNIQUE INDEX IF NOT EXISTS uq_" + table + "_client_request ON " + table + "(client_request_id) WHERE client_request_id IS NOT NULL"); err != nil {
			return fmt.Errorf("failed creating %s client_request_id index: %w", table, err)
		}
	}

	// Issue #22: oxygen saturation and temperature, and their alert.
	_ = database.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='alerts'").Scan(&alertsSQL)
	if !strings.Contains(alertsSQL, "'spo2_low'") {
		log.Println("Migrating alerts: low oxygen kind...")
		if _, err := database.Exec(`
			PRAGMA foreign_keys = OFF;
			DROP TABLE IF EXISTS alerts_v6;
			CREATE TABLE alerts_v6 (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL CHECK (kind IN ('bp_crisis', 'bp_low', 'alertness', 'fast_positive', 'dose_override', 'vomited', 'correction', 'duty_handover', 'duty_unaccepted', 'emergency_card', 'low_stock', 'spo2_low')),
				ref_id TEXT,
				created_by TEXT REFERENCES caregivers(id),
				payload TEXT NOT NULL DEFAULT '{}',
				created_at TEXT DEFAULT (datetime('now')),
				target_caregiver_id TEXT REFERENCES caregivers(id)
			);
			INSERT INTO alerts_v6 (id, kind, ref_id, created_by, payload, created_at, target_caregiver_id)
				SELECT id, kind, ref_id, created_by, payload, created_at, target_caregiver_id FROM alerts;
			DROP TABLE alerts;
			ALTER TABLE alerts_v6 RENAME TO alerts;
			CREATE INDEX IF NOT EXISTS idx_alerts_created ON alerts(created_at DESC);
			PRAGMA foreign_keys = ON;
		`); err != nil {
			return fmt.Errorf("failed migrating alerts for low oxygen: %w", err)
		}
	}
	for _, c := range []struct{ col, def string }{
		{"spo2", "INTEGER CHECK (spo2 IS NULL OR spo2 BETWEEN 50 AND 100)"},
		{"temperature_c", "REAL CHECK (temperature_c IS NULL OR temperature_c BETWEEN 30 AND 43)"},
	} {
		var n int
		_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('vitals_logs') WHERE name = ?", c.col).Scan(&n)
		if n == 0 {
			if _, err := database.Exec("ALTER TABLE vitals_logs ADD COLUMN " + c.col + " " + c.def); err != nil {
				return fmt.Errorf("failed adding vitals_logs.%s: %w", c.col, err)
			}
		}
	}

	// Migrate alerts for reposition_overdue, bowel_overdue, low_fluid (issues #27, #28)
	alertsSQL = ""
	_ = database.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='alerts'").Scan(&alertsSQL)
	if !strings.Contains(alertsSQL, "'bowel_overdue'") {
		log.Println("Migrating alerts: adding bowel_overdue and low_fluid kinds...")
		if _, err := database.Exec(`
			PRAGMA foreign_keys = OFF;
			DROP TABLE IF EXISTS alerts_v8;
			CREATE TABLE alerts_v8 (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL CHECK (kind IN ('bp_crisis', 'bp_low', 'alertness', 'fast_positive', 'dose_override', 'vomited', 'correction', 'duty_handover', 'duty_unaccepted', 'emergency_card', 'low_stock', 'spo2_low', 'reposition_overdue', 'bowel_overdue', 'low_fluid')),
				ref_id TEXT,
				created_by TEXT REFERENCES caregivers(id),
				payload TEXT NOT NULL DEFAULT '{}',
				created_at TEXT DEFAULT (datetime('now')),
				target_caregiver_id TEXT REFERENCES caregivers(id)
			);
			INSERT INTO alerts_v8 (id, kind, ref_id, created_by, payload, created_at, target_caregiver_id)
				SELECT id, kind, ref_id, created_by, payload, created_at, target_caregiver_id FROM alerts;
			DROP TABLE alerts;
			ALTER TABLE alerts_v8 RENAME TO alerts;
			CREATE INDEX IF NOT EXISTS idx_alerts_created ON alerts(created_at DESC);
			PRAGMA foreign_keys = ON;
		`); err != nil {
			return fmt.Errorf("failed migrating alerts for bowel_overdue: %w", err)
		}
	}

	// Migrate alerts for backup_failed (issue #51): off-site backups are stale,
	// the restore test failed, or Google sign-in for backups expired.
	alertsSQL = ""
	_ = database.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='alerts'").Scan(&alertsSQL)
	if !strings.Contains(alertsSQL, "'backup_failed'") {
		log.Println("Migrating alerts: adding backup_failed kind...")
		if _, err := database.Exec(`
			PRAGMA foreign_keys = OFF;
			DROP TABLE IF EXISTS alerts_v9;
			CREATE TABLE alerts_v9 (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL CHECK (kind IN ('bp_crisis', 'bp_low', 'alertness', 'fast_positive', 'dose_override', 'vomited', 'correction', 'duty_handover', 'duty_unaccepted', 'emergency_card', 'low_stock', 'spo2_low', 'reposition_overdue', 'bowel_overdue', 'low_fluid', 'backup_failed')),
				ref_id TEXT,
				created_by TEXT REFERENCES caregivers(id),
				payload TEXT NOT NULL DEFAULT '{}',
				created_at TEXT DEFAULT (datetime('now')),
				target_caregiver_id TEXT REFERENCES caregivers(id)
			);
			INSERT INTO alerts_v9 (id, kind, ref_id, created_by, payload, created_at, target_caregiver_id)
				SELECT id, kind, ref_id, created_by, payload, created_at, target_caregiver_id FROM alerts;
			DROP TABLE alerts;
			ALTER TABLE alerts_v9 RENAME TO alerts;
			CREATE INDEX IF NOT EXISTS idx_alerts_created ON alerts(created_at DESC);
			PRAGMA foreign_keys = ON;
		`); err != nil {
			return fmt.Errorf("failed migrating alerts for backup_failed: %w", err)
		}
	}

	// Migrate alerts for bleeding: a bleeding sign was reported (blood thinner).
	alertsSQL = ""
	_ = database.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='alerts'").Scan(&alertsSQL)
	if !strings.Contains(alertsSQL, "'bleeding'") {
		log.Println("Migrating alerts: adding bleeding kind...")
		if _, err := database.Exec(`
			PRAGMA foreign_keys = OFF;
			DROP TABLE IF EXISTS alerts_v10;
			CREATE TABLE alerts_v10 (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL CHECK (kind IN ('bp_crisis', 'bp_low', 'alertness', 'fast_positive', 'dose_override', 'vomited', 'correction', 'duty_handover', 'duty_unaccepted', 'emergency_card', 'low_stock', 'spo2_low', 'reposition_overdue', 'bowel_overdue', 'low_fluid', 'backup_failed', 'bleeding')),
				ref_id TEXT,
				created_by TEXT REFERENCES caregivers(id),
				payload TEXT NOT NULL DEFAULT '{}',
				created_at TEXT DEFAULT (datetime('now')),
				target_caregiver_id TEXT REFERENCES caregivers(id)
			);
			INSERT INTO alerts_v10 (id, kind, ref_id, created_by, payload, created_at, target_caregiver_id)
				SELECT id, kind, ref_id, created_by, payload, created_at, target_caregiver_id FROM alerts;
			DROP TABLE alerts;
			ALTER TABLE alerts_v10 RENAME TO alerts;
			CREATE INDEX IF NOT EXISTS idx_alerts_created ON alerts(created_at DESC);
			PRAGMA foreign_keys = ON;
		`); err != nil {
			return fmt.Errorf("failed migrating alerts for bleeding: %w", err)
		}
	}

	// Ensure care_events table exists
	if _, err := database.Exec(`
		CREATE TABLE IF NOT EXISTS care_events (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL CHECK (kind IN ('reposition', 'fluid', 'bowel', 'hygiene', 'physio')),
			value TEXT NOT NULL,
			caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
			skin_condition TEXT CHECK (skin_condition IN ('ok', 'red_area', '')),
			notes TEXT,
			thickened INTEGER NOT NULL DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now')),
			client_request_id TEXT UNIQUE
		);
		CREATE INDEX IF NOT EXISTS idx_care_events_kind_time ON care_events(kind, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_care_events_time ON care_events(created_at DESC);
	`); err != nil {
		return fmt.Errorf("failed creating care_events table: %w", err)
	}

	// Ensure thickened column exists on care_events (issue #28)
	var thickenedColCount int
	_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('care_events') WHERE name = 'thickened'").Scan(&thickenedColCount)
	if thickenedColCount == 0 {
		if _, err := database.Exec("ALTER TABLE care_events ADD COLUMN thickened INTEGER NOT NULL DEFAULT 0"); err != nil {
			return fmt.Errorf("failed adding thickened column to care_events: %w", err)
		}
	}
	// The immutability trigger below must cover the new values too.
	var trigSQL string
	_ = database.QueryRow("SELECT sql FROM sqlite_master WHERE type='trigger' AND name='vitals_values_immutable'").Scan(&trigSQL)
	if trigSQL != "" && !strings.Contains(trigSQL, "temperature_c") {
		if _, err := database.Exec("DROP TRIGGER vitals_values_immutable"); err != nil {
			return fmt.Errorf("failed replacing vitals immutability trigger: %w", err)
		}
	}

	// Issue #15: a reading's values can never be changed in place, only corrected by a
	// new row, and a reading can be superseded once. (Created after any rebuild above,
	// which would have dropped them.)
	if _, err := database.Exec(`
		CREATE TRIGGER IF NOT EXISTS vitals_values_immutable
		BEFORE UPDATE OF caregiver_id, recorded_at, systolic, diastolic, pulse, blood_sugar, alertness_level, vital_type, notes, fast_result, supersedes_id, spo2, temperature_c
		ON vitals_logs
		BEGIN SELECT RAISE(ABORT, 'vitals readings are immutable: post a correction instead'); END;
		CREATE TRIGGER IF NOT EXISTS vitals_superseded_once
		BEFORE UPDATE OF superseded_by ON vitals_logs
		WHEN OLD.superseded_by IS NOT NULL
		BEGIN SELECT RAISE(ABORT, 'this reading was already corrected'); END;
	`); err != nil {
		return fmt.Errorf("failed creating vitals immutability triggers: %w", err)
	}

	// Issue #23: Immutability triggers on clinical columns and append-only audit trail
	for _, c := range []struct{ col, def string }{
		{"prev_hash", "TEXT"},
		{"row_hash", "TEXT"},
	} {
		var n int
		_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('audit_logs') WHERE name = ?", c.col).Scan(&n)
		if n == 0 {
			if _, err := database.Exec("ALTER TABLE audit_logs ADD COLUMN " + c.col + " " + c.def); err != nil {
				return fmt.Errorf("failed adding audit_logs.%s: %w", c.col, err)
			}
		}
	}

	// Backfill hashes for legacy audit rows, once: before the append-only trigger
	// exists. After that an unhashed row can't be patched (the trigger refuses it,
	// which would stop the backend from starting); /audit/verify reports it instead.
	var appendOnly int
	_ = database.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name='audit_no_update'").Scan(&appendOnly)
	if appendOnly == 0 {
		type legacyRow struct {
			rowid                                                             int64
			id, caregiverID, action, entityType, entityID, details, createdAt string
		}
		// Read every row and close the cursor before the next query: the pool has a
		// single connection, so querying with the cursor open would hang the boot.
		rows, err := database.Query("SELECT rowid, id, COALESCE(caregiver_id, ''), action, entity_type, COALESCE(entity_id, ''), COALESCE(details, ''), COALESCE(created_at, datetime('now')) FROM audit_logs WHERE row_hash IS NULL ORDER BY rowid ASC")
		if err != nil {
			return fmt.Errorf("failed querying unhashed audit logs: %w", err)
		}
		var legacy []legacyRow
		for rows.Next() {
			var lr legacyRow
			if err := rows.Scan(&lr.rowid, &lr.id, &lr.caregiverID, &lr.action, &lr.entityType, &lr.entityID, &lr.details, &lr.createdAt); err != nil {
				rows.Close()
				return fmt.Errorf("failed scanning unhashed audit row: %w", err)
			}
			legacy = append(legacy, lr)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("failed reading unhashed audit logs: %w", err)
		}

		var prevHash string
		_ = database.QueryRow("SELECT row_hash FROM audit_logs WHERE row_hash IS NOT NULL ORDER BY rowid DESC LIMIT 1").Scan(&prevHash)
		for _, lr := range legacy {
			rowHash := audit.ComputeHash(prevHash, lr.id, lr.caregiverID, lr.action, lr.entityID, lr.details, lr.createdAt)
			if _, err := database.Exec("UPDATE audit_logs SET prev_hash = ?, row_hash = ? WHERE rowid = ?", prevHash, rowHash, lr.rowid); err != nil {
				return fmt.Errorf("failed backfilling audit hash: %w", err)
			}
			prevHash = rowHash
		}
	}

	// Triggers for append-only audit trail and immutable clinical logs:
	if _, err := database.Exec(`
		CREATE TRIGGER IF NOT EXISTS audit_no_update
		BEFORE UPDATE ON audit_logs
		BEGIN SELECT RAISE(ABORT, 'audit_logs is append-only'); END;

		CREATE TRIGGER IF NOT EXISTS audit_no_delete
		BEFORE DELETE ON audit_logs
		BEGIN SELECT RAISE(ABORT, 'audit_logs is append-only'); END;

		CREATE TRIGGER IF NOT EXISTS med_logs_values_immutable
		BEFORE UPDATE OF medication_id, caregiver_id, slot, scheduled_date, status, notes, dosage_snapshot
		ON medication_logs
		BEGIN SELECT RAISE(ABORT, 'medication log entries are immutable: void the dose instead'); END;

		CREATE TRIGGER IF NOT EXISTS med_logs_voided_once
		BEFORE UPDATE OF voided_at, voided_by, void_reason ON medication_logs
		WHEN OLD.voided_at IS NOT NULL
		BEGIN SELECT RAISE(ABORT, 'this dose was already voided'); END;

		CREATE TRIGGER IF NOT EXISTS med_logs_no_delete
		BEFORE DELETE ON medication_logs
		BEGIN SELECT RAISE(ABORT, 'medication logs cannot be deleted: void the dose instead'); END;

		CREATE TRIGGER IF NOT EXISTS insulin_logs_voided_once
		BEFORE UPDATE OF voided_at, voided_by, void_reason ON insulin_logs
		WHEN OLD.voided_at IS NOT NULL
		BEGIN SELECT RAISE(ABORT, 'this injection was already voided'); END;

		CREATE TRIGGER IF NOT EXISTS insulin_logs_no_delete
		BEFORE DELETE ON insulin_logs
		BEGIN SELECT RAISE(ABORT, 'insulin logs cannot be deleted: void the injection instead'); END;

		CREATE TRIGGER IF NOT EXISTS vitals_no_delete
		BEFORE DELETE ON vitals_logs
		BEGIN SELECT RAISE(ABORT, 'vitals logs cannot be deleted: post a correction instead'); END;
	`); err != nil {
		return fmt.Errorf("failed creating immutability triggers: %w", err)
	}
	if operatorResetID != "" {
		if err := audit.Record(database, "", "2FA_RESET_BY_OPERATOR", "caregiver", operatorResetID, "DISABLE_2FA_FOR"); err != nil {
			return fmt.Errorf("failed auditing the operator 2FA reset: %w", err)
		}
	}

	// 5. Safety net: the double-dose lock depends entirely on this UNIQUE constraint.
	// Refuse to start if a future table rebuild ever drops it.
	if err := verifyDoseUniqueConstraint(database); err != nil {
		return err
	}

	// 6. Safe migration for handover_notes to remove old restrictive CHECK constraint on shift_type
	var handoverSQL string
	_ = database.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='handover_notes'").Scan(&handoverSQL)
	if strings.Contains(handoverSQL, "shift_type IN") {
		log.Println("Migrating handover_notes table: removing shift_type CHECK constraint...")
		migrationSQL := `
			PRAGMA foreign_keys = OFF;
			CREATE TABLE IF NOT EXISTS handover_notes_v2 (
				id TEXT PRIMARY KEY,
				caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
				created_at TEXT DEFAULT (datetime('now')),
				shift_type TEXT NOT NULL DEFAULT 'morning',
				summary TEXT NOT NULL,
				urgent_flag INTEGER DEFAULT 0
			);
			INSERT OR IGNORE INTO handover_notes_v2 (id, caregiver_id, created_at, shift_type, summary, urgent_flag)
			SELECT id, caregiver_id, created_at, shift_type, summary, urgent_flag FROM handover_notes;
			DROP TABLE handover_notes;
			ALTER TABLE handover_notes_v2 RENAME TO handover_notes;
			CREATE INDEX IF NOT EXISTS idx_handover_date ON handover_notes(created_at DESC);
			PRAGMA foreign_keys = ON;
		`
		if _, err := database.Exec(migrationSQL); err != nil {
			return fmt.Errorf("failed migrating handover_notes table: %w", err)
		}
	}

	// Several photos per medicine (box front, back, the pill): photo_urls. A
	// medicine that already has an uploaded photo keeps it as the first one.
	var photoCols int
	_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('medications') WHERE name = 'photo_urls'").Scan(&photoCols)
	if photoCols == 0 {
		if _, err := database.Exec(`ALTER TABLE medications ADD COLUMN photo_urls TEXT NOT NULL DEFAULT '[]'`); err != nil {
			return fmt.Errorf("failed adding medications.photo_urls: %w", err)
		}
		if _, err := database.Exec(`UPDATE medications SET photo_urls = json_array(image_url)
			WHERE image_url LIKE '/api/v1/medications/photos/%'`); err != nil {
			return fmt.Errorf("failed copying medicine photos: %w", err)
		}
	}

	// 7. PRN (As-Needed) medications and prn_logs table (issue #25)
	for _, c := range []struct{ col, def string }{
		{"is_prn", "INTEGER NOT NULL DEFAULT 0"},
		{"prn_min_hours", "REAL"},
		{"prn_max_per_24h", "INTEGER"},
	} {
		var n int
		_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('medications') WHERE name = ?", c.col).Scan(&n)
		if n == 0 {
			if _, err := database.Exec("ALTER TABLE medications ADD COLUMN " + c.col + " " + c.def); err != nil {
				return fmt.Errorf("failed adding medications.%s: %w", c.col, err)
			}
		}
	}
	if _, err := database.Exec(`
		CREATE TABLE IF NOT EXISTS prn_logs (
			id TEXT PRIMARY KEY,
			medication_id TEXT NOT NULL REFERENCES medications(id) ON DELETE CASCADE,
			caregiver_id TEXT NOT NULL REFERENCES caregivers(id),
			given_at TEXT DEFAULT (datetime('now')),
			reason_tag TEXT NOT NULL CHECK (reason_tag IN ('headache', 'pain', 'fever', 'other')),
			notes TEXT,
			voided_at TEXT,
			voided_by TEXT REFERENCES caregivers(id),
			void_reason TEXT,
			dosage_snapshot TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_prn_logs_med_given ON prn_logs(medication_id, given_at DESC);
		CREATE INDEX IF NOT EXISTS idx_prn_logs_given ON prn_logs(given_at DESC);
	`); err != nil {
		return fmt.Errorf("failed creating prn_logs table: %w", err)
	}
	// When the entry was made (given_at may be backdated): the own-undo window.
	var prnRecorded int
	_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('prn_logs') WHERE name = 'recorded_at'").Scan(&prnRecorded)
	if prnRecorded == 0 {
		if _, err := database.Exec("ALTER TABLE prn_logs ADD COLUMN recorded_at TEXT"); err != nil {
			return fmt.Errorf("failed adding prn_logs.recorded_at: %w", err)
		}
	}
	// PRN history follows the same audit-trail rules as medication_logs (#23).
	if _, err := database.Exec(`
		CREATE TRIGGER IF NOT EXISTS prn_logs_values_immutable
		BEFORE UPDATE OF medication_id, caregiver_id, given_at, reason_tag, notes, dosage_snapshot, recorded_at
		ON prn_logs
		BEGIN SELECT RAISE(ABORT, 'PRN entries are immutable: cancel the dose instead'); END;

		CREATE TRIGGER IF NOT EXISTS prn_logs_voided_once
		BEFORE UPDATE OF voided_at, voided_by, void_reason ON prn_logs
		WHEN OLD.voided_at IS NOT NULL
		BEGIN SELECT RAISE(ABORT, 'this PRN dose was already cancelled'); END;

		CREATE TRIGGER IF NOT EXISTS prn_logs_no_delete
		BEFORE DELETE ON prn_logs
		BEGIN SELECT RAISE(ABORT, 'PRN entries cannot be deleted: cancel the dose instead'); END;
	`); err != nil {
		return fmt.Errorf("failed creating prn_logs triggers: %w", err)
	}

	for _, c := range []struct{ col, def string }{
		{"frequency_slots", "TEXT NOT NULL DEFAULT '[]'"},
		{"slot_times", "TEXT NOT NULL DEFAULT '{}'"},
	} {
		var n int
		_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('insulin_plans') WHERE name = ?", c.col).Scan(&n)
		if n == 0 {
			if _, err := database.Exec("ALTER TABLE insulin_plans ADD COLUMN " + c.col + " " + c.def); err != nil {
				return fmt.Errorf("failed adding insulin_plans.%s: %w", c.col, err)
			}
		}
	}

	for _, c := range []struct{ col, def string }{
		{"voided_at", "TEXT"},
		{"voided_by", "TEXT REFERENCES caregivers(id)"},
		{"void_reason", "TEXT"},
	} {
		var n int
		_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('insulin_logs') WHERE name = ?", c.col).Scan(&n)
		if n == 0 {
			if _, err := database.Exec("ALTER TABLE insulin_logs ADD COLUMN " + c.col + " " + c.def); err != nil {
				return fmt.Errorf("failed adding insulin_logs.%s: %w", c.col, err)
			}
		}
	}
	_, _ = database.Exec(`CREATE INDEX IF NOT EXISTS idx_insulin_logs_voided ON insulin_logs(plan_id, voided_at, given_at DESC)`)

	if err := seedSuperAdmin(database); err != nil {
		return fmt.Errorf("failed seeding super admin: %w", err)
	}

	if err := seedShifts(database); err != nil {
		return fmt.Errorf("failed seeding shifts: %w", err)
	}

	log.Println("Database schema migrated and verified successfully.")
	return nil
}

func seedSuperAdmin(database *sql.DB) error {
	var count int
	err := database.QueryRow("SELECT COUNT(*) FROM caregivers").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	adminUser := strings.TrimSpace(os.Getenv("SUPER_ADMIN_USER"))
	if adminUser == "" {
		adminUser = "admin"
	}

	adminPIN := strings.TrimSpace(os.Getenv("SUPER_ADMIN_PIN"))
	if adminPIN == "" {
		adminPIN = "1234"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(adminPIN), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed hashing super admin PIN: %w", err)
	}
	pinHash := string(hash)

	email := adminUser
	if !strings.Contains(email, "@") {
		email = fmt.Sprintf("%s@birrcare.local", strings.ToLower(adminUser))
	}

	_, err = database.Exec(`
		INSERT INTO caregivers (id, name, email, role, pin_hash, avatar_color, preferred_lang)
		VALUES ('cg_superadmin', ?, ?, 'super_admin', ?, '#d97706', 'en')
	`, adminUser, email, pinHash)
	if err != nil {
		return fmt.Errorf("failed inserting super admin: %w", err)
	}

	log.Printf("Seeded initial Super Admin account (%s) from environment configuration.\n", adminUser)
	return nil
}

// verifyDoseUniqueConstraint asserts medication_logs has the double-dose lock:
// a UNIQUE index on exactly (medication_id, scheduled_date, slot) that is PARTIAL
// on "voided_at IS NULL" (one active row per dose; voided mis-taps don't block the
// real entry). Boot is refused if it is missing, or replaced by a non-partial one.
func verifyDoseUniqueConstraint(database *sql.DB) error {
	rows, err := database.Query(`SELECT name, partial FROM pragma_index_list('medication_logs') WHERE "unique" = 1`)
	if err != nil {
		return fmt.Errorf("failed inspecting medication_logs indexes: %w", err)
	}
	type idx struct {
		name    string
		partial bool
	}
	var list []idx
	for rows.Next() {
		var i idx
		if err := rows.Scan(&i.name, &i.partial); err == nil {
			list = append(list, i)
		}
	}
	rows.Close() // release the single pooled connection before the next query

	for _, i := range list {
		cols, err := database.Query(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, i.name)
		if err != nil {
			continue
		}
		var got []string
		for cols.Next() {
			var c string
			if err := cols.Scan(&c); err == nil {
				got = append(got, c)
			}
		}
		cols.Close()
		if strings.Join(got, ",") != "medication_id,scheduled_date,slot" {
			continue
		}
		if !i.partial {
			return fmt.Errorf("SAFETY: medication_logs unique index %q is not partial on voided_at IS NULL - a voided mis-tap would block the real dose", i.name)
		}
		var def string
		_ = database.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`, i.name).Scan(&def)
		if !strings.Contains(strings.Join(strings.Fields(strings.ToLower(def)), " "), "where voided_at is null") {
			return fmt.Errorf("SAFETY: medication_logs unique index %q must be partial on exactly voided_at IS NULL", i.name)
		}
		return nil
	}
	return fmt.Errorf("SAFETY: medication_logs is missing UNIQUE(medication_id, scheduled_date, slot) WHERE voided_at IS NULL - double-dose lock disabled")
}

func seedShifts(database *sql.DB) error {
	var count int
	err := database.QueryRow("SELECT COUNT(*) FROM shifts").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	// Default: 24-Hour Shift (single shift per day covering 24 hours)
	_, err = database.Exec(`
		INSERT INTO shifts (id, name_ar, name_en, start_time, end_time, duration_hours, color, display_order, is_active)
		VALUES ('shift_24h', 'وردية 24 ساعة (يوم كامل)', '24-Hour Full Day Shift', '08:00', '08:00', 24.0, '#059669', 1, 1)
	`)
	if err != nil {
		return err
	}

	// Seed default today's assignment to first super_admin or caregiver
	var cgID string
	_ = database.QueryRow("SELECT id FROM caregivers WHERE role = 'super_admin' OR role = 'caregiver' ORDER BY created_at ASC LIMIT 1").Scan(&cgID)
	if cgID != "" {
		todayStr := time.Now().Format("2006-01-02")
		_, _ = database.Exec(`
			INSERT OR IGNORE INTO shift_assignments (id, shift_id, caregiver_id, date, notes)
			VALUES ('assign_default', 'shift_24h', ?, ?, 'Initial shift assignment')
		`, cgID, todayStr)
	}

	log.Println("Seeded default 24-hour caregiver shift configuration.")
	return nil
}
