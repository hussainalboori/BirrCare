import type { Locale } from "./i18n";

/**
 * Returns the device-local calendar date as 'YYYY-MM-DD'.
 *
 * Never use `new Date().toISOString().slice(0, 10)` for "today": that is the
 * UTC date, which in UTC+3 is still *yesterday* between 00:00 and 03:00 local
 * time, so doses would be shown/logged against the wrong scheduled_date.
 */
export function localDateString(d: Date = new Date()): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

/**
 * Parses timestamps coming from the Go backend. Accepts RFC3339 strings
 * (`2026-10-19T17:05:00Z`) and raw SQLite `datetime('now')` strings
 * (`2026-10-19 17:05:00`), which are UTC but carry no zone designator —
 * `new Date()` would treat those as *local* time (Chrome) or reject them
 * entirely (iOS Safari → Invalid Date).
 */
export function parseServerTime(value?: string | null): Date | null {
  if (!value) return null;
  let s = value.trim();
  if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}(:\d{2}(\.\d+)?)?$/.test(s)) {
    s = s.replace(" ", "T") + "Z";
  }
  const d = new Date(s);
  return isNaN(d.getTime()) ? null : d;
}

/**
 * BCP-47 tag for Intl formatting. Arabic is pinned to the Gregorian calendar
 * and Latin digits so dates match the YYYY-MM-DD schedule picker and the
 * numeric vitals (plain `ar-SA` defaults to the Hijri calendar).
 */
export function intlLocale(locale: Locale): string {
  return locale === "ar" ? "ar-SA-u-ca-gregory-nu-latn" : "en-US";
}

export function formatTime(value: string | null | undefined, locale: Locale): string {
  const d = parseServerTime(value);
  if (!d) return "";
  return d.toLocaleTimeString(intlLocale(locale), { hour: "numeric", minute: "2-digit", hour12: true });
}

export function formatShortDate(value: string | null | undefined, locale: Locale): string {
  const d = parseServerTime(value);
  if (!d) return "";
  return d.toLocaleDateString(intlLocale(locale), { month: "short", day: "numeric" });
}

export function formatDateTime(value: string | null | undefined, locale: Locale): string {
  const d = parseServerTime(value);
  if (!d) return "";
  return d.toLocaleString(intlLocale(locale), {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function arCount(n: number, one: string, two: string, few: string, many: string): string {
  if (n === 1) return one;
  if (n === 2) return two;
  return `${n} ${n >= 3 && n <= 10 ? few : many}`;
}

/** "4 h 15 min" / "4 ساعات و15 دقيقة" for a duration in milliseconds (rounded down to minutes). */
export function formatDuration(ms: number, locale: Locale): string {
  const total = Math.max(0, Math.floor(ms / 60000));
  const h = Math.floor(total / 60);
  const m = total % 60;
  if (locale === "ar") {
    const hours = h ? arCount(h, "ساعة", "ساعتين", "ساعات", "ساعة") : "";
    const mins = m || !h ? arCount(m, "دقيقة", "دقيقتين", "دقائق", "دقيقة") : "";
    if (!h && m === 0) return "أقل من دقيقة";
    return hours && mins ? `${hours} و${mins}` : hours || mins;
  }
  if (!h && m === 0) return "less than a minute";
  return [h ? `${h} h` : "", m ? `${m} min` : ""].filter(Boolean).join(" ");
}

/** "Last given 4 h 15 min ago by Fatima" for the interval lock (issue #7). */
export function lastGivenText(takenAt: string | null | undefined, by: string | undefined, now: Date, locale: Locale): string {
  const d = parseServerTime(takenAt);
  if (!d) return "";
  const ago = formatDuration(now.getTime() - d.getTime(), locale);
  if (locale === "ar") return `آخر جرعة قبل ${ago}${by ? ` بواسطة ${by}` : ""}`;
  return `Last given ${ago} ago${by ? ` by ${by}` : ""}`;
}
