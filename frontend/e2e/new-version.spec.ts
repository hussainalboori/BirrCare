import { test, expect, type Page } from "@playwright/test";

// A phone that keeps the app open must not keep the old screens after an
// update: when the server answers with another version, offer a one-tap reload.
async function open(page: Page, baseURL: string, locale: "ar" | "en", version: () => string) {
  await page.context().addCookies([{ name: "birrcare_lang", value: locale, url: baseURL }]);
  await page.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(b) });
    if (p.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 200\nevent: connected\ndata: {}\n\n" });
    if (p === "/auth/me") return json({ id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (p === "/health") return json({ status: "healthy", version: version() });
    if (p.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
    return json([]);
  });
  await page.goto("/");
}

test("same version: no banner", async ({ page, baseURL }) => {
  await open(page, baseURL!, "en", () => "aaaaaaa");
  await page.waitForTimeout(4000);
  await expect(page.getByTestId("new-version-banner")).toHaveCount(0);
});

test("ar: after an update the open app offers to reload, and the reload loads it", async ({ page, baseURL }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  let server = "bbbbbbb";
  await open(page, baseURL!, "ar", () => server);
  const banner = page.getByTestId("new-version-banner");
  await expect(banner).toContainText("نسخة جديدة من التطبيق جاهزة", { timeout: 10_000 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
  // The reload brings the page the server now serves (same version here).
  server = "aaaaaaa";
  await banner.getByRole("button", { name: "حدّث الآن" }).click();
  await page.waitForLoadState("load");
  await page.waitForTimeout(4000);
  await expect(page.getByTestId("new-version-banner")).toHaveCount(0);
});
