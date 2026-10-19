import { describe, expect, it } from "vitest";
import { localDateString, parseServerTime } from "@/lib/datetime";

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
