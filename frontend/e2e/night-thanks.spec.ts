import { test, expect, type Page } from "@playwright/test";

// Issue #66: a thank-you for the nights, a gentle rest note, days away, and the
// Super Admin's amber warning before a 4th night in a row.

const summary = {
  from: "2026-09-29", to: "2026-10-28", days: 30,
  caregivers: [
    { caregiver_id: "c2", name: "Fatima", avatar_color: "#db2777", hours_on_duty: 120, night_hours: 81, nights: 9, consecutive_nights: 3, last_night_off: "2026-10-24", night_alerts_answered: 2 },
    { caregiver_id: "c1", name: "Hussain", avatar_color: "#059669", hours_on_duty: 100, night_hours: 63, nights: 7, consecutive_nights: 0, night_alerts_answered: 0 },
    { caregiver_id: "c3", name: "Maryam", avatar_color: "#2563eb", hours_on_duty: 90, night_hours: 54, nights: 6, consecutive_nights: 1, night_alerts_answered: 1 },
  ],
};

async function mockApi(page: Page, locale: string, role: string, posts: { path: string; body: Record<string, unknown> }[]) {
  const away: Record<string, unknown>[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 60000\nevent: connected\ndata: {}\n\n" });
    if (path === "/auth/me") return json({ id: "c1", name: "Hussain", role, avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path === "/shifts/summary") return json(summary);
    if (path === "/shifts/unavailable" && req.method() === "GET") return json(away);
    if (path === "/shifts/unavailable" && req.method() === "POST") {
      const body = req.postDataJSON();
      posts.push({ path, body });
      const u = { id: "u1", caregiver_id: "c1", name: "Hussain", ...body };
      away.push(u);
      return json(u, 201);
    }
    if (path === "/users")
      return json([
        { id: "c1", name: "Hussain", role: "super_admin", is_active: true, avatar_color: "#059669" },
        { id: "c2", name: "Fatima", role: "caregiver", is_active: true, avatar_color: "#db2777" },
      ]);
    if (path === "/shifts" && req.method() === "GET") return json([{ id: "sh_night", name_ar: "ليل", name_en: "Night", start_time: "20:00", end_time: "08:00", duration_hours: 12, color: "#4338ca", is_active: true }]);
    if (path === "/shifts/current-duty") return json({});
    if (path.startsWith("/shifts/assignments") && req.method() === "GET") return json([]);
    if (path === "/shifts/load")
      return json({ date: url.searchParams.get("date"), night_shift_ids: ["sh_night"], caregivers: [{ caregiver_id: "c2", nights_before: 3, unavailable: false }, { caregiver_id: "c1", nights_before: 0, unavailable: false }] });
    if (path === "/shifts/assignments" && req.method() === "POST") {
      posts.push({ path, body: req.postDataJSON() });
      return json({ status: "assigned", warning: "consecutive_nights", warnings: ["consecutive_nights"], consecutive_nights: 4 });
    }
    return json([]);
  });
}

const noScroll = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);

for (const locale of ["en", "ar"] as const) {
  for (const scheme of ["light", "dark"] as const) {
    test(`thank-you card reads as appreciation and fits 375 px (${locale}, ${scheme})`, async ({ page, context, baseURL }) => {
      await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
      await page.emulateMedia({ colorScheme: scheme });
      await page.setViewportSize({ width: 375, height: 740 });
      await mockApi(page, locale, "caregiver", []);
      await page.goto("/report");
      const card = page.getByTestId("night-thanks");
      await expect(card).toContainText(locale === "ar" ? "شكراً على سهر هذا الشهر" : "Thank you for this month's nights");
      // In name order, not a ranking.
      await expect(page.getByTestId("night-thanks-list")).toHaveText(
        locale === "ar" ? "Fatima: 9 ليالٍ · Hussain: 7 ليالٍ · Maryam: 6 ليالٍ" : "Fatima: 9 nights · Hussain: 7 nights · Maryam: 6 nights",
      );
      await expect(page.getByTestId("night-rest-note")).toHaveCount(1);
      await expect(page.getByTestId("night-rest-note")).toContainText(locale === "ar" ? "ليلة راحة ستفيد" : "a night off would help");
      expect(await noScroll(page)).toBe(true);
    });
  }

  test(`mark days I can't take a shift (${locale})`, async ({ page, context, baseURL }) => {
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    await page.setViewportSize({ width: 375, height: 740 });
    const posts: { path: string; body: Record<string, unknown> }[] = [];
    await mockApi(page, locale, "caregiver", posts);
    await page.goto("/report");
    await page.getByTestId("unavailable-add").click();
    await page.getByTestId("unavailable-from").fill("2026-11-10");
    await page.getByTestId("unavailable-save").click();
    await expect(page.getByTestId("unavailable-row")).toHaveCount(1);
    expect(posts[0].body).toMatchObject({ start_date: "2026-11-10", end_date: "2026-11-10" });
    expect(await noScroll(page)).toBe(true);
  });
}

test("Super Admin: a 4th night in a row shows an amber warning, not a block", async ({ page, context, baseURL }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  await page.setViewportSize({ width: 375, height: 800 });
  const posts: { path: string; body: Record<string, unknown> }[] = [];
  await mockApi(page, "en", "super_admin", posts);
  await page.goto("/report");
  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "Shift Management" }).first().click();
  await page.getByRole("button", { name: "Duty Schedule" }).click();
  const picker = page.getByRole("combobox").first();
  await expect(picker.locator("option", { hasText: "Fatima — 3 nights in a row" })).toHaveCount(1);
  await picker.selectOption("c2");
  await expect(page.getByTestId("assign-warning")).toContainText("Fatima has done 3 nights in a row — assign someone else so they can rest?");
  expect(posts.find((p) => p.path === "/shifts/assignments")?.body).toMatchObject({ shift_id: "sh_night", caregiver_id: "c2" });
});
