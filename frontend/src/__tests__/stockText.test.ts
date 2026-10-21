import { describe, expect, it } from "vitest";
import { daysLeftText } from "@/lib/stockText";

describe("low stock wording (issue #20)", () => {
  it("counts days in English and Arabic", () => {
    expect(daysLeftText(3.7, false)).toBe("about 3 days left");
    expect(daysLeftText(1, false)).toBe("about 1 day left");
    expect(daysLeftText(0.4, false)).toBe("out or runs out today");
    expect(daysLeftText(2, true)).toBe("يكفي نحو يومين");
    expect(daysLeftText(3, true)).toBe("يكفي نحو 3 أيام");
    expect(daysLeftText(14, true)).toBe("يكفي نحو 14 يوماً");
  });
});
