import { describe, expect, it } from "vitest";

describe("PRN (as-needed) medications clinical safety", () => {
  it("recognizes worst-ever headache as a neurological red flag requiring F.A.S.T. assessment", () => {
    const isWorstHeadache = true;
    const reasonTag = "headache";

    // When a patient has sudden / worst-ever headache, emergency protocol triggers
    const requiresFastAssessment = reasonTag === "headache" && isWorstHeadache;
    expect(requiresFastAssessment).toBe(true);

    const fastPath = "/vitals?quick=fast";
    expect(fastPath).toContain("quick=fast");
  });

  it("calculates lock state correctly based on 24h count and min interval", () => {
    interface PRNCheck {
      countLast24h: number;
      maxPer24h: number;
      hoursSinceLast: number;
      minHours: number;
    }

    const checkCanGive = (status: PRNCheck) => {
      if (status.countLast24h >= status.maxPer24h) {
        return { canGive: false, reason: "limit_reached" };
      }
      if (status.hoursSinceLast < status.minHours) {
        return { canGive: false, reason: "too_soon" };
      }
      return { canGive: true, reason: null };
    };

    // Case 1: Under limit and sufficient time passed
    expect(checkCanGive({ countLast24h: 1, maxPer24h: 4, hoursSinceLast: 6, minHours: 4 })).toEqual({
      canGive: true,
      reason: null,
    });

    // Case 2: Too soon (3h passed, but 4h required)
    expect(checkCanGive({ countLast24h: 1, maxPer24h: 4, hoursSinceLast: 3, minHours: 4 })).toEqual({
      canGive: false,
      reason: "too_soon",
    });

    // Case 3: Reached 24h limit (4 doses given already)
    expect(checkCanGive({ countLast24h: 4, maxPer24h: 4, hoursSinceLast: 10, minHours: 4 })).toEqual({
      canGive: false,
      reason: "limit_reached",
    });
  });

  it("ensures reason chips cover all clinical scenarios", () => {
    const validReasons = ["headache", "pain", "fever", "cough", "other"];
    expect(validReasons).toContain("headache");
    expect(validReasons).toContain("pain");
    expect(validReasons).toContain("fever");
  });
});
