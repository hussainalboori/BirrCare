import { test, expect, type Page } from "@playwright/test";

// Issue #7: bedtime dose given at 23:30; at 03:45 the next day's bedtime card must be locked.
test.use({ timezoneId: "Asia/Riyadh" });

const caregiver = {
  id: "c1", name: "مريم", role: "caregiver", avatar_color: "#059669",
  preferred_lang: "ar", is_active: true, created_at: "2026-10-01T00:00:00Z",
};

async function mockApi(page: Page, logCalls: string[]) {
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const json = (body: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });

    if (path.startsWith("/events/stream")) {
      return route.fulfill({ status: 200, contentType: "text/event-stream", body: 'event: connected\ndata: {"status":"connected"}\n\n' });
    }
    if (path === "/auth/me") return json(caregiver);
    if (path === "/caregivers") return json([{ id: caregiver.id, name: caregiver.name, avatar_color: caregiver.avatar_color }]);
    if (path === "/medications/log") {
      logCalls.push(route.request().postData() || "");
      return route.fulfill({ status: 409, contentType: "application/json", body: JSON.stringify({ error: "DOSE_TOO_SOON" }) });
    }
    if (path.startsWith("/medications/schedule")) {
      return json({
        date: "2026-10-20",
        schedule: [
          {
            slot: "bedtime",
            time: "22:30",
            medication: {
              id: "m1", name_ar: "وارفارين", name_en: "Warfarin", dosage: "5 mg",
              frequency_slots: ["bedtime"], is_active: true, created_at: "2026-10-01T00:00:00Z",
            },
            last_taken: {
              taken_at: "2026-10-19T20:30:00Z", // 23:30 in Riyadh
              slot: "bedtime", scheduled_date: "2026-10-19", caregiver_name: "Fatima", hours_since: 4.25,
            },
            interval_hours: 18,
            next_allowed_at: "2026-10-20T14:30:00Z", // 17:30 in Riyadh
          },
        ],
      });
    }
    return json([]);
  });
}

for (const locale of ["ar", "en"] as const) {
  test(`locale=${locale}: D+1 bedtime card at 03:45 has no enabled "mark taken" button`, async ({ page, context, baseURL }) => {
    await page.clock.setFixedTime(new Date("2026-10-20T03:45:00+03:00"));
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    const logCalls: string[] = [];
    await mockApi(page, logCalls);
    await page.goto("/");

    await expect(page.getByText(locale === "ar" ? "وارفارين" : "Warfarin").first()).toBeVisible();
    const markTaken = page.getByRole("button", { name: locale === "ar" ? "تسجيل أخذ الجرعة" : "Mark as Taken" });
    await expect(markTaken).toHaveCount(0);

    await expect(
      page.getByText(locale === "ar" ? "آخر جرعة قبل 4 ساعات و15 دقيقة بواسطة Fatima" : "Last given 4 h 15 min ago by Fatima")
    ).toBeVisible();
    await expect(page.getByRole("status").filter({ hasText: locale === "ar" ? "بعد" : "After" })).toBeVisible();
    expect(logCalls).toHaveLength(0);
  });
}
