import { test, expect } from "@playwright/test";

// The shift-note box used to turn white on focus in dark mode while its text
// stayed white: nothing typed could be seen. It must stay readable, and big
// enough to see a few lines.
test.use({ colorScheme: "dark" });

for (const [path, label] of [["/handovers", "Extra note"], ["/vitals?quick=bp", null]] as const) {
  test(`dark mode: typing in the ${path} note box stays readable`, async ({ page, baseURL }) => {
    await page.setViewportSize({ width: 375, height: 800 });
    await page.context().addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
    await page.route("**/api/v1/**", async (route) => {
      const p = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, "");
      const json = (b: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(b) });
      if (p.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
      if (p === "/auth/me") return json({ id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: "en", is_active: true, created_at: "2026-10-01T00:00:00Z" });
      return json([]);
    });
    await page.goto(path);
    const box = label ? page.getByRole("textbox", { name: label }) : page.locator("textarea").first();
    await box.click();
    await box.fill("She ate half her lunch");
    const c = await box.evaluate((el) => {
      const s = getComputedStyle(el);
      return { bg: s.backgroundColor, fg: s.color, h: el.getBoundingClientRect().height };
    });
    expect(c.bg).not.toBe(c.fg);
    expect(c.bg).not.toMatch(/^rgb\(255, 255, 255\)$/);
    expect(c.h).toBeGreaterThanOrEqual(100);
    await box.screenshot({ path: test.info().outputPath("box.png") });
  });
}
