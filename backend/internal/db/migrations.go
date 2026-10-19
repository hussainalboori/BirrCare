package db

import (
	"database/sql"
	"fmt"
	"log"

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
`

// Migrate runs schema migrations and seeds initial data if tables are empty.
func Migrate(database *sql.DB) error {
	if _, err := database.Exec(SchemaSQL); err != nil {
		return fmt.Errorf("failed executing schema: %w", err)
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
		{"cg_fatima", "فاطمة (Fatima)", "fatima@family.local", "caregiver", "#059669", "ar"},
		{"cg_ali", "علي (Ali)", "ali@family.local", "caregiver", "#2563eb", "ar"},
		{"cg_zainab", "زينب (Zainab)", "zainab@family.local", "caregiver", "#7c3aed", "ar"},
		{"cg_hussain", "حسين (Hussain)", "hussain@family.local", "admin", "#d97706", "ar"},
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
		id, nameAr, nameEn, dosage, instAr, instEn, slots string
	}{
		{
			"med_aspirin",
			"أسبرين حماية",
			"Aspirin Protect",
			"81mg",
			"قرص واحد يومياً بعد الإفطار مع ماء وافر",
			"One tablet daily after breakfast with plenty of water",
			`["morning"]`,
		},
		{
			"med_clopidogrel",
			"بلافيكس (كلوبيدوجريل)",
			"Plavix (Clopidogrel)",
			"75mg",
			"قرص واحد يومياً في نفس الموعد لمنع التجلط",
			"One tablet daily at the exact same hour to prevent thrombosis",
			`["evening"]`,
		},
		{
			"med_amlodipine",
			"أملوديبين (للضغط)",
			"Amlodipine (Blood Pressure)",
			"5mg",
			"قرص واحد صباحاً لضبط ضغط الدم الشرياني",
			"One tablet in the morning to maintain blood pressure within range",
			`["morning"]`,
		},
		{
			"med_atorvastatin",
			"ليبيتور (أتورفاستاتين)",
			"Lipitor (Atorvastatin)",
			"40mg",
			"قرص واحد مساءً قبل النوم لخفض الكوليسترول",
			"One tablet at bedtime to regulate lipid panel",
			`["bedtime"]`,
		},
		{
			"med_citicoline",
			"سيتيكولين (منشط عصبي)",
			"Citicoline (Neuroprotector)",
			"500mg",
			"قرص مرتين يومياً صباحاً ومساءً لدعم التعافي العصبي",
			"Twice daily morning and evening to support neuro-recovery",
			`["morning", "evening"]`,
		},
	}

	for _, m := range meds {
		_, err := database.Exec(`
			INSERT INTO medications (id, name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots, created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'cg_hussain')
		`, m.id, m.nameAr, m.nameEn, m.dosage, m.instAr, m.instEn, m.slots)
		if err != nil {
			return err
		}
	}

	log.Println("Seeded post-stroke medical prescription catalog.")
	return nil
}
