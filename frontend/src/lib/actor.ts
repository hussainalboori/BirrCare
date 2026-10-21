/** Issue #17: every write button names who it will be recorded under. */

export function firstName(name?: string | null): string {
  return (name || "").trim().split(/\s+/)[0] || "";
}

/** "Give as Hussain" / "أعطِ باسم حسين"; falls back to `fallback` with no one signed in. */
export function giveAs(name: string | undefined | null, ar: boolean, fallback: string): string {
  const n = firstName(name);
  if (!n) return fallback;
  return ar ? `أعطِ باسم ${n}` : `Give as ${n}`;
}

/** "Save as Hussain" / "احفظ باسم حسين". */
export function saveAs(name: string | undefined | null, ar: boolean, fallback: string): string {
  const n = firstName(name);
  if (!n) return fallback;
  return ar ? `احفظ باسم ${n}` : `Save as ${n}`;
}
