import { test, expect, type Page } from "@playwright/test";

// Issue #67: "Today in short" on the Report page, for viewers too, with the
// evening notification and the lock-screen privacy choice.

const summaries = {
  en: { date: "2026-10-28", headline: "Today: 1 of 2 doses given, BP 128–141, ate well", lines: ["Medicines: 1 of 2 given (not recorded: Atorvastatin)", "Blood thinner: given ✓", "Blood pressure (top number): 128–141", "Meals and mood: ate well, good mood", "On duty: Fatima", "Last note: «Walked to the kitchen twice.»"] },
  ar: { date: "2026-10-28", headline: "اليوم: أُعطيت 1 من 2 جرعات، الضغط 128–141، أكلت جيداً", lines: ["الأدوية: 1 من 2 (لم تُسجَّل: أتورفاستاتين)", "مميّع الدم: أُعطي ✓", "الضغط (الانقباضي): 128–141", "الأكل والمزاج: أكلت جيداً، مزاج جيد", "المناوب: Fatima"] },
};

async function mockApi(page: Page, locale: "en" | "ar", role: string, puts: Record<string, unknown>[]) {
  const prefs = { enabled: false, lock_details: false, time: "21:00" };
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 60000\nevent: connected\ndata: {}\n\n" });
    if (path === "/auth/me") return json({ id: "c9", name: "Ahmed", role, avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path === "/summary/today") return json(summaries[locale]);
    if (path === "/summary/prefs" && req.method() === "GET") return json(prefs);
    if (path === "/summary/prefs" && req.method() === "PUT") {
      const body = req.postDataJSON();
      puts.push(body);
      Object.assign(prefs, body);
      return json(prefs);
    }
    if (path === "/shifts/summary") return json({ caregivers: [] });
    return json([]);
  });
}

for (const locale of ["en", "ar"] as const) {
  for (const scheme of ["light", "dark"] as const) {
    test(`a viewer reads today in ten seconds and opts in (${locale}, ${scheme})`, async ({ page, context, baseURL }) => {
      await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
      await page.emulateMedia({ colorScheme: scheme });
      await page.setViewportSize({ width: 375, height: 740 });
      const puts: Record<string, unknown>[] = [];
      await mockApi(page, locale, "viewer", puts);
      await page.goto("/report");
      await expect(page.getByTestId("today-headline")).toHaveText(summaries[locale].headline);
      await expect(page.getByTestId("today-lines")).toContainText(locale === "ar" ? "مميّع الدم: أُعطي ✓" : "Blood thinner: given ✓");
      await expect(page.getByTestId("summary-lock-details")).toHaveCount(0);
      await page.getByTestId("summary-enabled").check();
      await expect(page.getByTestId("summary-lock-details")).not.toBeChecked();
      expect(puts[0]).toEqual({ enabled: true });
      await expect(page.getByTestId("summary-time")).toHaveCount(0); // only the Super Admin sets the time
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    });
  }
}

test("the Super Admin sets the time the summary goes out", async ({ page, context, baseURL }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  const puts: Record<string, unknown>[] = [];
  await mockApi(page, "en", "super_admin", puts);
  await page.goto("/report");
  await page.getByTestId("summary-time").fill("20:30");
  await expect.poll(() => puts.some((p) => p.time === "20:30")).toBe(true);
});
