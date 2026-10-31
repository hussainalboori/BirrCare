import { describe, expect, it } from "vitest";
import { monthGrid, monthLabel, shiftMonth, weekdayNames } from "@/lib/appointments";

describe("appointment calendar helpers", () => {
  it("lays a month out in Sunday-first weeks", () => {
    const g = monthGrid("2026-11"); // 1 Nov 2026 is a Sunday
    expect(g[0]).toBe("2026-11-01");
    expect(g.filter(Boolean)).toHaveLength(30);
    expect(g.length % 7).toBe(0);
    const feb = monthGrid("2026-02"); // 1 Feb 2026 is a Sunday, 28 days: exactly 4 weeks
    expect(feb).toHaveLength(28);
    const oct = monthGrid("2026-10"); // 1 Oct 2026 is a Thursday
    expect(oct.slice(0, 5)).toEqual([null, null, null, null, "2026-10-01"]);
    expect(oct.filter(Boolean).at(-1)).toBe("2026-10-31");
  });

  it("moves across years", () => {
    expect(shiftMonth("2026-12", 1)).toBe("2027-01");
    expect(shiftMonth("2026-01", -1)).toBe("2025-12");
  });

  it("names months and weekdays in both languages, with Latin digits", () => {
    expect(monthLabel("2026-11", "en")).toBe("November 2026");
    expect(monthLabel("2026-11", "ar")).toContain("2026");
    expect(weekdayNames("en")[0]).toBe("Sun");
    expect(weekdayNames("ar")).toHaveLength(7);
    expect(weekdayNames("ar").every((n) => n.length <= 2)).toBe(true); // fits seven columns on a phone
  });
});
