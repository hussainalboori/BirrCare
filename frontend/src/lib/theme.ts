/**
 * Issue #21: screen colours. Auto / Light / Dark / Night, kept in a cookie so the
 * server renders the right class in the first paint (like the language cookie).
 * Auto = Night from 21:00 to 07:00 on this phone's clock, otherwise the system
 * light/dark setting.
 */
export type ThemeMode = "auto" | "light" | "dark" | "night";
export type ResolvedTheme = "light" | "dark" | "night";

export const THEME_COOKIE = "birrcare_theme";
export const THEME_MODES: ThemeMode[] = ["auto", "light", "dark", "night"];
export const NIGHT_FROM_HOUR = 21;
export const NIGHT_UNTIL_HOUR = 7;

export const THEME_COLORS: Record<ResolvedTheme, string> = { light: "#059669", dark: "#020617", night: "#000000" };

export function parseThemeMode(v: string | undefined | null): ThemeMode {
  return THEME_MODES.includes(v as ThemeMode) ? (v as ThemeMode) : "auto";
}

export function resolveTheme(mode: ThemeMode, hour: number, systemDark: boolean): ResolvedTheme {
  if (mode !== "auto") return mode;
  if (hour >= NIGHT_FROM_HOUR || hour < NIGHT_UNTIL_HOUR) return "night";
  return systemDark ? "dark" : "light";
}

/** Classes on <html> for a resolved theme: Night is Dark plus the night palette. */
export function themeClasses(t: ResolvedTheme): string {
  return t === "night" ? "dark night" : t === "dark" ? "dark" : "";
}

/**
 * Runs in <head> before the first paint (inline, so no white flash at 2 a.m.).
 * Must match resolveTheme; themeScript.test.ts runs it against the same cases.
 */
export const THEME_SCRIPT = `(function(){try{
var m=(document.cookie.match(/(?:^|; )${THEME_COOKIE}=([^;]*)/)||[])[1]||"auto";
if(["auto","light","dark","night"].indexOf(m)<0)m="auto";
var h=new Date().getHours(),t=m;
if(m==="auto"){t=(h>=${NIGHT_FROM_HOUR}||h<${NIGHT_UNTIL_HOUR})?"night":(window.matchMedia&&matchMedia("(prefers-color-scheme: dark)").matches?"dark":"light");}
var c=document.documentElement.classList;c.toggle("dark",t!=="light");c.toggle("night",t==="night");
var meta=document.querySelector('meta[name="theme-color"]');if(meta)meta.setAttribute("content",${JSON.stringify(THEME_COLORS)}[t]);
}catch(e){}})();`;

export function applyTheme(t: ResolvedTheme) {
  const c = document.documentElement.classList;
  c.toggle("dark", t !== "light");
  c.toggle("night", t === "night");
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", THEME_COLORS[t]);
}

export function isNight(): boolean {
  return typeof document !== "undefined" && document.documentElement.classList.contains("night");
}

/**
 * Vibrate, gently at night. Crisis patterns are never shortened.
 */
export function haptic(pattern: number | number[], opts: { crisis?: boolean } = {}) {
  if (typeof navigator === "undefined" || !("vibrate" in navigator)) return;
  if (isNight() && !opts.crisis) {
    pattern = Array.isArray(pattern) ? pattern.map((n, i) => (i % 2 === 0 ? Math.max(10, Math.round(n / 3)) : n)) : Math.max(10, Math.round(pattern / 3));
  }
  navigator.vibrate?.(pattern);
}
