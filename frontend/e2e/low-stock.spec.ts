import { test, expect, type Page, type BrowserContext } from "@playwright/test";

// Issue #20: "Clopidogrel: about 3 days left" on Home; Refilled → keypad → 30 →
// the card disappears on every phone, live.
type Shared = { units: number; refills: unknown[]; queues: Map<Page, string[]> };

const status = (s: Shared) => {
  const days = Math.floor((s.units / 1) * 10) / 10;
  return { medication_id: "m_clop", name_ar: "كلوبيدوقريل", name_en: "Clopidogrel", dosage: "75 mg", stock_units: s.units, units_per_dose: 1, doses_per_day: 1, days_left: days, refill_threshold_days: 4, low: days < 4, blood_thinner: true };
};

async function phone(context: BrowserContext, baseURL: string, shared: Shared, locale: "ar" | "en"): Promise<Page> {
  await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL }]);
  const page = await context.newPage();
  shared.queues.set(page, []);
  page.on("dialog", (d) => {
    throw new Error(`unexpected dialog: ${d.message()}`);
  });
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, code = 200) => route.fulfill({ status: code, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) {
      const q = shared.queues.get(page)!.splice(0);
      return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 200\nevent: connected\ndata: {}\n\n" + q.map((e) => `data: ${e}\n\n`).join("") });
    }
    if (path === "/auth/me") return json({ id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path === "/medications/stock") return json([status(shared)]);
    if (path === "/medications/m_clop/stock" && req.method() === "POST") {
      const body = req.postDataJSON();
      shared.refills.push(body);
      shared.units += body.units;
      const ev = JSON.stringify({ type: "STOCK_UPDATED", timestamp: new Date().toISOString(), actor: { id: "c1", name: "Hussain" }, payload: { stock: status(shared) } });
      for (const q of shared.queues.values()) q.push(ev);
      return json(status(shared));
    }
    if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
    return json([]);
  });
  await page.goto("/");
  return page;
}

test("Refilled → keypad → 30 → the warning disappears on both phones", async ({ browser, baseURL }) => {
  const shared: Shared = { units: 3, refills: [], queues: new Map() };
  const a = await phone(await browser.newContext(), baseURL!, shared, "en");
  const b = await phone(await browser.newContext(), baseURL!, shared, "ar");

  const cardA = a.getByTestId("low-stock");
  await expect(cardA).toContainText("Clopidogrel: about 3 days left");
  await expect(cardA).toContainText("Blood thinner");
  await expect(b.getByTestId("low-stock")).toContainText("يكفي نحو 3 أيام");

  await cardA.getByRole("button", { name: "Already bought" }).click(); // "Refilled" before issue #65
  const pad = a.getByRole("dialog", { name: "New box" });
  await pad.getByRole("button", { name: "3", exact: true }).click();
  await pad.getByRole("button", { name: "0", exact: true }).click();
  await expect(pad.getByTestId("refill-units")).toHaveText("30");
  await pad.getByRole("button", { name: "Save" }).click();

  expect(shared.refills).toEqual([{ action: "refill", units: 30 }]);
  await expect(cardA).toHaveCount(0);
  await expect(b.getByTestId("low-stock")).toHaveCount(0, { timeout: 5000 }); // the other phone, via SSE
});
