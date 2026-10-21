import { test, expect } from "@playwright/test";

// The Super Admin has the most header buttons. At every width the header must fit
// without a sideways scroll, and the caregiver's name must never be cut off.
for (const locale of ["ar", "en"] as const) {
  for (const w of [375, 1280, 1536, 1759, 1760, 1920]) {
    test(`${locale} ${w}px: header fits and shows the full name`, async ({ browser, baseURL }) => {
      const ctx = await browser.newContext({ viewport: { width: w, height: 700 } });
      await ctx.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
      const page = await ctx.newPage();
      await page.route("**/api/v1/**", async (route) => {
        const path = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, "");
        const json = (b: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(b) });
        if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
        if (path === "/auth/me") return json({ id: "c1", name: "Hussain Alboori", role: "super_admin", totp_enabled: true, avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
        if (path === "/patient/emergency-link") return json({ token: "ab".repeat(32) });
        if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
        return json([]);
      });
      await page.goto("/");
      await page.getByTestId("sos").waitFor();
      const r = await page.evaluate(() => ({
        overflow: document.documentElement.scrollWidth > window.innerWidth,
        nameCut: [...document.querySelectorAll("header span")].some((e) => e.textContent === "Hussain Alboori" && e.scrollWidth > e.clientWidth),
      }));
      expect(r).toEqual({ overflow: false, nameCut: false });
      await ctx.close();
    });
  }
}
