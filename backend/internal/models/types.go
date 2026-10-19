package models

import "time"

// Caregiver represents a family member or caregiver authorized in the system.
type Caregiver struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email,omitempty"`
	Role          string    `json:"role"` // 'admin', 'caregiver'
	PINHash       string    `json:"-"`
	AvatarColor   string    `json:"avatar_color"`
	PreferredLang string    `json:"preferred_lang"` // 'ar', 'en'
	Phone         string    `json:"phone,omitempty"`
	IsActive      bool      `json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
}

// Medication represents a prescribed medicine with bilingual labels and timing slots.
type Medication struct {
	ID             string    `json:"id"`
	NameAr         string    `json:"name_ar"`
	NameEn         string    `json:"name_en"`
	Dosage         string    `json:"dosage"`
	InstructionsAr string    `json:"instructions_ar,omitempty"`
	InstructionsEn string    `json:"instructions_en,omitempty"`
	FrequencySlots []string  `json:"frequency_slots"` // e.g., ["morning", "afternoon", "evening", "bedtime"]
	IsActive       bool      `json:"is_active"`
	CreatedBy      string    `json:"created_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// MedicationLog represents an intake entry with double-dose safety constraint.
type MedicationLog struct {
	ID            string     `json:"id"`
	MedicationID  string     `json:"medication_id"`
	CaregiverID   string     `json:"caregiver_id"`
	Slot          string     `json:"slot"`
	ScheduledDate string     `json:"scheduled_date"` // 'YYYY-MM-DD'
	TakenAt       *time.Time `json:"taken_at,omitempty"`
	Status        string     `json:"status"` // 'taken', 'skipped'
	Notes         string     `json:"notes,omitempty"`
	CaregiverName string     `json:"caregiver_name,omitempty"`
	AvatarColor   string     `json:"avatar_color,omitempty"`
}

// ScheduledDose merges a medication with its log status for a specific slot on a given date.
type ScheduledDose struct {
	Medication Medication     `json:"medication"`
	Slot       string         `json:"slot"`
	Log        *MedicationLog `json:"log,omitempty"`
}

// VitalsLog represents cardiovascular and cognitive metrics critical for stroke recovery.
type VitalsLog struct {
	ID             string    `json:"id"`
	CaregiverID    string    `json:"caregiver_id"`
	CaregiverName  string    `json:"caregiver_name,omitempty"`
	AvatarColor    string    `json:"avatar_color,omitempty"`
	RecordedAt     time.Time `json:"recorded_at"`
	Systolic       int       `json:"systolic"`
	Diastolic      int       `json:"diastolic"`
	Pulse          int       `json:"pulse"`
	BloodSugar     *float64  `json:"blood_sugar,omitempty"`
	AlertnessLevel string    `json:"alertness_level"` // 'alert', 'confused', 'restless', 'drowsy'
	Notes          string    `json:"notes,omitempty"`
	BPStatus       string    `json:"bp_status"` // 'NORMAL', 'ELEVATED', 'STAGE_2', 'CRISIS'
}

// HandoverNote captures shift transitions and urgent flags between siblings.
type HandoverNote struct {
	ID            string    `json:"id"`
	CaregiverID   string    `json:"caregiver_id"`
	CaregiverName string    `json:"caregiver_name,omitempty"`
	AvatarColor   string    `json:"avatar_color,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	ShiftType     string    `json:"shift_type"` // 'morning', 'evening', 'night'
	Summary       string    `json:"summary"`
	UrgentFlag    bool      `json:"urgent_flag"`
}

// AuditLog tracks every state mutation for forensic accountability.
type AuditLog struct {
	ID          string    `json:"id"`
	CaregiverID string    `json:"caregiver_id"`
	Action      string    `json:"action"`
	EntityType  string    `json:"entity_type"`
	EntityID    string    `json:"entity_id,omitempty"`
	Details     string    `json:"details,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// SSEMessage is the payload envelope broadcasted over Server-Sent Events.
type SSEMessage struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"` // "MED_LOGGED", "VITAL_LOGGED", "NOTE_ADDED", "CAREGIVER_SWITCHED"
	Timestamp time.Time   `json:"timestamp"`
	Actor     ActorInfo   `json:"actor"`
	Payload   interface{} `json:"payload"`
}

// ActorInfo provides the sibling's basic badge info for real-time notifications.
type ActorInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AvatarColor string `json:"avatar_color"`
}
