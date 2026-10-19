package models

import "time"

const (
	RoleSuperAdmin = "super_admin"
	RoleAdmin      = "admin"
	RoleCaregiver  = "caregiver"
	RoleViewer     = "viewer"
)

// Caregiver represents a family member or caregiver authorized in the system.
type Caregiver struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email,omitempty"`
	Role          string    `json:"role"` // 'super_admin', 'admin', 'caregiver', 'viewer'
	PINHash       string    `json:"-"`
	AvatarColor   string    `json:"avatar_color"`
	PreferredLang string    `json:"preferred_lang"` // 'ar', 'en'
	Phone         string    `json:"phone,omitempty"`
	IsActive      bool      `json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
}

type CreateUserRequest struct {
	Name          string `json:"name"`
	Email         string `json:"email,omitempty"`
	Role          string `json:"role"` // 'super_admin', 'admin', 'caregiver', 'viewer'
	PIN           string `json:"pin"`  // 4-digit PIN
	AvatarColor   string `json:"avatar_color,omitempty"`
	PreferredLang string `json:"preferred_lang,omitempty"`
	Phone         string `json:"phone,omitempty"`
}

type UpdateUserRequest struct {
	Name          string `json:"name"`
	Email         string `json:"email,omitempty"`
	Role          string `json:"role"`
	AvatarColor   string `json:"avatar_color,omitempty"`
	PreferredLang string `json:"preferred_lang,omitempty"`
	Phone         string `json:"phone,omitempty"`
	IsActive      *bool  `json:"is_active,omitempty"`
}

type ResetPinRequest struct {
	NewPIN string `json:"new_pin"`
}

// Medication represents a prescribed medicine with bilingual labels, timing slots, days of week, and clock times.
type Medication struct {
	ID             string            `json:"id"`
	NameAr         string            `json:"name_ar"`
	NameEn         string            `json:"name_en"`
	Dosage         string            `json:"dosage"`
	InstructionsAr string            `json:"instructions_ar,omitempty"`
	InstructionsEn string            `json:"instructions_en,omitempty"`
	FrequencySlots []string          `json:"frequency_slots"` // e.g., ["morning", "afternoon", "evening", "bedtime"]
	DaysOfWeek     []string          `json:"days_of_week"`    // e.g., ["sun", "mon", "tue", "wed", "thu", "fri", "sat"]
	SlotTimes      map[string]string `json:"slot_times"`      // e.g., {"morning": "08:00", "evening": "20:00"}
	ImageURL       string            `json:"image_url,omitempty"`
	IsActive       bool              `json:"is_active"`
	CreatedBy      string            `json:"created_by,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
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
	Time       string         `json:"time,omitempty"` // scheduled dose clock time, e.g. "08:00"
	Log        *MedicationLog `json:"log,omitempty"`
}

// VitalsLog represents cardiovascular and cognitive metrics critical for stroke recovery.
type VitalsLog struct {
	ID             string    `json:"id"`
	CaregiverID    string    `json:"caregiver_id"`
	CaregiverName  string    `json:"caregiver_name,omitempty"`
	AvatarColor    string    `json:"avatar_color,omitempty"`
	RecordedAt     time.Time `json:"recorded_at"`
	VitalType      string    `json:"vital_type,omitempty"` // 'bp', 'sugar', 'alertness', 'full'
	Systolic       *int      `json:"systolic,omitempty"`
	Diastolic      *int      `json:"diastolic,omitempty"`
	Pulse          *int      `json:"pulse,omitempty"`
	BloodSugar     *float64  `json:"blood_sugar,omitempty"`
	AlertnessLevel string    `json:"alertness_level"` // 'alert', 'confused', 'restless', 'drowsy'
	Notes          string    `json:"notes,omitempty"`
	BPStatus       string    `json:"bp_status,omitempty"` // 'NORMAL', 'ELEVATED', 'STAGE_2', 'CRISIS'
	SugarStatus    string    `json:"sugar_status,omitempty"` // 'LOW', 'NORMAL', 'ELEVATED', 'VERY_HIGH'
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

// Shift represents a caregiver work shift window (e.g. 24h shift, 4h shift, 8h shift, or custom).
type Shift struct {
	ID            string    `json:"id"`
	NameAr        string    `json:"name_ar"`
	NameEn        string    `json:"name_en"`
	StartTime     string    `json:"start_time"` // "08:00"
	EndTime       string    `json:"end_time"`   // "12:00" or "08:00"
	DurationHours float64   `json:"duration_hours"`
	Color         string    `json:"color"`
	DisplayOrder  int       `json:"display_order"`
	IsActive      bool      `json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
}

// ShiftAssignment assigns a caregiver to a specific shift on a given date (YYYY-MM-DD).
type ShiftAssignment struct {
	ID             string    `json:"id"`
	ShiftID        string    `json:"shift_id"`
	CaregiverID    string    `json:"caregiver_id"`
	Date           string    `json:"date"` // "YYYY-MM-DD"
	Notes          string    `json:"notes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	CaregiverName  string    `json:"caregiver_name,omitempty"`
	AvatarColor    string    `json:"avatar_color,omitempty"`
	CaregiverRole  string    `json:"caregiver_role,omitempty"`
	ShiftNameAr    string    `json:"shift_name_ar,omitempty"`
	ShiftNameEn    string    `json:"shift_name_en,omitempty"`
	ShiftStartTime string    `json:"shift_start_time,omitempty"`
	ShiftEndTime   string    `json:"shift_end_time,omitempty"`
	ShiftColor     string    `json:"shift_color,omitempty"`
}

// CurrentDutyStatus conveys who is currently on duty right now.
type CurrentDutyStatus struct {
	ActiveShift      *Shift           `json:"active_shift,omitempty"`
	ActiveAssignment *ShiftAssignment `json:"active_assignment,omitempty"`
	ActiveCaregiver  *Caregiver       `json:"active_caregiver,omitempty"`
	IsOnDuty         bool             `json:"is_on_duty"`
	CurrentTime      string           `json:"current_time"`
	Date             string           `json:"date"`
}

