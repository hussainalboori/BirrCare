import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { clockDiffersFromHome, householdClock, householdDateTime, householdToday, zoneOffsetMinutes } from "@/lib/householdTime";

// Issue #58: "today" and "overdue" follow the household's clock, not the phone's.

describe("household time", () => {
  // 23:30 in London (BST) is 01:30 the next day in Riyadh.
  const londonLateEvening = new Date("2026-07-10T22:30:00Z");

  it("a phone in London at 23:30 sees home's next date and home's 01:30", () => {
    expect(householdToday(londonLateEvening, "Europe/London")).toBe("2026-07-10");
    expect(householdToday(londonLateEvening, "Asia/Riyadh")).toBe("2026-07-11");
    expect(householdClock(londonLateEvening, "Asia/Riyadh")).toEqual({ hours: 1, minutes: 30 });
  });

  it("a home date and time is the right moment, wherever the phone is", () => {
    expect(householdDateTime("2026-07-11", "01:00", "Asia/Riyadh")?.toISOString()).toBe("2026-07-10T22:00:00.000Z");
    // So the 01:00 dose is 30 minutes late at London's 23:30.
    const late = londonLateEvening.getTime() - householdDateTime("2026-07-11", "01:00", "Asia/Riyadh")!.getTime();
    expect(late).toBe(30 * 60_000);
    // Across London's DST change the offset is still right.
    expect(householdDateTime("2026-10-25", "12:00", "Europe/London")?.toISOString()).toBe("2026-10-25T12:00:00.000Z");
  });

  it("compares offsets, not names", () => {
    expect(zoneOffsetMinutes("Asia/Riyadh", londonLateEvening)).toBe(180);
    expect(clockDiffersFromHome(londonLateEvening, "Asia/Riyadh", "Asia/Bahrain")).toBe(false);
    expect(clockDiffersFromHome(londonLateEvening, "Asia/Riyadh", "Europe/London")).toBe(true);
  });
});

// No clinical screen reads the phone's own date or clock.
describe("grep: no device clock in clinical code", () => {
  const root = join(__dirname, "..");
  const allowed = new Set(["lib/householdTime.ts", "lib/datetime.ts", "lib/theme.ts", "components/ThemePicker.tsx"]);
  const files: string[] = [];
  const walk = (dir: string) => {
    for (const f of readdirSync(dir)) {
      const p = join(dir, f);
      if (statSync(p).isDirectory()) {
        if (f !== "__tests__") walk(p);
      } else if (/\.tsx?$/.test(f)) files.push(p);
    }
  };
  walk(root);

  it("uses the household helpers instead of localDateString() and getHours()", () => {
    const offenders = files
      .map((p) => p.slice(root.length + 1))
      .filter((rel) => !allowed.has(rel))
      .filter((rel) => /localDateString\(|\.getHours\(\)/.test(readFileSync(join(root, rel), "utf8")));
    expect(offenders).toEqual([]);
  });
});
