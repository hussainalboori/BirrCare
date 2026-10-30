import { test, expect, type Page } from "@playwright/test";

// "Start fresh": after the trial weeks the Super Admin removes the test records
// from Menu → Server, after seeing what goes and typing a word to confirm.

const counts = { doses: 312, readings: 95, alerts: 41, notes: 28, care: 120, shifts: 14, visits: 2 };

async function open(page: Page, baseURL: string, locale: "ar" | "en", scheme: "light" | "dark", posts: unknown[]) {
  await page.setViewportSize({ width: 375, height: 812 });
  await page.emulateMedia({ colorScheme: scheme });
  await page.context().addCookies([{ name: "birrcare_lang", value: locale, url: baseURL }]);
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const p = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (p.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
    if (p === "/auth/me") return json({ id: "u1", name: "Hussain", role: "super_admin", totp_enabled: true, avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (p === "/admin/reset" && req.method() === "GET") return json({ counts });
    if (p === "/admin/reset" && req.method() === "POST") {
      posts.push(req.postDataJSON());
      return json({ removed: counts, copy: "backup/before-reset/birrcare-20261030-210000.db" });
    }
    if (p === "/admin/backup/status") return json({ configured: false, stale: false, reason: "not_configured", pending: false, schedule: { times: ["03:00"], avoid_busy: true } });
    if (p === "/admin/update/status") return json({ version: "606fb10", available: null, update_available: false, result: null, policy: { mode: "manual" }, pending: false, checker_stale: false });
    if (p.startsWith("/medications/schedule")) return json({ date: "2026-10-30", schedule: [] });
    return json([]);
  });
  await page.goto("/");
  await page.getByRole("button", { name: locale === "ar" ? "القائمة" : "Menu" }).click();
  await page.getByRole("button", { name: locale === "ar" ? /^الخادم/ : /^Server/ }).click();
}

for (const locale of ["ar", "en"] as const) {
  for (const scheme of ["light", "dark"] as const) {
    test(`${locale} ${scheme}: see what goes, type the word, start fresh`, async ({ page, baseURL }) => {
      const posts: unknown[] = [];
      await open(page, baseURL!, locale, scheme, posts);
      await page.getByTestId("reset-open").click();
      const confirm = page.getByTestId("reset-confirm");
      await expect(confirm).toContainText("312");
      await expect(confirm).toContainText(locale === "ar" ? "يبقى" : "This stays");
      expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);

      const go = page.getByTestId("reset-go");
      await expect(go).toBeDisabled();
      await page.getByTestId("reset-word").fill("something");
      await expect(go).toBeDisabled();
      await page.getByTestId("reset-word").fill(locale === "ar" ? "مسح" : "reset");
      await go.click();
      await expect(page.getByTestId("reset-done")).toContainText("before-reset");
      expect(posts).toEqual([{ confirm: "start_fresh" }]);
    });
  }
}

test("cancel removes nothing", async ({ page, baseURL }) => {
  const posts: unknown[] = [];
  await open(page, baseURL!, "en", "light", posts);
  await page.getByTestId("reset-open").click();
  await page.getByRole("button", { name: "Cancel" }).last().click();
  await expect(page.getByTestId("reset-confirm")).toHaveCount(0);
  expect(posts).toEqual([]);
});
