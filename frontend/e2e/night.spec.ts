import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

// Issue #21: Night mode for a dark sickroom. True black, nothing bright except
// crisis red, readable text.

async function mockApi(page: Page) {
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
    if (path === "/auth/me") return json({ id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: "ar", is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path === "/caregivers") return json([{ id: "c1", name: "Hussain", avatar_color: "#059669" }, { id: "c2", name: "Fatima", avatar_color: "#db2777" }]);
    if (path === "/medications/stock") return json([{ medication_id: "m1", name_ar: "أسبرين", name_en: "Aspirin", dosage: "81 mg", stock_units: 2, units_per_dose: 1, doses_per_day: 1, days_left: 2, refill_threshold_days: 4, low: true, blood_thinner: true }]);
    if (path.startsWith("/handovers")) return json([{ id: "n1", caregiver_id: "c2", caregiver_name: "Fatima", avatar_color: "#db2777", created_at: new Date().toISOString(), shift_type: "morning", summary: "", urgent_flag: false, tags: ["ate_half", "needs_rest"] }]);
    if (path.startsWith("/medications/schedule")) {
      const med = (id: string, en: string, ar: string) => ({ id, name_ar: ar, name_en: en, dosage: "81 mg", frequency_slots: ["morning"], is_active: true, created_at: "2026-10-01T00:00:00Z" });
      return json({
        date: "2026-10-21",
        schedule: [
          { slot: "morning", medication: med("m1", "Aspirin", "أسبرين") },
          { slot: "morning", medication: med("m2", "Atorvastatin", "أتورفاستاتين"), log: { id: "L1", medication_id: "m2", caregiver_id: "c2", slot: "morning", scheduled_date: "2026-10-21", taken_at: new Date().toISOString(), status: "taken", caregiver_name: "Fatima" } },
        ],
      });
    }
    if (path.startsWith("/export/summary")) {
      return json({
        generated_at: new Date().toISOString(), days_covered: 7, start_date: "2026-10-15", end_date: "2026-10-21",
        vitals_stats: { total_readings: 3, avg_systolic: 150, max_systolic: 185, min_systolic: 120, avg_diastolic: 90, max_diastolic: 121, min_diastolic: 80, avg_pulse: 80, crisis_count: 1, elevated_count: 1 },
        adherence: { total_scheduled: 14, total_taken: 12, total_skipped: 1, adherence_rate: 85.7 },
        per_medication: [{ medication_id: "m1", name_ar: "أسبرين", name_en: "Aspirin", dosage: "81 mg", is_critical: true, expected: 7, taken: 6, partial: 0, vomited: 0, refused: 0, skipped: 1, not_recorded: 0, rate: 85.7, missed_dates: [] }],
        daily_bp: [{ date: "2026-10-21", avg_sys: 150, avg_dia: 90, max_sys: 185, min_sys: 120, readings: 3 }],
        alertness_distribution: { alert: 3 },
        recent_notes: [], recent_vitals: [], insulin_logs: [],
      });
    }
    if (path.startsWith("/vitals")) return json([{ id: "v1", caregiver_id: "c1", caregiver_name: "Hussain", recorded_at: new Date().toISOString(), vital_type: "bp", systolic: 150, diastolic: 95, pulse: 80, bp_status: "STAGE_2" }]);
    return json([]);
  });
}

/** Elements brighter than luminance 0.4 (text or filled background), ignoring crisis red. */
async function brightElements(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const canvas = document.createElement("canvas");
    canvas.width = canvas.height = 1;
    const ctx = canvas.getContext("2d", { willReadFrequently: true })!;
    const rgba = (c: string) => {
      ctx.clearRect(0, 0, 1, 1);
      ctx.fillStyle = "#000";
      ctx.fillStyle = c;
      ctx.fillRect(0, 0, 1, 1);
      const d = ctx.getImageData(0, 0, 1, 1).data;
      return [d[0], d[1], d[2], d[3] / 255];
    };
    const lin = (v: number) => ((v /= 255) <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
    const lum = ([r, g, b]: number[]) => 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
    const isRed = ([r, g, b]: number[]) => r > 120 && r > g * 1.5 && r > b * 1.3;
    const red = '[class*="bg-rose-5"],[class*="bg-rose-6"],[class*="bg-rose-7"],[class*="bg-rose-8"],[class*="bg-red-"],[data-crisis]';
    const out: string[] = [];
    for (const el of Array.from(document.body.querySelectorAll<HTMLElement>("*"))) {
      const r = el.getBoundingClientRect();
      const cs = getComputedStyle(el);
      if (r.width === 0 || r.height === 0 || cs.visibility === "hidden" || cs.display === "none" || Number(cs.opacity) === 0) continue;
      if (el.closest(red) || el.closest("nextjs-portal") || el.tagName === "IMG" || el.tagName === "svg" || el.closest("svg")) continue;
      const hasText = Array.from(el.childNodes).some((n) => n.nodeType === 3 && n.textContent!.trim());
      const checks: [string, string][] = [["bg", cs.backgroundColor]];
      if (hasText) checks.push(["text", cs.color]);
      for (const [what, c] of checks) {
        const v = rgba(c);
        if (v[3] < 0.5 || isRed(v)) continue;
        if (lum(v) > 0.4) out.push(`${what} ${c} lum=${lum(v).toFixed(2)} <${el.tagName.toLowerCase()} class="${(el.getAttribute("class") || "").slice(0, 90)}">${(el.textContent || "").trim().slice(0, 30)}`);
      }
    }
    return out;
  });
}

