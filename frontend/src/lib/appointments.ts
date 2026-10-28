/**
 * Appointments and questions for the doctor (issue #63). Times come from the
 * server as UTC; the date and HH:MM are the household's (APP_TIMEZONE), so a
 * sibling abroad sees home's appointment time.
 */
import type { Locale } from "./i18n";
import { intlLocale } from "./datetime";

export type AppointmentKind = "doctor" | "lab" | "physio" | "other";

export interface Appointment {
  id: string;
  kind: AppointmentKind;
  title: string;
  starts_at: string;
  date: string; // household YYYY-MM-DD
  time: string; // household HH:MM
  location?: string;
  notes?: string;
  assigned_caregiver_id?: string;
  assigned_name?: string;
  status: "scheduled" | "done" | "cancelled";
  created_by_name?: string;
  created_at: string;
  updated_at: string;
}

export interface AppointmentList {
  upcoming: Appointment[];
  recent: Appointment[];
}

export interface DoctorQuestion {
  id: string;
  text: string;
  by?: string;
  created_by?: string;
  created_at: string;
  answered_at?: string;
  answered_by_name?: string;
  answer?: string;
  appointment_id?: string;
}

export interface QuestionList {
  open: DoctorQuestion[];
  answered: DoctorQuestion[];
}

export const APPOINTMENT_KINDS: { key: AppointmentKind; en: string; ar: string }[] = [
  { key: "doctor", en: "Doctor", ar: "طبيب" },
  { key: "lab", en: "Blood test", ar: "تحليل" },
  { key: "physio", en: "Physio", ar: "علاج طبيعي" },
  { key: "other", en: "Other", ar: "أخرى" },
];

export function kindLabel(kind: string, ar: boolean): string {
  const k = APPOINTMENT_KINDS.find((x) => x.key === kind);
  return k ? (ar ? k.ar : k.en) : kind;
}

/** "Sun 2 Nov" for a household date, in the app's language (Gregorian, Latin digits). */
export function appointmentDay(date: string, locale: Locale): string {
  const d = new Date(`${date}T12:00:00Z`);
  if (Number.isNaN(d.getTime())) return date;
  return d.toLocaleDateString(intlLocale(locale), { weekday: "short", day: "numeric", month: "short", timeZone: "UTC" });
}

/** The .ics file for "Add to my calendar" (served through the API proxy). */
export function icsHref(id: string): string {
  return `/api/v1/appointments/${encodeURIComponent(id)}/ics`;
}

export const APPOINTMENTS_CHANGED = new Set(["APPOINTMENT_CHANGED", "APPOINTMENT_REMINDER", "DOCTOR_QUESTION_CHANGED"]);
