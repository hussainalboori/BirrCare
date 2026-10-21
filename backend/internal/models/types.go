package models

import (
	"encoding/json"
	"time"
)

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
	// TOTPEnabled: signing in needs the PIN AND an authenticator code.
	TOTPEnabled bool `json:"totp_enabled,omitempty"`
	// SessionsValidAfter (unix seconds): session cookies issued earlier are refused.
	SessionsValidAfter int64 `json:"-"`
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
	// MinHoursBetween is the doctor's minimum gap between doses; nil = automatic
	// (derived from the schedule), 0 = no interval lock.
	MinHoursBetween *float64 `json:"min_hours_between"`
	// Anticoagulant / antiplatelet: listed first in the doctor report (issue #14).
	IsCritical bool `json:"is_critical"`
	// Stock (issue #20): nil = not tracked.
	StockUnits          *float64  `json:"stock_units"`
	UnitsPerDose        float64   `json:"units_per_dose"`
	RefillThresholdDays int       `json:"refill_threshold_days"`
	IsActive            bool      `json:"is_active"`
	CreatedBy           string    `json:"created_by,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

// LastDose is the most recent taken dose of a medication, on any date.
type LastDose struct {
	TakenAt       string  `json:"taken_at"` // RFC3339
	Slot          string  `json:"slot"`
	ScheduledDate string  `json:"scheduled_date"`
	CaregiverName string  `json:"caregiver_name"`
	AvatarColor   string  `json:"avatar_color,omitempty"`
	HoursSince    float64 `json:"hours_since"`
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
	RecordedAt    string     `json:"recorded_at,omitempty"` // RFC3339: when it was tapped (undo window)
	// Set when the entry was voided (mis-tap); voided entries never count as given.
	VoidedAt     string `json:"voided_at,omitempty"`
	VoidedByName string `json:"voided_by_name,omitempty"`
	VoidReason   string `json:"void_reason,omitempty"`
	// Follow-ups such as "vomited 10 min later" (issue #9).
	Events []MedicationLogEvent `json:"events,omitempty"`
}

// MedicationLogEvent is a follow-up on a given dose (e.g. vomited).
type MedicationLogEvent struct {
	ID            string `json:"id"`
	Type          string `json:"type"` // 'vomited'
	MinutesAfter  *int   `json:"minutes_after,omitempty"`
	Notes         string `json:"notes,omitempty"`
	CaregiverName string `json:"caregiver_name,omitempty"`
	CreatedAt     string `json:"created_at"` // RFC3339
}

// ScheduledDose merges a medication with its log status for a specific slot on a given date.
type ScheduledDose struct {
	Medication Medication     `json:"medication"`
	Slot       string         `json:"slot"`
	Time       string         `json:"time,omitempty"` // scheduled dose clock time, e.g. "08:00"
	Log        *MedicationLog `json:"log,omitempty"`
	// Voided entries for this card (struck through in the UI).
	VoidedLogs []MedicationLog `json:"voided_logs,omitempty"`
	// Interval lock (issue #7), independent of the date being viewed.
	LastTaken     *LastDose `json:"last_taken,omitempty"`
	IntervalHours float64   `json:"interval_hours"`
	NextAllowedAt string    `json:"next_allowed_at,omitempty"` // RFC3339; set while still locked
}

// VitalsLog represents cardiovascular and cognitive metrics critical for stroke recovery.
type VitalsLog struct {
	ID            string    `json:"id"`
	CaregiverID   string    `json:"caregiver_id"`
	CaregiverName string    `json:"caregiver_name,omitempty"`
	AvatarColor   string    `json:"avatar_color,omitempty"`
	RecordedAt    time.Time `json:"recorded_at"`
	VitalType     string    `json:"vital_type,omitempty"` // 'bp', 'sugar', 'alertness', 'full'
	Systolic      *int      `json:"systolic,omitempty"`
	Diastolic     *int      `json:"diastolic,omitempty"`
	Pulse         *int      `json:"pulse,omitempty"`
	BloodSugar    *float64  `json:"blood_sugar,omitempty"`
	// nil (JSON null) = not assessed. 'alert', 'confused', 'restless', 'drowsy'.
	AlertnessLevel *string `json:"alertness_level"`
	Notes          string  `json:"notes,omitempty"`
	// The sugar may be the old form's pre-filled 6.4, not a measurement (issue #10).
	SuspectDefaultSugar bool `json:"suspect_default_sugar,omitempty"`
	// F.A.S.T. stroke check (vital_type 'fast', issue #13) and when she was last seen normal.
	FastResult   json.RawMessage `json:"fast_result,omitempty"`
	LastNormalAt string          `json:"last_normal_at,omitempty"` // RFC3339
	// Set on a corrected reading (issue #15): what was first entered, by whom, and why
	// it was corrected. The original row stays in the database unchanged.
	Correction  *VitalCorrection `json:"correction,omitempty"`
	BPStatus    string           `json:"bp_status,omitempty"`    // 'NORMAL', 'ELEVATED', 'STAGE_2', 'CRISIS'
	SugarStatus string           `json:"sugar_status,omitempty"` // 'LOW', 'NORMAL', 'ELEVATED', 'VERY_HIGH'
	// Issue #22: oxygen saturation (%) and temperature (C).
	SpO2         *int     `json:"spo2,omitempty"`
	TemperatureC *float64 `json:"temperature_c,omitempty"`
	SpO2Status   string   `json:"spo2_status,omitempty"` // 'NORMAL', 'LOW', 'CRITICAL'
	TempStatus   string   `json:"temp_status,omitempty"` // 'NORMAL', 'FEVER', 'LOW'
}

// VitalCorrection describes the reading a correction replaced.
type VitalCorrection struct {
	OriginalID           string   `json:"original_id"`
	OriginalSystolic     *int     `json:"original_systolic,omitempty"`
	OriginalDiastolic    *int     `json:"original_diastolic,omitempty"`
	OriginalPulse        *int     `json:"original_pulse,omitempty"`
	OriginalBloodSugar   *float64 `json:"original_blood_sugar,omitempty"`
	OriginalAlertness    *string  `json:"original_alertness_level,omitempty"`
	OriginalSpO2         *int     `json:"original_spo2,omitempty"`
	OriginalTemperatureC *float64 `json:"original_temperature_c,omitempty"`
	OriginalLoggedByName string   `json:"original_logged_by_name"`
	Reason               string   `json:"reason"`
	CorrectedAt          string   `json:"corrected_at"` // RFC3339
}

// HandoverNote captures shift transitions and urgent flags between siblings.
type HandoverNote struct {
	ID            string    `json:"id"`
	CaregiverID   string    `json:"caregiver_id"`
	CaregiverName string    `json:"caregiver_name,omitempty"`
	AvatarColor   string    `json:"avatar_color,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	ShiftType     string    `json:"shift_type"` // 'morning', 'evening', 'night' or a shift id (e.g. "shift_4h_1")
	Summary       string    `json:"summary"`
	UrgentFlag    bool      `json:"urgent_flag"`
	// Tags are fixed quick-tap observations (issue #18), e.g. "ate_half".
	Tags []string `json:"tags,omitempty"`
	// Set when this note passed duty to another caregiver.
	DutyHandoverID string `json:"duty_handover_id,omitempty"`
	PassedToID     string `json:"passed_to_id,omitempty"`
	PassedToName   string `json:"passed_to_name,omitempty"`
	AcceptedAt     string `json:"accepted_at,omitempty"` // RFC3339
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
	Type      string      `json:"type"` // "MED_LOGGED", "VITAL_LOGGED", "NOTE_ADDED", "CAREGIVER_SWITCHED", "CAREGIVERS_UPDATED", "MEDICATIONS_UPDATED", "INSULIN_LOGGED", "INSULIN_PLANS_UPDATED", "SHIFT_UPDATED", "DUTY_UPDATED"
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

