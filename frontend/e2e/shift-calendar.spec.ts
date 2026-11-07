import { test, expect, type Page } from "@playwright/test";

// The roster as a calendar, with a shift that repeats every week.

const today = new Date().toISOString().slice(0, 10);
const month = today.slice(0, 7);

async function mockApi(page: Page, lang: "ar" | "en") {
  await page.context().addCookies([{ name: "birrcare_lang", value: lang, url: "http://127.0.0.1:3100" }]);
  const repeats: unknown[] = [];
  const posts: unknown[] = [];
  const hussain = { id: "c1", name: "Hussain", role: "super_admin", avatar_color: "#059669", preferred_lang: lang, is_active: true, created_at: "2026-10-01T00:00:00Z" };
  const shift = { id: "s24", name_ar: "24 ساعة", name_en: "24 hours", start_time: "08:00", end_time: "08:00", duration_hours: 24, color: "#059669", is_active: true };
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const method = route.request().method();
    const json = (b: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 200\n\n" });
    if (path === "/auth/me") return json(hussain);
    if (path === "/users") return json([hussain]);
    if (path === "/shifts") return json([shift]);
    if (path === "/shifts/current-duty") return json({ current_time: "10:00", date: today, is_on_duty: false });
    if (path === "/shifts/med-order") return json({ order: "clock" });
    if (path === "/shifts/assignments") return json([{ id: "a1", shift_id: "s24", caregiver_id: "c1", date: url.searchParams.get("date"), caregiver_name: "Hussain" }]);
    if (path === "/shifts/month") return json({ month, assignments: [{ date: today, shift_id: "s24", caregiver_id: "c1", caregiver_name: "Hussain", avatar_color: "#059669" }], repeats });
    if (path === "/shifts/repeats" && method === "POST") {
      const body = route.request().postDataJSON();
      posts.push(body);
      repeats.push({ id: "r1", shift_id: "s24", caregiver_id: "c1", caregiver_name: "Hussain", weekday: new Date(`${body.date}T12:00:00Z`).getUTCDay() });
      return json({ id: "r1" });
    }
    if (path === "/shifts/repeats/r1" && method === "DELETE") {
      repeats.length = 0;
      return json({ status: "stopped" });
    }
    return json([]);
  });
  return { posts };
}

async function openCalendar(page: Page, lang: "ar" | "en") {
  await page.goto("/");
  await page.getByRole("button", { name: lang === "ar" ? "القائمة" : "Menu" }).first().click();
  await page.getByRole("button", { name: lang === "ar" ? /الورديات/ : /shift management/i }).first().click();
  await page.getByRole("button", { name: lang === "ar" ? /جدول المناوبات/ : /duty schedule/i }).click();
  await expect(page.getByTestId("shift-calendar")).toBeVisible();
}

for (const lang of ["en", "ar"] as const) {
  test(`set and stop a weekly shift from the calendar (${lang})`, async ({ page }) => {
    const { posts } = await mockApi(page, lang);
    await openCalendar(page, lang);

    await expect(page.locator(`[data-date="${today}"]`)).toHaveAttribute("data-count", "1");
    await page.getByTestId("shift-repeat").click();
    await expect(page.getByTestId("shift-repeat-note")).toBeVisible();
    expect(posts).toEqual([{ shift_id: "s24", caregiver_id: "c1", date: today }]);

    await page.getByTestId("shift-repeat-stop").click();
    await page.getByTestId("shift-repeat-stop-yes").click();
    await expect(page.getByTestId("shift-repeat")).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
}

test("tapping a day opens that day without blanking the calendar", async ({ page }) => {
  await mockApi(page, "en");
  await openCalendar(page, "en");
  const other = await page.locator(`[data-testid="shift-calendar-day"]:not([data-date="${today}"])`).first();
  const date = await other.getAttribute("data-date");
  await other.click();
  await expect(page.getByTestId("shift-calendar")).toBeVisible();
  await expect(page.locator(`[data-date="${date}"]`)).toHaveAttribute("aria-selected", "true");
  const box = await other.boundingBox();
  expect(box!.height).toBeGreaterThanOrEqual(48);
});
