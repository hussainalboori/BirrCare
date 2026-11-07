import { describe, expect, it } from "vitest";
import { orderedSlots } from "@/lib/medOrder";
import type { ScheduledDose } from "@/lib/types";

const dose = (slot: string, time?: string) => ({ slot, time }) as ScheduledDose;

describe("orderedSlots", () => {
  it("keeps clock order when the setting is off", () => {
    expect(orderedSlots([])).toEqual(["night", "morning", "afternoon", "evening", "bedtime"]);
  });

  it("starts from the shift", () => {
    expect(orderedSlots([], "00:00")).toEqual(["night", "morning", "afternoon", "evening", "bedtime"]);
    expect(orderedSlots([], "08:00")).toEqual(["morning", "afternoon", "evening", "bedtime", "night"]);
    expect(orderedSlots([], "20:00")).toEqual(["evening", "bedtime", "night", "morning", "afternoon"]);
  });

  it("wraps past midnight", () => {
    expect(orderedSlots([], "23:00")).toEqual(["night", "morning", "afternoon", "evening", "bedtime"]);
  });

  it("uses the doses' real times", () => {
    // Morning moved to 09:00: still after an 08:30 shift start.
    expect(orderedSlots([dose("morning", "09:00")], "08:30")[0]).toBe("morning");
    // Morning moved to 07:30: before an 08:00 start, so it goes last.
    expect(orderedSlots([dose("morning", "07:30")], "08:00").at(-1)).toBe("morning");
    // A slot with one dose after the start stays near the top.
    expect(orderedSlots([dose("morning", "07:30"), dose("morning", "09:00")], "08:00")[0]).toBe("morning");
  });

  it("never loses a slot", () => {
    expect(orderedSlots([dose("night")], "13:37").slice().sort()).toEqual(["afternoon", "bedtime", "evening", "morning", "night"]);
  });
});
