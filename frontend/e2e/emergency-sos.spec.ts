import { test, expect } from "@playwright/test";

// Issue #19: the SOS button (with a plain-word label) and the QR page. The card
// itself is server-rendered HTML; it is covered by Go tests, emergencyCard.test.ts
// and a manual run against a real backend.
const TOKEN = "ab".repeat(32);

for (const locale of ["ar", "en"] as const) {
  test(`${locale}: SOS in the header links to the card; QR page shows the code`, async ({ page, context, baseURL }) => {
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    page.on("dialog", (d) => {
      throw new Error(`unexpected dialog: ${d.message()}`);
    });
    await page.route("**/api/v1/**", async (route) => {
      const path = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, "");
      const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
      if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
      if (path === "/auth/me") return json({ id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
      if (path === "/patient/emergency-link") return json({ token: TOKEN, rotated_at: "2026-10-21T07:00:00Z" });
      if (path === "/patient") return json({ profile: { name: "Amina" }, has_emergency_link: true });
      if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
      return json([]);
    });
    await page.route(`**/emergency/${TOKEN}*`, (route) => route.fulfill({ status: 200, contentType: "text/html", body: "<p>card</p>" }));

    await page.goto("/");
    const sos = page.getByTestId("sos");
    await expect(sos).toHaveText(locale === "ar" ? "طوارئ" : "SOS");
    await expect(sos).toHaveAttribute("href", `/emergency/${TOKEN}`);

    await page.goto("/emergency-qr");
    await expect(page.getByAltText(locale === "ar" ? "رمز QR لبطاقة الطوارئ" : "QR code for the emergency card")).toBeVisible();
    await expect(page.getByText(locale === "ar" ? "اطبع رمز QR" : "Print the QR code")).toBeVisible();
    // A caregiver can show and print the QR, but only admins edit details or make a new one.
    await expect(page.getByText(locale === "ar" ? "احفظ البيانات" : "Save details")).toHaveCount(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
}

// Issue #56: on the card itself, the blood thinner's last dose is the first thing
// a paramedic sees on a phone, and it survives printing. The card is rendered by
// the same function the /emergency/[token] route uses.
test("the blood-thinner last-dose line is the first content block at 375 px, on screen and in print", async ({ page }) => {
  const { renderEmergencyCard } = await import("../src/lib/emergencyCard");
  const html = renderEmergencyCard({
    patient: { name: "Amina", dob: "1950-03-02", diagnosis_ar: "", diagnosis_en: "Ischaemic stroke", stroke_date: "", allergies_ar: "", allergies_en: "", blood_type: "", baseline_bp: "", notes: "" },
    medications: [
      { name_ar: "أبيكسابان", name_en: "Apixaban", dosage: "5 mg", slots: ["morning", "bedtime"], blood_thinner: true, last_dose: { at: "2026-10-20T20:30:00Z", status: "taken", hours_ago: 8.25, vomited: false } },
    ],
    last_bp: [],
    last_insulin: { at: "2026-10-21T02:45:00Z", units: 4, plan_name_ar: "نوفورابيد", plan_name_en: "NovoRapid" },
    prn_24h: [{ name_ar: "باراسيتامول", name_en: "Paracetamol", dosage: "1 g", count: 3, total_dose: "3 g", last_at: "2026-10-21T03:00:00Z" }],
    contacts: { emergency_number: "997" },
    family: [],
    generated_at: "2026-10-21T04:45:00Z",
    time_zone: "Asia/Riyadh",
  });
  await page.setViewportSize({ width: 375, height: 667 });
  for (const media of ["screen", "print"] as const) {
    await page.emulateMedia({ media });
    await page.setContent(html);
    const first = page.locator("main > *").first();
    await expect(first).toHaveAttribute("data-testid", "recent-doses");
    const thin = first.locator(".thinrow");
    await expect(thin).toContainText("Blood thinner");
    await expect(thin).toContainText("Apixaban 5 mg — last dose 20 Oct, 23:30 UTC+3 (8 h 15 min ago)");
    await expect(thin).toContainText("مميّع للدم");
    await expect(thin).toBeInViewport({ ratio: 1 });
    const size = await thin.locator(".en .v").evaluate((el) => ({ px: parseFloat(getComputedStyle(el).fontSize), weight: Number(getComputedStyle(el).fontWeight) }));
    expect(size.px, `${media} font size`).toBeGreaterThanOrEqual(14);
    expect(size.weight, `${media} bold`).toBeGreaterThanOrEqual(700);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  }
});
