import { test, expect, type Page, type BrowserContext } from "@playwright/test";

// Issue #55: the server's overdue-dose alert offers "Give as …"; tapping it logs
// the dose, and the alert clears on a sibling's phone too.
const people = {
  c1: { id: "c1", name: "Hussain", avatar_color: "#059669" },
  c2: { id: "c2", name: "Fatima", avatar_color: "#db2777" },
};

type Shared = { logged: Record<string, unknown>[]; queues: Map<Page, string[]> };

const alert = {
  id: "od1",
  kind: "dose_overdue",
  emergency: false,
  payload: { medication_id: "med_clop", name_ar: "كلوبيدوقريل", name_en: "Clopidogrel", dosage: "75 mg", slot: "morning", scheduled_date: "2026-10-27", slot_time: "08:00", critical: true },
  created_at: new Date().toISOString(),
};

async function phone(context: BrowserContext, baseURL: string, who: "c1" | "c2", shared: Shared, locale: string): Promise<Page> {
  await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL }]);
  const page = await context.newPage();
  shared.queues.set(page, []);
  page.on("dialog", (d) => {
    throw new Error(`unexpected dialog: ${d.message()}`);
  });
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
    if (path === "/alerts/unacked") return json(shared.logged.length ? [] : [alert]);
    if (path === "/medications/log" && req.method() === "POST") {
      shared.logged.push({ ...req.postDataJSON(), by: who });
      const ev = JSON.stringify({ type: "ALERT_RESOLVED", timestamp: new Date().toISOString(), actor: people[who], payload: { alert_id: "od1", resolved: true } });
      for (const q of shared.queues.values()) q.push(ev);
      return json({ status: "success", log_id: "l1", caregiver_name: people[who].name }, 201);
    }
    if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-27", schedule: [] });
    return json([]);
  });
  return page;
}

for (const locale of ["en", "ar"] as const) {
  test(`overdue blood thinner: Give as Hussain logs it and clears it on Fatima's phone (${locale})`, async ({ browser, baseURL }) => {
    const shared: Shared = { logged: [], queues: new Map() };
    const hussain = await phone(await browser.newContext({ viewport: { width: 375, height: 667 } }), baseURL!, "c1", shared, locale);
    const fatima = await phone(await browser.newContext({ viewport: { width: 375, height: 667 } }), baseURL!, "c2", shared, locale);
    await hussain.goto("/");
    await fatima.goto("/");

    for (const p of [hussain, fatima]) {
      const bar = p.getByTestId("alert-bar");
      await expect(bar).toHaveAttribute("data-kind", "dose_overdue");
      await expect(bar).toContainText(locale === "ar" ? "لم تُسجَّل جرعة" : "isn't logged");
      await expect(bar).toContainText("08:00");
      await expect(bar.locator('a[href^="tel:"]')).toHaveCount(0);
    }
    const give = hussain.getByTestId("give-dose");
    await expect(give).toContainText("Hussain");
    expect(await hussain.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await give.click();

    await expect(hussain.getByTestId("alert-bar")).toHaveCount(0);
    await expect(fatima.getByTestId("alert-bar")).toHaveCount(0, { timeout: 5000 });
    expect(shared.logged).toEqual([{ medication_id: "med_clop", slot: "morning", scheduled_date: "2026-10-27", status: "taken", by: "c1" }]);
  });
}
