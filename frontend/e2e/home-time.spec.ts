import { test, expect } from "@playwright/test";

// Issue #58: a sibling helping from London at 23:30 sees home's date (Riyadh is
// already 01:30 the next day), and a banner saying times are home time.
test.use({ timezoneId: "Europe/London" });

for (const locale of ["en", "ar"] as const) {
  test(`phone in London: Home opens on the household date with the home-time banner (${locale})`, async ({ page, context, baseURL }) => {
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    await page.clock.install({ time: new Date("2026-07-10T22:30:00Z") }); // 23:30 London, 01:30 Riyadh
    const scheduleDates: string[] = [];
    await page.route("**/api/v1/**", async (route) => {
      const url = new URL(route.request().url());
      const path = url.pathname.replace(/^\/api\/v1/, "");
      const json = (b: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(b) });
      if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 60000\nevent: connected\ndata: {}\n\n" });
      if (path === "/health") return json({ status: "healthy", timezone: "Asia/Riyadh" });
      if (path === "/auth/me") return json({ id: "c2", name: "Fatima", role: "caregiver", avatar_color: "#db2777", preferred_lang: locale, is_active: true, created_at: "2026-07-01T00:00:00Z" });
      if (path.startsWith("/medications/schedule")) {
        scheduleDates.push(url.searchParams.get("date") || "");
        return json({ date: url.searchParams.get("date"), schedule: [] });
      }
      return json([]);
    });
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto("/");

    await expect(page.getByLabel(locale === "ar" ? "تاريخ الجدول" : "Schedule date")).toHaveValue("2026-07-11");
    await expect.poll(() => scheduleDates.at(-1)).toBe("2026-07-11");
    const banner = page.getByTestId("home-time-banner");
    await expect(banner).toContainText(locale === "ar" ? "بتوقيت البيت" : "Times shown are home time");
    await expect(banner).toContainText("Riyadh");
    await expect(banner).toContainText("1:30");
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
}
