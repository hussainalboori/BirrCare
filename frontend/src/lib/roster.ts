/**
 * Night-shift fairness and rest (issue #66). Worded as appreciation, never a
 * scoreboard; the warnings advise and never block.
 */

export interface CaregiverLoad {
  caregiver_id: string;
  name: string;
  avatar_color: string;
  hours_on_duty: number;
  night_hours: number;
  nights: number;
  consecutive_nights: number;
  last_night_off?: string;
  night_alerts_answered: number;
}

export interface ShiftSummary {
  from: string;
  to: string;
  days: number;
  caregivers: CaregiverLoad[];
}

export interface Unavailability {
  id: string;
  caregiver_id: string;
  name: string;
  start_date: string;
  end_date: string;
  reason?: string;
}

export interface AssignWarning {
  warning?: "consecutive_nights" | "unavailable";
  warnings?: string[];
  consecutive_nights?: number;
  unavailable_reason?: string;
}

/** "3 nights" / "3 ليالٍ", with Arabic counting. */
export function nightsText(n: number, ar: boolean): string {
  if (!ar) return `${n} night${n === 1 ? "" : "s"}`;
  if (n === 0) return "لا ليالٍ";
  if (n === 1) return "ليلة واحدة";
  if (n === 2) return "ليلتان";
  return n <= 10 ? `${n} ليالٍ` : `${n} ليلة`;
}

/** The Super Admin's amber hint after assigning (or passing) a shift. */
export function assignWarningText(w: AssignWarning, name: string, ar: boolean): string[] {
  const out: string[] = [];
  for (const k of w.warnings ?? (w.warning ? [w.warning] : [])) {
    if (k === "consecutive_nights" && w.consecutive_nights) {
      const before = w.consecutive_nights - 1;
      out.push(
        ar
          ? `سهرات ${name}: ${nightsText(before, true)} متتالية — هل تختار شخصاً آخر لهذه الليلة؟ الراحة مهمة.`
          : `${name} has done ${nightsText(before, false)} in a row — assign someone else so they can rest?`,
      );
    }
    if (k === "unavailable") {
      const why = w.unavailable_reason ? ` (${w.unavailable_reason})` : "";
      // Arabic kept gender-neutral: "لدى X ظرف" (X has something on) rather than a verb.
      out.push(ar ? `لدى ${name} ظرف هذا اليوم${why} — هل تختار شخصاً آخر؟` : `${name} said they're away that day${why} — assign someone else?`);
    }
  }
  return out;
}