test("cookie theme=night: black on the first paint, before any script runs", async ({ browser, baseURL }) => {
  const ctx = await browser.newContext({ javaScriptEnabled: false });
  await ctx.addCookies([{ name: "birrcare_theme", value: "night", url: baseURL! }]);
  const page = await ctx.newPage();
  await page.goto("/");
  expect(await page.evaluate(() => getComputedStyle(document.body).backgroundColor)).toBe("rgb(0, 0, 0)");
  expect(await page.getAttribute("html", "class")).toContain("night");
  expect(await page.getAttribute('meta[name="theme-color"]', "content")).toBe("#000000");
  await ctx.close();
});

test("Auto after 21:00 is Night before the page paints (the head script)", async ({ browser, baseURL }) => {
  const ctx = await browser.newContext();
  await ctx.addInitScript(() => {
    const RealDate = Date;
    const fixed = new RealDate(2026, 9, 21, 23, 30).getTime();
    // @ts-expect-error -- test clock for the pre-paint script only
    globalThis.Date = class extends RealDate {
      constructor(...a: unknown[]) {
        // @ts-expect-error spread into Date
        super(...(a.length ? a : [fixed]));
      }
      static now() {
        return fixed;
      }
    };
  });
  const page = await ctx.newPage();
  await mockApi(page);
  await page.goto("/");
  expect(await page.evaluate(() => getComputedStyle(document.body).backgroundColor)).toBe("rgb(0, 0, 0)");
  await ctx.close();
});

for (const locale of ["ar", "en"] as const) {
  test(`${locale} Night at 375px: nothing bright except crisis red, and text contrast passes`, async ({ browser, baseURL }) => {
    const ctx = await browser.newContext({ viewport: { width: 375, height: 800 } });
    await ctx.addCookies([
      { name: "birrcare_theme", value: "night", url: baseURL! },
      { name: "birrcare_lang", value: locale, url: baseURL! },
    ]);
    const page = await ctx.newPage();
    await mockApi(page);
    await page.goto("/");
    await page.getByTestId("low-stock").waitFor();
    await page.getByText(locale === "ar" ? "أتورفاستاتين" : "Atorvastatin").first().waitFor();
    expect(await brightElements(page)).toEqual([]);
    const axeHome = await new AxeBuilder({ page }).withRules(["color-contrast"]).analyze();
    expect(axeHome.violations.flatMap((v) => v.nodes.map((n) => `${n.target}: ${n.failureSummary}`))).toEqual([]);

    await page.goto("/vitals");
    await page.getByRole("button", { name: locale === "ar" ? /تسجيل قراءة/ : /Record/ }).first().click();
    await page.getByRole("dialog").getByRole("button").nth(1).click();
    await page.waitForTimeout(300);
    expect(await brightElements(page)).toEqual([]);
    const axeForm = await new AxeBuilder({ page }).include('[role="dialog"]').withRules(["color-contrast"]).analyze();
    expect(axeForm.violations.flatMap((v) => v.nodes.map((n) => `${n.target}: ${n.failureSummary}`))).toEqual([]);
    await ctx.close();
  });
}

test("the Screen colours menu switches to Night and remembers it", async ({ page, context, baseURL }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  await mockApi(page);
  await page.goto("/");
  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: /Screen colours/ }).click();
  await page.getByRole("button", { name: /Night \(black\)/ }).click();
  await expect(page.locator("html")).toHaveClass(/night/);
  expect(await page.evaluate(() => getComputedStyle(document.body).backgroundColor)).toBe("rgb(0, 0, 0)");
  expect((await context.cookies()).find((c) => c.name === "birrcare_theme")?.value).toBe("night");
  await page.reload();
  await expect(page.locator("html")).toHaveClass(/night/);
});

test("Night: every page and the main dialogs stay dim", async ({ browser, baseURL }) => {
  const ctx = await browser.newContext({ viewport: { width: 375, height: 800 } });
  await ctx.addCookies([{ name: "birrcare_theme", value: "night", url: baseURL! }]);
  const page = await ctx.newPage();
  await mockApi(page);
  const found: Record<string, string[]> = {};
  for (const path of ["/handovers", "/insulin", "/fast", "/export", "/emergency-qr"]) {
    await page.goto(path);
    await page.waitForTimeout(700);
    const b = await brightElements(page);
    if (b.length) found[path] = b;
  }
  await page.goto("/");
  await page.getByRole("button", { name: "القائمة" }).click();
  await page.waitForTimeout(300);
  let b = await brightElements(page);
  if (b.length) found["menu"] = b;
  await page.keyboard.press("Escape");
  await page.goto("/");
  await page.getByRole("button", { name: "تبديل" }).click();
  await page.waitForTimeout(300);
  b = await brightElements(page);
  if (b.length) found["pin pad"] = b;
  expect(found).toEqual({});
  await ctx.close();
});
