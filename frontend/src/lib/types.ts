export type AlertnessLevel = "alert" | "confused" | "restless" | "drowsy";
export type BPStatus = "LOW" | "NORMAL" | "ELEVATED" | "STAGE_2" | "CRISIS";
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
  /** This browser is marked as a shared tablet: sign-in ends after `idle_minutes` without use. */
  shared_device?: boolean;
  idle_minutes?: number;
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
  /** Anticoagulant / antiplatelet: listed first in the doctor report. */
  is_critical?: boolean;
  /** Pills/units in the box (issue #20); null = stock not tracked. */
  stock_units?: number | null;
  units_per_dose?: number;
  refill_threshold_days?: number;
  is_active: boolean;
  created_by?: string;
  created_at: string;
}

/** GET /medications/stock (issue #20). */
export interface StockStatus {
  medication_id: string;
  name_ar: string;
  name_en: string;
  dosage: string;
  stock_units: number;
  units_per_dose: number;
  doses_per_day: number;
  days_left: number | null;
  refill_threshold_days: number;
  low: boolean;
  blood_thinner: boolean;
}

export interface MedicationLog {
  id: string;
  medication_id: string;
  caregiver_id: string;
  slot: string;
  scheduled_date: string;
  taken_at?: string;
  /** partial = only part was taken; refused = she refused it (issue #9). */
  status: "taken" | "skipped" | "partial" | "refused";
  notes?: string;
  caregiver_name?: string;
  avatar_color?: string;
  /** Follow-ups such as "vomited 10 min later". */
  events?: MedicationLogEvent[];
  /** When it was tapped (the undo window counts from here). */
  recorded_at?: string;
  /** Set when the entry was cancelled (mis-tap); it never counts as given. */
  voided_at?: string;
  voided_by_name?: string;
  void_reason?: string;
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

export interface MedicationLogEvent {
  id: string;
  type: "vomited";
  minutes_after?: number | null;
  notes?: string;
  caregiver_name?: string;
  created_at: string;
}

export interface ScheduledDose {
  medication: Medication;
  slot: string;
  time?: string;
  log?: MedicationLog;
  /** Cancelled entries for this card, shown struck through. */
  voided_logs?: MedicationLog[];
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

export type VitalKind = "bp" | "sugar" | "alertness" | "full" | "fast" | "spo2" | "temp";

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
  /** null = not assessed (never defaulted to "alert"). */
  alertness_level: AlertnessLevel | null;
  /** The sugar may be the old form's pre-filled 6.4, not a measurement. */
  suspect_default_sugar?: boolean;
  /** F.A.S.T. stroke check (vital_type "fast"). */
  fast_result?: { face: boolean; arm: boolean; speech: boolean };
  /** When she was last seen normal (RFC3339), for the paramedics. */
  last_normal_at?: string;
  /** Issue #22: oxygen saturation (%) and temperature (°C). */
  spo2?: number;
  temperature_c?: number;
  spo2_status?: "NORMAL" | "LOW" | "CRITICAL";
  temp_status?: "NORMAL" | "FEVER" | "LOW";
  /** Set on a corrected reading: what was first entered (issue #15). */
  correction?: {
    original_id: string;
    original_systolic?: number;
    original_diastolic?: number;
    original_pulse?: number;
    original_blood_sugar?: number;
    original_alertness_level?: string;
    original_spo2?: number;
    original_temperature_c?: number;
    original_logged_by_name: string;
    reason: string;
    corrected_at: string;
  };
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
  /** Quick-tap tags (issue #18), keys from lib/handoverTags. */
  tags?: string[];
  /** Set when this note passed duty to a sibling. */
  duty_handover_id?: string;
  passed_to_id?: string;
  passed_to_name?: string;
  accepted_at?: string;
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
    /** Readings flagged as possibly the old pre-filled 6.4; left out of the average. */
    unverified_sugar_count?: number;
    min_spo2?: number;
    low_spo2_count?: number;
    critical_spo2_count?: number;
    max_temperature_c?: number;
    fever_count?: number;
  };
  adherence: {
    total_scheduled: number;
    total_taken: number;
    total_skipped: number;
    total_partial?: number;
    total_refused?: number;
    total_vomited?: number;
    total_not_recorded?: number;
    adherence_rate: number;
  };
  /** Per medication: given fully, partly, later vomited, refused, skipped. */
  /** Per medication against the doses the schedule expected; critical ones first. */
  per_medication?: {
    medication_id: string;
    name_ar: string;
    name_en: string;
    dosage: string;
    past_dosages?: string[];
    is_critical: boolean;
    expected: number;
    taken: number;
    partial: number;
    vomited: number;
    refused: number;
    skipped: number;
    not_recorded: number;
    rate: number;
    missed_dates: string[];
  }[];
  /** Blood pressure per household-local day. */
  daily_bp?: { date: string; avg_sys: number; avg_dia: number; max_sys: number; min_sys: number; readings: number }[];
  /** Lowest SpO2 and highest temperature per household-local day (issue #22). */
  daily_oxygen_temp?: { date: string; min_spo2?: number; max_temperature_c?: number; readings: number }[];
  alertness_distribution: Partial<Record<AlertnessLevel | "not_assessed", number>>;
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

export type TrustReason = "paired_device" | "geofence" | "lan_subnet" | "trusted_ip" | "untrusted";

export interface TrustStatus {
  trusted: boolean;
  reason: TrustReason;
  device_name?: string;
  client_ip: string;
}

export interface PairedDevice {
  id: string;
  device_name: string;
  created_at: string;
  last_used_at?: string;
  is_active: boolean;
}

export interface HomeLocation {
  latitude: number;
  longitude: number;
  radius_meters: number;
  updated_at?: string;
}
