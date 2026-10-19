export type AlertnessLevel = "alert" | "confused" | "restless" | "drowsy";
export type BPStatus = "NORMAL" | "ELEVATED" | "STAGE_2" | "CRISIS";
export type BloodSugarStatus = "LOW" | "NORMAL" | "ELEVATED" | "VERY_HIGH";
export type ShiftType = "morning" | "evening" | "night";

export interface Caregiver {
  id: string;
  name: string;
  email?: string;
  role: "admin" | "caregiver";
  avatar_color: string;
  preferred_lang: "ar" | "en";
  phone?: string;
  is_active: boolean;
  created_at: string;
}

export interface Medication {
  id: string;
  name_ar: string;
  name_en: string;
  dosage: string;
  instructions_ar?: string;
  instructions_en?: string;
  frequency_slots: string[];
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

export interface ScheduledDose {
  medication: Medication;
  slot: string;
  log?: MedicationLog;
}

export interface DailyScheduleResponse {
  date: string;
  schedule: ScheduledDose[];
}

export interface VitalsLog {
  id: string;
  caregiver_id: string;
  caregiver_name?: string;
  avatar_color?: string;
  recorded_at: string;
  systolic: number;
  diastolic: number;
  pulse: number;
  blood_sugar?: number;
  alertness_level: AlertnessLevel;
  notes?: string;
  bp_status: BPStatus;
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
  type: "MED_LOGGED" | "VITAL_LOGGED" | "NOTE_ADDED" | "CAREGIVER_SWITCHED";
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
}
