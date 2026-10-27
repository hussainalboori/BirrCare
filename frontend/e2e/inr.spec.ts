import { test, expect, type Page } from "@playwright/test";

// Issue #62: INR entry with the doctor's range, and a dose that changes by weekday.

async function mockApi(page: Page, locale: string, posts: Record<string, unknown>[]) {
  const results: Record<string, unknown>[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 60000\nevent: connected\ndata: {}\n\n" });
    if (path === "/auth/me") return json({ id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path === "/labs" && req.method() === "GET") return json({ results, inr_target: { inr_target_low: 2, inr_target_high: 3 } });
    if (path === "/labs" && req.method() === "POST") {
      const body = req.postDataJSON();
      posts.push(body);
      const r = { id: "l1", kind: "inr", value: body.value, taken_on: body.taken_on, next_test_on: body.next_test_on, out_of_range: body.value < 2 || body.value > 3 };
      results.unshift(r);
      return json(r, 201);
    }
    if (path.startsWith("/medications/schedule"))
      return json({
        date: url.searchParams.get("date"),
        schedule: [
          {
            medication: { id: "med_w", name_ar: "وارفارين", name_en: "Warfarin", dosage: "2.5 mg", frequency_slots: ["evening"], slot_times: { evening: "20:00" }, is_active: true },
            slot: "evening",
            time: "20:00",
            interval_hours: 18,
            dose_changed_from_yesterday: true,
          },
        ],
      });
    return json([]);
  });
}

for (const locale of ["en", "ar"] as const) {
  test(`INR 4.1 with range 2–3: amber "tell the doctor", 375 px (${locale})`, async ({ page, context, baseURL }) => {
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    await page.setViewportSize({ width: 375, height: 667 });
    const posts: Record<string, unknown>[] = [];
    await mockApi(page, locale, posts);
    await page.goto("/vitals");
    const card = page.getByTestId("inr-card");
    await expect(card).toContainText("2–3");
    await card.getByTestId("inr-value").fill("4.1");
    await card.getByTestId("inr-save").click();
    await expect(card.getByTestId("inr-last")).toContainText(locale === "ar" ? "أخبر الطبيب قبل الجرعة القادمة" : "tell the doctor before the next dose");
    expect(posts[0]).toMatchObject({ kind: "inr", value: 4.1 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });

  test(`a weekday dose shows "different today" on the card (${locale})`, async ({ page, context, baseURL }) => {
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    await page.setViewportSize({ width: 375, height: 667 });
    await mockApi(page, locale, []);
    await page.goto("/");
    await expect(page.getByText("2.5 mg").first()).toBeVisible();
    await expect(page.getByTestId("dose-different-today")).toContainText(locale === "ar" ? "جرعة مختلفة اليوم" : "different today");
  });
}
