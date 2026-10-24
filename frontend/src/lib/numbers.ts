/**
 * Number utilities for BirrCare.
 * Handles normalization and parsing of Arabic-Indic (٠-٩), Extended Arabic-Indic (۰-۹),
 * Arabic decimal separator (٫), Arabic comma (،), and Western numbers.
 */

/**
 * Normalizes Eastern Arabic (Arabic-Indic U+0660–U+0669),
 * Extended Arabic-Indic (Persian/Urdu U+06F0–U+06F9),
 * Arabic decimal separator (U+066B), Arabic comma (U+060C),
 * and standard comma (,) to standard ASCII digits and decimal point.
 */
export function normalizeArabicNumerals(str: string): string {
  if (!str) return "";
  return str
    .replace(/[٠-٩]/g, (d) => String(d.charCodeAt(0) - 0x0660))
    .replace(/[۰-۹]/g, (d) => String(d.charCodeAt(0) - 0x06f0))
    .replace(/[٫،,]/g, ".");
}

/**
 * Parses a numeric string input from a form or input box that might contain
 * ASCII digits or Arabic digits/separators into a JavaScript number.
 * Returns NaN if empty, whitespace-only, or invalid.
 */
export function parseNumberInput(str: string | null | undefined): number {
  if (!str || typeof str !== "string" || !str.trim()) return NaN;
  const normalized = normalizeArabicNumerals(str.trim());
  return Number(normalized);
}

/**
 * Checks whether a string input can be parsed into a finite number.
 */
export function isValidNumber(str: string | null | undefined): boolean {
  const n = parseNumberInput(str);
  return Number.isFinite(n);
}
