/** Issue #20: "about 3 days left" / "يكفي نحو 3 أيام", with Arabic counting rules. */
export function daysLeftText(daysLeft: number | null | undefined, ar: boolean): string {
  const d = Math.floor(Number(daysLeft) || 0);
  if (d < 1) return ar ? "نفد أو سينفد اليوم" : "out or runs out today";
  if (!ar) return `about ${d} day${d === 1 ? "" : "s"} left`;
  if (d === 1) return "يكفي يوماً واحداً تقريباً";
  if (d === 2) return "يكفي نحو يومين";
  return `يكفي نحو ${d} ${d <= 10 ? "أيام" : "يوماً"}`;
}

/** Issue #65: "by tomorrow 12:00" / "by Tue 12:00", in home time. */
export function dueText(dueBy: string | undefined, ar: boolean, now: Date, tz: string): string {
  if (!dueBy) return "";
  const due = new Date(dueBy);
  if (Number.isNaN(due.getTime())) return "";
  const day = (d: Date) => d.toLocaleDateString("en-CA", { timeZone: tz });
  const time = due.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", hour12: false, timeZone: tz });
  const tomorrow = new Date(now.getTime() + 86_400_000);
  let when: string;
  if (day(due) === day(now)) when = ar ? "اليوم" : "today";
  else if (day(due) === day(tomorrow)) when = ar ? "غداً" : "tomorrow";
  else when = due.toLocaleDateString(ar ? "ar-SA-u-ca-gregory-nu-latn" : "en-US", { weekday: "short", timeZone: tz });
  return ar ? `قبل ${when} ${time}` : `by ${when} ${time}`;
}

/** The window event that opens the "new box" keypad for a medicine (from an alert). */
export const OPEN_REFILL_EVENT = "birrcare:open_refill";
