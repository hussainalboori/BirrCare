import { afterEach, describe, expect, it, vi } from "vitest";
import { THEME_SCRIPT, resolveTheme, themeClasses, haptic } from "@/lib/theme";

// Issue #21: Auto = Night from 21:00 to 07:00, otherwise the system setting.
describe("resolveTheme", () => {
  it("auto at 23:00 is night, at 08:00 follows the system", () => {
    expect(resolveTheme("auto", 23, false)).toBe("night");
    expect(resolveTheme("auto", 3, true)).toBe("night");
    expect(resolveTheme("auto", 8, false)).toBe("light");
    expect(resolveTheme("auto", 8, true)).toBe("dark");
    expect(resolveTheme("auto", 21, false)).toBe("night");
    expect(resolveTheme("auto", 7, false)).toBe("light");
  });
  it("an explicit choice wins at any hour", () => {
    expect(resolveTheme("light", 2, true)).toBe("light");
    expect(resolveTheme("night", 12, false)).toBe("night");
    expect(themeClasses("night")).toBe("dark night");
  });
});

describe("the pre-paint script matches resolveTheme", () => {
  afterEach(() => {
    vi.useRealTimers();
    document.documentElement.className = "";
    document.cookie = "birrcare_theme=; max-age=0";
  });
  const run = (cookie: string | null, hour: number, systemDark: boolean) => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 9, 21, hour, 30));
    document.cookie = cookie ? `birrcare_theme=${cookie}` : "birrcare_theme=; max-age=0";
    window.matchMedia = ((q: string) => ({ matches: systemDark && q.includes("dark") })) as unknown as typeof window.matchMedia;
    document.documentElement.className = "";
    new Function(THEME_SCRIPT)();
    return document.documentElement.className.trim();
  };
  for (const [cookie, hour, sys] of [[null, 23, false], [null, 8, false], [null, 8, true], ["light", 2, true], ["dark", 12, false], ["night", 12, false], ["bogus", 23, false]] as const) {
    it(`${cookie ?? "no cookie"} at ${hour}:30, system ${sys ? "dark" : "light"}`, () => {
      const mode = cookie === "light" || cookie === "dark" || cookie === "night" ? cookie : "auto";
      expect(run(cookie, hour, sys)).toBe(themeClasses(resolveTheme(mode, hour, sys)));
    });
  }
});

describe("haptic", () => {
  it("is shorter at night except for a crisis", () => {
    const vibrate = vi.fn();
    Object.defineProperty(navigator, "vibrate", { value: vibrate, configurable: true });
    document.documentElement.className = "dark night";
    haptic([60, 50, 60]);
    haptic([400, 150, 400], { crisis: true });
    expect(vibrate.mock.calls).toEqual([[[20, 50, 20]], [[400, 150, 400]]]);
    document.documentElement.className = "";
  });
});
