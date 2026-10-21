import { test, expect, type Page } from "@playwright/test";

const caregiver = {
  id: "c1",
  name: "مريم",
  role: "caregiver",
  avatar_color: "#059669",
  preferred_lang: "ar",
  is_active: true,
  created_at: "2026-10-01T00:00:00Z",
};

async function mockApi(page: Page) {
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const json = (body: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });

    if (path.startsWith("/events/stream")) {
      return route.fulfill({ status: 200, contentType: "text/event-stream", body: 'event: connected\ndata: {"status":"connected"}\n\n' });
    }
    if (path === "/auth/me") return json(caregiver);
    if (path === "/caregivers") return json([caregiver]);
    if (path.startsWith("/medications/schedule")) {
      return json({
        date: "2026-10-19",
        schedule: [
          {
            slot: "morning",
            medication: {
              id: "m1", name_ar: "أسبرين", name_en: "Aspirin", dosage: "81 mg",
              frequency_slots: ["morning"], is_active: true, created_at: "2026-10-01T00:00:00Z",
            },
          },
        ],
      });
    }
    if (path.startsWith("/vitals")) {
      return json([
        {
          id: "v1", caregiver_id: "c1", caregiver_name: "مريم", recorded_at: "2026-10-19T08:00:00Z",
          vital_type: "bp", systolic: 185, diastolic: 121, pulse: 88, alertness_level: "alert", bp_status: "CRISIS",
        },
      ]);
    }
    if (path.startsWith("/handovers")) return json([]);
    return json({});
  });
}

for (const [locale, dir] of [["ar", "rtl"], ["en", "ltr"]] as const) {
  test.describe(`locale=${locale}`, () => {
    test.beforeEach(async ({ page, context, baseURL }) => {
      await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
      await mockApi(page);
    });

    test(`server-rendered <html dir="${dir}" lang="${locale}">`, async ({ page }) => {
      // Check the raw SSR HTML so a client-side correction can't mask a flash of wrong direction.
      const res = await page.request.get("/", { headers: { cookie: `birrcare_lang=${locale}` } });
      const html = await res.text();
      expect(html).toMatch(new RegExp(`<html[^>]*\\bdir="${dir}"`));
      expect(html).toMatch(new RegExp(`<html[^>]*\\blang="${locale}"`));

      await page.goto("/");
      await expect(page.locator("html")).toHaveAttribute("dir", dir);
      await expect(page.locator("html")).toHaveAttribute("lang", locale);
    });

    test("BP reading stays on one line and in systolic/diastolic order", async ({ page }) => {
      await page.goto("/vitals");
      const reading = page.getByTestId("bp-reading").first();
      await expect(reading).toBeVisible();
      await expect(reading).toHaveText(/185\/121\s*mmHg/);

      const lineHeights = await reading.evaluate((el) => {
        const rects = Array.from(el.getClientRects());
        const tops = new Set(rects.map((r) => Math.round(r.top)));
        return { lines: tops.size, direction: getComputedStyle(el).direction, whiteSpace: getComputedStyle(el).whiteSpace };
      });
      expect(lineHeights.lines).toBe(1);
      expect(lineHeights.direction).toBe("ltr");
      expect(lineHeights.whiteSpace).toBe("nowrap");

      // Visual order: the number must be left of "mmHg" even inside RTL text.
      const [numBox, unitBox] = await Promise.all([
        reading.locator("span").nth(0).boundingBox(),
        reading.locator("span").nth(1).boundingBox(),
      ]);
      expect(numBox!.x).toBeLessThan(unitBox!.x);
    });

    test("tap targets on the meds page are at least 48px", async ({ page }) => {
      await page.goto("/");
      const button = page.getByRole("button", { name: locale === "ar" ? /^أعطِ باسم / : /^Give as / });
      const box = await button.boundingBox();
      expect(box!.height).toBeGreaterThanOrEqual(48);
    });
  });
}
