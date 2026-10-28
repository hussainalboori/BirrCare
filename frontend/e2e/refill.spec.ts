import { test, expect, type Page, type BrowserContext } from "@playwright/test";

// Issue #65: "I'll get it" on one phone shows "Hussain is getting it" on the
// other, live, and the low-stock alert goes quiet on both.
const people = {
  c1: { id: "c1", name: "Hussain", avatar_color: "#059669" },
  c2: { id: "c2", name: "Fatima", avatar_color: "#db2777" },
};

type Shared = { claimedBy: "" | "c1" | "c2"; queues: Map<Page, string[]>; claims: number };

const alert = {
  id: "ls1",
  kind: "low_stock",
  emergency: false,
  payload: { medication_id: "med_clop", name_ar: "كلوبيدوقريل", name_en: "Clopidogrel", days_left: 2, stock_units: 2, blood_thinner: true },
  created_at: new Date().toISOString(),
};

function stock(shared: Shared) {
  const due = new Date(Date.now() + 26 * 3600_000).toISOString();
  return [
    {
      medication_id: "med_clop", name_ar: "كلوبيدوقريل", name_en: "Clopidogrel", dosage: "75 mg", stock_units: 2, units_per_dose: 1, doses_per_day: 1,
      days_left: 2, refill_threshold_days: 4, low: true, blood_thinner: true, needs_prescription: true, pharmacy_name: "Al-Nahdi", pharmacy_phone: "+966110000000",
      refill: {
        id: "t1", medication_id: "med_clop", created_at: new Date().toISOString(),
        ...(shared.claimedBy ? { claimed_by: shared.claimedBy, claimed_by_name: people[shared.claimedBy].name, claimed_at: new Date().toISOString(), due_by: due } : {}),
      },
    },
  ];
}

async function phone(context: BrowserContext, baseURL: string, who: "c1" | "c2", shared: Shared, locale: string): Promise<Page> {
  await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL }]);
  const page = await context.newPage();
  shared.queues.set(page, []);
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) {
      const q = shared.queues.get(page)!.splice(0);
      return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 200\nevent: connected\ndata: {}\n\n" + q.map((e) => `data: ${e}\n\n`).join("") });
    }
    if (path === "/auth/me") return json({ ...people[who], role: "caregiver", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path === "/caregivers") return json(Object.values(people));
    if (path === "/alerts/unacked") return json(shared.claimedBy ? [] : [alert]);
    if (path === "/medications/stock") return json(stock(shared));
    if (path === "/medications/med_clop/refill/claim" && req.method() === "POST") {
      shared.claimedBy = who;
      shared.claims++;
      const ev = JSON.stringify({
        type: "REFILL_CLAIMED", timestamp: new Date().toISOString(), actor: people[who],
        payload: { medication_id: "med_clop", claimed_by: who, claimed_by_name: people[who].name, alert_id: "t1" },
      });
      for (const q of shared.queues.values()) q.push(ev);
      return json(stock(shared)[0].refill);
    }
    if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-27", schedule: [] });
    return json([]);
  });
  return page;
}

for (const locale of ["en", "ar"] as const) {
  test(`I'll get it on Hussain's phone shows on Fatima's (${locale})`, async ({ browser, baseURL }) => {
    const shared: Shared = { claimedBy: "", queues: new Map(), claims: 0 };
    const hussain = await phone(await browser.newContext({ viewport: { width: 375, height: 740 } }), baseURL!, "c1", shared, locale);
    const fatima = await phone(await browser.newContext({ viewport: { width: 375, height: 740 } }), baseURL!, "c2", shared, locale);
    await hussain.goto("/");
    await fatima.goto("/");

    for (const p of [hussain, fatima]) {
      await expect(p.getByTestId("alert-bar")).toHaveAttribute("data-kind", "low_stock");
      const card = p.getByTestId("low-stock");
      await expect(card.getByTestId("needs-prescription")).toContainText(locale === "ar" ? "وصفة جديدة" : "new prescription");
      await expect(card.getByTestId("refill-call-pharmacy")).toHaveAttribute("href", "tel:+966110000000");
      await expect(card.getByTestId("refill-claim")).toBeVisible();
    }
    expect(await hussain.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

    await hussain.getByTestId("alert-refill-claim").click();
    expect(shared.claims).toBe(1);
    await expect(hussain.getByTestId("alert-bar")).toHaveCount(0);
    await expect(hussain.getByTestId("refill-owner")).toContainText(locale === "ar" ? "أنت ستشتريه" : "You're getting it");

    // Fatima's phone, live: the alert goes quiet and she sees who is getting it, by when.
    await expect(fatima.getByTestId("alert-bar")).toHaveCount(0, { timeout: 5000 });
    const owner = fatima.getByTestId("refill-owner");
    await expect(owner).toContainText(locale === "ar" ? "Hussain سيشتريه" : "Hussain is getting it", { timeout: 5000 });
    await expect(owner).toContainText(locale === "ar" ? "قبل" : "by");
    await expect(fatima.getByTestId("refill-claim")).toHaveCount(0);
    await expect(fatima.getByTestId("refill-takeover")).toBeVisible();
    expect(await fatima.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
}

test("Already bought on an alert opens the new-box keypad", async ({ browser, baseURL }) => {
  const shared: Shared = { claimedBy: "", queues: new Map(), claims: 0 };
  const page = await phone(await browser.newContext({ viewport: { width: 375, height: 740 } }), baseURL!, "c1", shared, "en");
  await page.goto("/");
  await page.getByTestId("alert-refill-bought").click();
  await expect(page.getByRole("dialog", { name: "New box" })).toBeVisible();
});
