export type AlertnessLevel = "alert" | "confused" | "restless" | "drowsy";
export type BPStatus = "NORMAL" | "ELEVATED" | "STAGE_2" | "CRISIS";
export type BloodSugarStatus = "LOW" | "NORMAL" | "ELEVATED" | "VERY_HIGH";
export type ShiftType = "morning" | "evening" | "night" | string;

export interface Shift {
  id: string;
  name_ar: string;
  name_en: string;
  start_time: string;
  end_time: string;
  duration_hours: number;
  color: string;
  display_order: number;
  is_active: boolean;
  created_at?: string;
}

export interface ShiftAssignment {
  id: string;
  shift_id: string;
  caregiver_id: string;
  date: string;
  notes?: string;
  created_at?: string;
  caregiver_name?: string;
  avatar_color?: string;
  caregiver_role?: CaregiverRole;
  shift_name_ar?: string;
  shift_name_en?: string;
  shift_start_time?: string;
  shift_end_time?: string;
  shift_color?: string;
}

export interface CurrentDutyStatus {
  active_shift?: Shift;
  active_assignment?: ShiftAssignment;
  active_caregiver?: Caregiver;
  is_on_duty: boolean;
  current_time: string;
  date: string;
}

export type ShiftPreset = "24h" | "4h" | "8h" | "12h";

export type CaregiverRole = "super_admin" | "admin" | "caregiver" | "viewer";

export interface Caregiver {
  id: string;
  name: string;
  email?: string;
  role: CaregiverRole;
  avatar_color: string;
  preferred_lang: "ar" | "en";
  phone?: string;
  is_active: boolean;
  created_at: string;
  /** Sign-in needs the PIN and an authenticator-app code. */
  totp_enabled?: boolean;
}

/** Public PIN switcher entry (GET /caregivers): no email, role or phone. */
export type CaregiverOption = Pick<Caregiver, "id" | "name" | "avatar_color">;

export interface Medication {
  id: string;
  name_ar: string;
  name_en: string;
  dosage: string;
  instructions_ar?: string;
  instructions_en?: string;
  frequency_slots: string[];
  days_of_week?: string[];
  slot_times?: Record<string, string>;
  image_url?: string;
  /** Doctor's minimum hours between doses; null = automatic, 0 = no lock. */
  min_hours_between?: number | null;
  is_active: boolean;
  created_by?: string;
  created_at: string;
}

export interface MedicationLog {
  id: string;
  medication_id: string;
  caregiver_id: string;
  slot: string;
  scheduled_date: string;
  taken_at?: string;
  status: "taken" | "skipped";
  notes?: string;
  caregiver_name?: string;
  avatar_color?: string;
}

/** Most recent taken dose of a medication, on any date. */
export interface LastDose {
  taken_at: string;
  slot: string;
  scheduled_date: string;
  caregiver_name: string;
  avatar_color?: string;
  hours_since: number;
}

export interface ScheduledDose {
  medication: Medication;
  slot: string;
  time?: string;
  log?: MedicationLog;
  /** Interval lock (cross-midnight double doses): independent of the date shown. */
  last_taken?: LastDose;
  interval_hours?: number;
  /** RFC3339; present while the medication is still inside its minimum interval. */
  next_allowed_at?: string;
}

export interface DailyScheduleResponse {
  date: string;
  schedule: ScheduledDose[];
}

export type VitalKind = "bp" | "sugar" | "alertness" | "full";

export interface VitalsLog {
  id: string;
  caregiver_id: string;
  caregiver_name?: string;
  avatar_color?: string;
  recorded_at: string;
  vital_type?: VitalKind;
  systolic?: number;
  diastolic?: number;
  pulse?: number;
  blood_sugar?: number;
  alertness_level: AlertnessLevel;
  notes?: string;
  bp_status?: BPStatus;
  sugar_status?: BloodSugarStatus;
}

export interface HandoverNote {
  id: string;
  caregiver_id: string;
  caregiver_name?: string;
  avatar_color?: string;
  created_at: string;
  shift_type: ShiftType;
  summary: string;
  urgent_flag: boolean;
}

export interface SSEEvent<T = any> {
  id: string;
  type: "MED_LOGGED" | "VITAL_LOGGED" | "NOTE_ADDED" | "CAREGIVER_SWITCHED" | "SHIFT_UPDATED" | "DUTY_UPDATED" | string;
  timestamp: string;
  actor: {
    id: string;
    name: string;
    avatar_color: string;
  };
  payload: T;
}

export interface DoctorSummary {
  generated_at: string;
  days_covered: number;
  start_date: string;
  end_date: string;
  vitals_stats: {
    total_readings: number;
    avg_systolic: number;
    max_systolic: number;
    min_systolic: number;
    avg_diastolic: number;
    max_diastolic: number;
    min_diastolic: number;
    avg_pulse: number;
    crisis_count: number;
    elevated_count: number;
    avg_blood_sugar?: number;
  };
  adherence: {
    total_scheduled: number;
    total_taken: number;
    total_skipped: number;
    adherence_rate: number;
  };
  alertness_distribution: Record<AlertnessLevel, number>;
  recent_notes: HandoverNote[];
  recent_vitals: VitalsLog[];
  insulin_logs?: InsulinLog[];
}

/** One row of the doctor's correction scale: min inclusive, max exclusive, mmol/L. */
export interface InsulinScaleRow {
  min: number;
  max: number;
  units: number;
}

export interface InsulinPlan {
  id: string;
  name_ar: string;
  name_en: string;
  kind: "scale" | "fixed";
  fixed_units?: number;
  scale: InsulinScaleRow[];
  min_hours_between: number;
  prescribed_by?: string;
  notes_ar?: string;
  notes_en?: string;
  is_active: boolean;
  updated_at?: string;
}

export interface InsulinLog {
  id: string;
  plan_id: string;
  plan_name_ar?: string;
  plan_name_en?: string;
  caregiver_id: string;
  caregiver_name: string;
  avatar_color?: string;
  given_at: string;
  status: "given" | "held";
  units: number;
  scale_units: number | null;
  blood_sugar: number | null;
  reason?: string;
}

export interface InsulinPlanStatus {
  plan: InsulinPlan;
  last_given: InsulinLog | null;
  next_allowed_at?: string;
  recent: InsulinLog[];
}
