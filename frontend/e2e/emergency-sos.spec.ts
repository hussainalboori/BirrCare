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