// CareEvent records care tasks (repositioning, fluids, bowel, hygiene, physio).
type CareEvent struct {
	ID              string  `json:"id"`
	Kind            string  `json:"kind"`
	Value           string  `json:"value"`
	CaregiverID     string  `json:"caregiver_id"`
	CaregiverName   string  `json:"caregiver_name,omitempty"`
	AvatarColor     string  `json:"avatar_color,omitempty"`
	SkinCondition   string  `json:"skin_condition,omitempty"`
	Notes           string  `json:"notes,omitempty"`
	CreatedAt       string  `json:"created_at"`
	ClientRequestID *string `json:"client_request_id,omitempty"`
}

// CareStatus conveys real-time status of a care task category (e.g. repositioning).
type CareStatus struct {
	Kind              string     `json:"kind"`
	LastAt            *string    `json:"last_at"`
	LastValue         *string    `json:"last_value"`
	LastCaregiverID   *string    `json:"last_caregiver_id,omitempty"`
	LastCaregiverName *string    `json:"last_caregiver_name,omitempty"`
	LastAvatarColor   *string    `json:"last_avatar_color,omitempty"`
	LastSkinCondition *string    `json:"last_skin_condition,omitempty"`
	IntervalMinutes   int        `json:"interval_minutes"`
	NextDueAt         *string    `json:"next_due_at"`
	MinutesSince      *int       `json:"minutes_since"`
	MinutesUntilDue   *int       `json:"minutes_until_due"`
	IsOverdueWarning  bool       `json:"is_overdue_warning"`
	IsOverdueCritical bool       `json:"is_overdue_critical"`
	OnDutyCaregiver   *Caregiver `json:"on_duty_caregiver,omitempty"`
}

