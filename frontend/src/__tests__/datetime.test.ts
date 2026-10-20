import { describe, expect, it } from "vitest";
import { localDateString, parseServerTime, formatDuration, lastGivenText } from "@/lib/datetime";

describe("localDateString", () => {
  it("uses the local calendar date, not the UTC date", () => {
    // 00:30 local on Oct 20 — in UTC+3 toISOString() would still say Oct 19.
    const d = new Date(2026, 9, 20, 0, 30);
    expect(localDateString(d)).toBe("2026-10-20");
  });
});

describe("parseServerTime", () => {
  it("treats SQLite datetime('now') strings as UTC", () => {
    expect(parseServerTime("2026-10-19 17:05:00")?.toISOString()).toBe("2026-10-19T17:05:00.000Z");
  });
  it("parses RFC3339", () => {
    expect(parseServerTime("2026-10-19T17:05:00Z")?.toISOString()).toBe("2026-10-19T17:05:00.000Z");
  });
  it("returns null for empty/invalid input", () => {
    expect(parseServerTime("")).toBeNull();
    expect(parseServerTime(undefined)).toBeNull();
    expect(parseServerTime("not a date")).toBeNull();
  });
});

describe("interval lock text (issue #7)", () => {
  // Bedtime dose at 23:30 local, viewed at 03:45 the next day (across midnight).
  const given = new Date(2026, 9, 19, 23, 30);
  const now = new Date(2026, 9, 20, 3, 45);

  it("says how long ago and by whom, in English", () => {
    expect(lastGivenText(given.toISOString(), "Fatima", now, "en")).toBe("Last given 4 h 15 min ago by Fatima");
  });

  it("says how long ago and by whom, in Arabic", () => {
    expect(lastGivenText(given.toISOString(), "فاطمة", now, "ar")).toBe("آخر جرعة قبل 4 ساعات و15 دقيقة بواسطة فاطمة");
  });

  it("accepts raw SQLite UTC timestamps", () => {
    const sqlite = given.toISOString().replace("T", " ").slice(0, 19);
    expect(lastGivenText(sqlite, "Fatima", now, "en")).toBe("Last given 4 h 15 min ago by Fatima");
  });

  it("uses Arabic dual and plural forms", () => {
    expect(formatDuration(60 * 60000, "ar")).toBe("ساعة");
    expect(formatDuration(2 * 60 * 60000 + 2 * 60000, "ar")).toBe("ساعتين ودقيقتين");
    expect(formatDuration(18 * 60 * 60000, "ar")).toBe("18 ساعة");
    expect(formatDuration(30 * 1000, "en")).toBe("less than a minute");
    expect(formatDuration(9 * 60 * 60000, "en")).toBe("9 h");
  });

  it("returns empty text without a timestamp", () => {
    expect(lastGivenText(undefined, "Fatima", now, "en")).toBe("");
  });
});
