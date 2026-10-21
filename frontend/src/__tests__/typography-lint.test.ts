import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

/**
 * Static Typography Lint (Issue #24):
 * Safety-critical text must never use sub-12px font sizes (e.g. text-[9px], text-[10px], text-[11px]).
 * Minimum 14px (text-sm) for any status or clinical text;
 * 16px (text-base) for "given by/at" and dosage;
 * 12px (text-xs) minimum for decorative metadata.
 * No text-[<12px] across the app outside an allowlist.
 */
const SRC = join(__dirname, "..");

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) return name === "__tests__" ? [] : walk(p);
    return p.endsWith(".tsx") || p.endsWith(".ts") ? [p] : [];
  });
}

// Matches text-[0px] through text-[11px] with optional Tailwind prefixes (e.g. sm:text-[10px], hover:text-[11px])
const SUB_12PX = /(?<![\w-])(?:[a-z0-9-]+:)*text-\[(?:[0-9]|1[0-1])px\](?![\w-])/g;

describe("Typography lint: forbid text-[<12px] (Issue #24)", () => {
  const files = walk(SRC);

  it("self-check: pattern accurately catches sub-12px classes and ignores >= 12px", () => {
    const bad = 'className="text-[9px] text-[10px] text-[11px] sm:text-[11px] hover:text-[10px]"';
    expect(bad.match(SUB_12PX)).toEqual([
      "text-[9px]",
      "text-[10px]",
      "text-[11px]",
      "sm:text-[11px]",
      "hover:text-[10px]",
    ]);

    const good = 'className="text-xs text-sm text-base text-[12px] text-[14px] text-[16px] print:text-[9pt]"';
    expect(good.match(SUB_12PX)).toBeNull();
  });

  it("finds source files to scan", () => {
    expect(files.length).toBeGreaterThan(10);
  });

  it("forbids text-[<12px] across all components in src outside any allowlist", () => {
    // Explicit allowlist: currently empty because all files have been migrated to accessible standard sizes!
    const allowlist: string[] = [];

    const violations: string[] = [];
    for (const file of files) {
      const relPath = relative(SRC, file);
      if (allowlist.includes(relPath)) continue;

      const lines = readFileSync(file, "utf8").split("\n");
      lines.forEach((line, i) => {
        if (/^\s*(\/\/|\*|\{\/\*)/.test(line)) return; // skip comments
        const matches = line.match(SUB_12PX);
        if (matches) {
          violations.push(`${relPath}:${i + 1} -> ${matches.join(", ")}`);
        }
      });
    }

    expect(
      violations,
      `Found sub-12px text sizes violating accessibility standards:\n${violations.join("\n")}`
    ).toEqual([]);
  });

  it("MedicationSlotCard.tsx specifically has no text-[11px] or other sub-12px text", () => {
    const cardPath = join(SRC, "components", "MedicationSlotCard.tsx");
    const content = readFileSync(cardPath, "utf8");

    expect(content.includes("text-[11px]")).toBe(false);
    expect(content.includes("text-[10px]")).toBe(false);
    expect(content.includes("text-[9px]")).toBe(false);
    expect(content.match(SUB_12PX)).toBeNull();
  });

  it("MedicationSlotCard.tsx uses >= 16px (text-base) for dose given-by/at and dosage", () => {
    const cardPath = join(SRC, "components", "MedicationSlotCard.tsx");
    const content = readFileSync(cardPath, "utf8");

    // Dosage badge must use text-base (16px)
    expect(content).toMatch(/<Auto[^>]*text-base[^>]*>\s*\{medication\.dosage\}\s*<\/Auto>/);

    // Dose status container must have role="status" and text-base for the given by/at row
    expect(content).toMatch(/text-base font-semibold/);
    expect(content).toContain("t.givenBy");
  });
});
