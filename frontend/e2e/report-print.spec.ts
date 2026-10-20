import { test, expect, type Page } from "@playwright/test";

// Issue #14: the doctor report prints on one A4 page (14 days, 6 medications), in
// Arabic and English, with unrecorded doses shown, never folded into "taken".
test.skip(({ browserName }) => browserName !== "chromium", "PDF output needs Chromium");

const me = { id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669" };

function summary() {
  const day = (i: number) => new Date(Date.UTC(2026, 9, 7 + i)).toISOString().slice(0, 10);
  const meds = ["Warfarin", "Clopidogrel", "Atorvastatin", "Amlodipine", "Metformin", "Citicoline"].map((n, i) => ({
    medication_id: `m${i}`, name_ar: `دواء ${i + 1}`, name_en: n, dosage: `${(i + 1) * 5} mg`, is_critical: i < 2,
    past_dosages: i === 0 ? ["2.5 mg"] : [], expected: 14 + (i % 2) * 14, taken: 12 + (i % 2) * 13, partial: i === 2 ? 1 : 0,
    vomited: i === 3 ? 1 : 0, refused: 0, skipped: i === 4 ? 1 : 0, not_recorded: 2, rate: 85.7, missed_dates: [day(3), day(9)],
  }));
  return {
    generated_at: "2026-10-20T09:00:00Z", days_covered: 14, start_date: day(0), end_date: day(13),
    vitals_stats: { total_readings: 42, avg_systolic: 138, max_systolic: 186, min_systolic: 98, avg_diastolic: 84, max_diastolic: 118, min_diastolic: 60, avg_pulse: 76, crisis_count: 1, elevated_count: 20, low_count: 0, fast_positive_count: 0, unverified_sugar_count: 0, avg_blood_sugar: 7.1 },
    adherence: { total_scheduled: 126, total_taken: 112, total_skipped: 1, total_partial: 1, total_refused: 0, total_vomited: 1, total_not_recorded: 12, adherence_rate: 88.9 },
    per_medication: meds,
    daily_bp: Array.from({ length: 14 }, (_, i) => ({ date: day(i), avg_sys: 130 + (i % 5) * 6, avg_dia: 80 + (i % 4) * 3, max_sys: i === 6 ? 186 : 150, min_sys: 110, readings: 3 })),
    alertness_distribution: { alert: 30, confused: 2, restless: 1, drowsy: 0, not_assessed: 9 },
    recent_notes: Array.from({ length: 6 }, (_, i) => ({ id: `n${i}`, caregiver_id: "c1", caregiver_name: "Fatima", created_at: "2026-10-19T08:00:00Z", shift_type: "morning", summary: "Ate well, walked twice with help, slept after lunch.", urgent_flag: false })),
    recent_vitals: Array.from({ length: 40 }, (_, i) => ({ id: `v${i}`, caregiver_id: "c1", caregiver_name: "Fatima", recorded_at: "2026-10-19T08:00:00Z", vital_type: "bp", systolic: 140, diastolic: 85, pulse: 75, alertness_level: "alert", bp_status: "STAGE_2" })),
    insulin_logs: [],
  };
}

async function mock(page: Page) {
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 60000\nevent: connected\ndata: {}\n\n" });
    if (path === "/auth/me") return json(me);
    if (path.startsWith("/export/summary")) return json(summary());
    if (path === "/alerts/unacked") return json([]);
    return json([]);
  });
}

for (const locale of ["ar", "en"] as const) {
  test(`report prints on one A4 page (${locale})`, async ({ page, context, baseURL }, testInfo) => {
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    await mock(page);
    await page.goto("/export");
    await expect(page.getByText(locale === "ar" ? "لم تُسجّل" : "Not recorded").first()).toBeVisible();

    const pdf = await page.pdf({ format: "A4", printBackground: true, preferCSSPageSize: true });
    await testInfo.attach(`report-${locale}.pdf`, { body: pdf, contentType: "application/pdf" });
    const pages = (pdf.toString("latin1").match(/\/Type\s*\/Page[^s]/g) || []).length;
    expect(pages, "the summary must fit one A4 page").toBe(1);
  });
}
