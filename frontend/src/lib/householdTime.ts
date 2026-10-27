/**
 * Household time (issue #58). "Today", "overdue" and every clinical time are the
 * household's (APP_TIMEZONE on the server, sent by /health), never the phone's own
 * clock: a sibling helping from London at 23:30 must see home's 01:30 and home's
 * date. The zone is kept on this phone so the next open knows it at once.
 */

const STORAGE_KEY = "birrcare_household_tz";

function validZone(tz: string): boolean {
  try {
    new Intl.DateTimeFormat("en-US", { timeZone: tz });
    return true;
  } catch {
    return false;
  }
}

let zone: string | null = (() => {
  try {
    const saved = typeof localStorage !== "undefined" ? localStorage.getItem(STORAGE_KEY) : null;
    return saved && validZone(saved) ? saved : null;
  } catch {
    return null;
  }
})();

export function deviceZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

/** The household zone, or this phone's until the server has said. */
export function getHouseholdZone(): string {
  return zone || deviceZone();
}

/** Set from /health. Returns false for an unknown zone name. */
export function setHouseholdZone(tz: string | null | undefined): boolean {
  if (!tz || !validZone(tz)) return false;
  zone = tz;
  try {
    localStorage.setItem(STORAGE_KEY, tz);
  } catch {
    // private mode / blocked storage: fine, it is fetched again next time
  }
  return true;
}

function parts(d: Date, tz: string): Record<string, number> {
  const out: Record<string, number> = {};
  for (const p of new Intl.DateTimeFormat("en-US", {
    timeZone: tz,
    hourCycle: "h23",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).formatToParts(d)) {
    if (p.type !== "literal") out[p.type] = Number(p.value);
  }
  if (out.hour === 24) out.hour = 0;
  return out;
}

/** The household calendar date of a moment, as 'YYYY-MM-DD'. */
export function householdDateString(d: Date = new Date(), tz: string = getHouseholdZone()): string {
  const p = parts(d, tz);
  return `${p.year}-${String(p.month).padStart(2, "0")}-${String(p.day).padStart(2, "0")}`;
}

/** Today at home. */
export function householdToday(now: Date = new Date(), tz: string = getHouseholdZone()): string {
  return householdDateString(now, tz);
}

/** The household wall clock of a moment. */
export function householdClock(d: Date = new Date(), tz: string = getHouseholdZone()): { hours: number; minutes: number } {
  const p = parts(d, tz);
  return { hours: p.hour, minutes: p.minute };
}

/** Minutes east of UTC for a zone at a moment (Riyadh = 180, London in winter = 0). */
export function zoneOffsetMinutes(tz: string, at: Date = new Date()): number {
  const p = parts(at, tz);
  const asUTC = Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second);
  return Math.round((asUTC - Math.floor(at.getTime() / 1000) * 1000) / 60000);
}

/** The moment a household date and "HH:MM" refer to. */
export function householdDateTime(date: string, hhmm: string, tz: string = getHouseholdZone()): Date | null {
  const [y, mo, d] = date.split("-").map(Number);
  const [h, mi] = hhmm.split(":").map(Number);
  if ([y, mo, d, h, mi].some((n) => Number.isNaN(n))) return null;
  const wall = Date.UTC(y, mo - 1, d, h, mi);
  let at = new Date(wall - zoneOffsetMinutes(tz, new Date(wall)) * 60000);
  at = new Date(wall - zoneOffsetMinutes(tz, at) * 60000); // settle across a DST change
  return at;
}

/** Is this phone on a different clock from home right now? Offsets, not names: Bahrain and Riyadh are both UTC+3. */
export function clockDiffersFromHome(at: Date = new Date(), home: string = getHouseholdZone(), device: string = deviceZone()): boolean {
  return zoneOffsetMinutes(home, at) !== zoneOffsetMinutes(device, at);
}

/** "Asia/Riyadh" → "Riyadh". */
export function homeCity(tz: string = getHouseholdZone()): string {
  return (tz.split("/").pop() || tz).replace(/_/g, " ");
}
