import { test, expect, type Page } from "@playwright/test";

// Issue #15: a corrected reading shows the original, in RTL and LTR, at 375 px.
const me = { id: "c1", name: "Fatima", role: "caregiver", avatar_color: "#059669" };
const now = new Date().toISOString();

async function mock(page: Page) {
  const posts: { path: string; body: unknown }[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (req.method() === "POST") posts.push({ path, body: req.postDataJSON() });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 60000\nevent: connected\ndata: {}\n\n" });
    if (path === "/auth/me") return json(me);
    if (path.endsWith("/correct")) return json({ id: "new" }, 201);
    if (path.startsWith("/vitals")) {
      return json([
        {
          id: "v2", caregiver_id: "c1", caregiver_name: "Fatima", recorded_at: now, vital_type: "bp", systolic: 119, diastolic: 80, pulse: 72,
          alertness_level: null, bp_status: "NORMAL",
          correction: { original_id: "v1", original_systolic: 190, original_diastolic: 100, original_pulse: 72, original_logged_by_name: "Fatima", reason: "Typo", corrected_at: now },
        },
        { id: "v3", caregiver_id: "c1", caregiver_name: "Fatima", recorded_at: now, vital_type: "bp", systolic: 135, diastolic: 85, pulse: 70, alertness_level: null, bp_status: "ELEVATED" },
      ]);
    }
    if (path === "/alerts/unacked") return json([]);
    return json([]);
  });
  return posts;
}

for (const locale of ["ar", "en"] as const) {
  test(`corrected reading shows the original without horizontal scroll (${locale})`, async ({ page, context, baseURL }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    const posts = await mock(page);
    await page.goto("/vitals");

    const note = page.getByTestId("correction-note");
    await expect(note).toBeVisible();
    await expect(note).toContainText(locale === "ar" ? "عدّلها" : "Edited by");
    await expect(note).toContainText("190/100");
    await expect(note).toContainText("Typo");
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);

    // Correct the other reading: new values plus a required reason.
    await page.getByRole("button", { name: locale === "ar" ? "تصحيح" : "Correct" }).last().click();
    const save = page.getByRole("button", { name: locale === "ar" ? "حفظ التصحيح" : "Save correction" });
    await expect(save).toBeDisabled();
    await page.getByLabel(locale === "ar" ? "الانقباضي" : "Systolic", { exact: true }).last().fill("125");
    await page.getByRole("button", { name: locale === "ar" ? "خطأ في الكتابة" : "Typo" }).click();
    await save.click();
    await expect.poll(() => posts.find((p) => p.path === "/vitals/v3/correct")?.body).toMatchObject({ systolic: 125, diastolic: 85, reason: locale === "ar" ? "خطأ في الكتابة" : "Typo" });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  });
}
