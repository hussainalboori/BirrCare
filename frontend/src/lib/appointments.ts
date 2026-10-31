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

/** "2026-11" → "2026-12" (step = 1) or "2026-10" (step = -1). */
export function shiftMonth(month: string, step: number): string {
  const [y, m] = month.split("-").map(Number);
  const d = new Date(Date.UTC(y, m - 1 + step, 1));
  return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, "0")}`;
}

/** The month's days as YYYY-MM-DD in weeks starting on Sunday; null pads the first and last week. */
export function monthGrid(month: string): (string | null)[] {
  const [y, m] = month.split("-").map(Number);
  const first = new Date(Date.UTC(y, m - 1, 1)).getUTCDay();
  const days = new Date(Date.UTC(y, m, 0)).getUTCDate();
  const cells: (string | null)[] = Array.from({ length: first }, () => null);
  for (let d = 1; d <= days; d++) cells.push(`${month}-${String(d).padStart(2, "0")}`);
  while (cells.length % 7) cells.push(null);
  return cells;
}

/** "November 2026" in the app's language (Gregorian, Latin digits). */
export function monthLabel(month: string, locale: Locale): string {
  return new Date(`${month}-01T12:00:00Z`).toLocaleDateString(intlLocale(locale), { month: "long", year: "numeric", timeZone: "UTC" });
}

/** Weekday names for the calendar header, Sunday first ("Sun"; one letter in Arabic, as Arabic calendars do: the full names don't fit a phone). */
export function weekdayNames(locale: Locale): string[] {
  // 2023-01-01 was a Sunday.
  return Array.from({ length: 7 }, (_, i) =>
    new Date(Date.UTC(2023, 0, 1 + i, 12)).toLocaleDateString(intlLocale(locale), { weekday: locale === "ar" ? "narrow" : "short", timeZone: "UTC" }),
  );
}
