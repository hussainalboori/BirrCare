import { describe, expect, it } from "vitest";
import { classifyBloodPressure } from "@/lib/clinical";

// AGENTS.md §4.3 — the higher category of systolic OR diastolic wins.
describe("classifyBloodPressure (AGENTS.md §4.3 boundaries)", () => {
  const cases: Array<[number, number, string]> = [
    // Normal: < 120 / < 80
    [119, 79, "NORMAL"],
    [90, 60, "NORMAL"],
    // Elevated: 120–139 or 80–89
    [120, 79, "ELEVATED"],
    [119, 80, "ELEVATED"],
    [139, 89, "ELEVATED"],
    [120, 80, "ELEVATED"],
    // Stage 2: 140–179 or 90–119
    [140, 70, "STAGE_2"],
    [130, 90, "STAGE_2"],
    [179, 119, "STAGE_2"],
    [139, 90, "STAGE_2"],
    [140, 89, "STAGE_2"],
    // Crisis: ≥ 180 or ≥ 120
    [180, 70, "CRISIS"],
    [110, 120, "CRISIS"],
    [179, 120, "CRISIS"],
    [180, 119, "CRISIS"],
    [220, 130, "CRISIS"],
  ];

  it.each(cases)("%i/%i mmHg → %s", (sys, dia, expected) => {
    expect(classifyBloodPressure(sys, dia).status).toBe(expected);
  });

  it("crisis badge blinks (animate-pulse) and other categories do not", () => {
    expect(classifyBloodPressure(185, 95).badgeClass).toContain("animate-pulse");
    expect(classifyBloodPressure(150, 95).badgeClass).not.toContain("animate-pulse");
  });

  it("uses distinct colours: green / yellow / orange / red", () => {
    expect(classifyBloodPressure(110, 70).badgeClass).toContain("emerald");
    expect(classifyBloodPressure(125, 70).badgeClass).toContain("yellow");
    expect(classifyBloodPressure(150, 70).badgeClass).toContain("orange");
    expect(classifyBloodPressure(190, 70).badgeClass).toContain("rose");
  });
});
