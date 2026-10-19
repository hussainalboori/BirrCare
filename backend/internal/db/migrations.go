package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"golang.org/x/crypto/bcrypt"
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
    image_url TEXT,
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
    systolic INTEGER CHECK (systolic IS NULL OR systolic BETWEEN 50 AND 300),
    diastolic INTEGER CHECK (diastolic IS NULL OR diastolic BETWEEN 30 AND 200),
    pulse INTEGER CHECK (pulse IS NULL OR pulse BETWEEN 30 AND 220),
    blood_sugar REAL,
    alertness_level TEXT DEFAULT 'alert' CHECK (alertness_level IN ('alert', 'confused', 'restless', 'drowsy')),
    vital_type TEXT DEFAULT 'full',
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

-- 7. Server-side settings (e.g. persisted session signing secret)
CREATE TABLE IF NOT EXISTS app_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_med_logs_date ON medication_logs(scheduled_date);
CREATE INDEX IF NOT EXISTS idx_vitals_date ON vitals_logs(recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_handover_date ON handover_notes(created_at DESC);
`

// Migrate runs schema migrations and seeds initial data if tables are empty.
func Migrate(database *sql.DB) error {
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

	// 3. Migrate medications table: add image_url column if missing
	var colCount int
	_ = database.QueryRow("SELECT COUNT(*) FROM pragma_table_info('medications') WHERE name='image_url'").Scan(&colCount)
	if colCount == 0 {
		log.Println("Migrating medications table: adding image_url column...")
		_, _ = database.Exec("ALTER TABLE medications ADD COLUMN image_url TEXT")
	}

	// Update visual pill image paths for seeded medications if empty
	_, _ = database.Exec("UPDATE medications SET image_url = '/meds/aspirin.svg' WHERE id = 'med_aspirin' AND (image_url IS NULL OR image_url = '')")
	_, _ = database.Exec("UPDATE medications SET image_url = '/meds/clopidogrel.svg' WHERE id = 'med_clopidogrel' AND (image_url IS NULL OR image_url = '')")
	_, _ = database.Exec("UPDATE medications SET image_url = '/meds/amlodipine.svg' WHERE id = 'med_amlodipine' AND (image_url IS NULL OR image_url = '')")
	_, _ = database.Exec("UPDATE medications SET image_url = '/meds/atorvastatin.svg' WHERE id = 'med_atorvastatin' AND (image_url IS NULL OR image_url = '')")
	_, _ = database.Exec("UPDATE medications SET image_url = '/meds/citicoline.svg' WHERE id = 'med_citicoline' AND (image_url IS NULL OR image_url = '')")

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

	// 5. Safety net: the double-dose lock depends entirely on this UNIQUE constraint.
	// Refuse to start if a future table rebuild ever drops it.
	if err := verifyDoseUniqueConstraint(database); err != nil {
		return err
	}

	if err := seedCaregivers(database); err != nil {
		return fmt.Errorf("failed seeding caregivers: %w", err)
	}

	if err := seedMedications(database); err != nil {
		return fmt.Errorf("failed seeding medications: %w", err)
	}

	log.Println("Database schema migrated and verified successfully.")
	return nil
}

func seedCaregivers(database *sql.DB) error {
	var count int
	err := database.QueryRow("SELECT COUNT(*) FROM caregivers").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	// Default PIN: "1234"
	hash, err := bcrypt.GenerateFromPassword([]byte("1234"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	pinHash := string(hash)

	caregivers := []struct {
		id, name, email, role, avatarColor, lang string
	}{
		{"cg_hussain", "حسين (Hussain)", "hussain@family.local", "super_admin", "#d97706", "ar"},
		{"cg_ali", "علي (Ali)", "ali@family.local", "admin", "#2563eb", "ar"},
		{"cg_fatima", "فاطمة (Fatima)", "fatima@family.local", "caregiver", "#059669", "ar"},
		{"cg_zainab", "زينب (Zainab)", "zainab@family.local", "viewer", "#7c3aed", "ar"},
	}

	for _, c := range caregivers {
		_, err := database.Exec(`
			INSERT INTO caregivers (id, name, email, role, pin_hash, avatar_color, preferred_lang)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, c.id, c.name, c.email, c.role, pinHash, c.avatarColor, c.lang)
		if err != nil {
			return err
		}
	}

	log.Println("Seeded initial family caregivers (Default PIN: 1234)")
	return nil
}

func seedMedications(database *sql.DB) error {
	var count int
	err := database.QueryRow("SELECT COUNT(*) FROM medications").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	meds := []struct {
		id, nameAr, nameEn, dosage, instAr, instEn, slots, imgURL string
	}{
		{
			"med_aspirin",
			"أسبرين حماية",
			"Aspirin Protect",
			"81mg",
			"قرص واحد يومياً بعد الإفطار مع ماء وافر",
			"One tablet daily after breakfast with plenty of water",
			`["morning"]`,
			"/meds/aspirin.svg",
		},
		{
			"med_clopidogrel",
			"بلافيكس (كلوبيدوجريل)",
			"Plavix (Clopidogrel)",
			"75mg",
			"قرص واحد يومياً في نفس الموعد لمنع التجلط",
			"One tablet daily at the exact same hour to prevent thrombosis",
			`["evening"]`,
			"/meds/clopidogrel.svg",
		},
		{
			"med_amlodipine",
			"أملوديبين (للضغط)",
			"Amlodipine (Blood Pressure)",
			"5mg",
			"قرص واحد صباحاً لضبط ضغط الدم الشرياني",
			"One tablet in the morning to maintain blood pressure within range",
			`["morning"]`,
			"/meds/amlodipine.svg",
		},
		{
			"med_atorvastatin",
			"ليبيتور (أتورفاستاتين)",
			"Lipitor (Atorvastatin)",
			"40mg",
			"قرص واحد مساءً قبل النوم لخفض الكوليسترول",
			"One tablet at bedtime to regulate lipid panel",
			`["bedtime"]`,
			"/meds/atorvastatin.svg",
		},
		{
			"med_citicoline",
			"سيتيكولين (منشط عصبي)",
			"Citicoline (Neuroprotector)",
			"500mg",
			"قرص مرتين يومياً صباحاً ومساءً لدعم التعافي العصبي",
			"Twice daily morning and evening to support neuro-recovery",
			`["morning", "evening"]`,
			"/meds/citicoline.svg",
		},
	}

	for _, m := range meds {
		_, err := database.Exec(`
			INSERT INTO medications (id, name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots, image_url, created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'cg_hussain')
		`, m.id, m.nameAr, m.nameEn, m.dosage, m.instAr, m.instEn, m.slots, m.imgURL)
		if err != nil {
			return err
		}
	}

	log.Println("Seeded post-stroke medical prescription catalog.")
	return nil
}

// verifyDoseUniqueConstraint asserts medication_logs has a UNIQUE index covering
// exactly (medication_id, scheduled_date, slot).
func verifyDoseUniqueConstraint(database *sql.DB) error {
	rows, err := database.Query(`SELECT name FROM pragma_index_list('medication_logs') WHERE "unique" = 1`)
	if err != nil {
		return fmt.Errorf("failed inspecting medication_logs indexes: %w", err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil {
			names = append(names, n)
		}
	}
	rows.Close() // release the single pooled connection before the next query

	for _, n := range names {
		cols, err := database.Query(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, n)
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
		if strings.Join(got, ",") == "medication_id,scheduled_date,slot" {
			return nil
		}
	}
	return fmt.Errorf("SAFETY: medication_logs is missing UNIQUE(medication_id, scheduled_date, slot) - double-dose lock disabled")
}
