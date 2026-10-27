import { test, expect, type Page, type BrowserContext } from "@playwright/test";

// Issue #59: pause care for a hospital stay; every phone shows it; resuming asks
// whether the hospital changed any medicines.
const people = {
  c1: { id: "c1", name: "Hussain", avatar_color: "#059669" },
  c2: { id: "c2", name: "Fatima", avatar_color: "#db2777" },
};
type Shared = { pause: Record<string, unknown> | null; queues: Map<Page, string[]> };

async function phone(context: BrowserContext, baseURL: string, who: "c1" | "c2", shared: Shared, locale: string): Promise<Page> {
  await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL }]);
  const page = await context.newPage();
  shared.queues.set(page, []);
  page.on("dialog", (d) => {
    throw new Error(`unexpected dialog: ${d.message()}`);
  });
  const broadcast = (type: string, payload: unknown) => {
    const ev = JSON.stringify({ type, timestamp: new Date().toISOString(), actor: people[who], payload });
    for (const q of shared.queues.values()) q.push(ev);
  };
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) {
      const q = shared.queues.get(page)!.splice(0);
      return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 200\nevent: connected\ndata: {}\n\n" + q.map((e) => `data: ${e}\n\n`).join("") });
    }
    if (path === "/auth/me") return json({ ...people[who], role: "caregiver", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path === "/care/pause" && req.method() === "GET") return json({ paused: !!shared.pause, pause: shared.pause });
    if (path === "/care/pause" && req.method() === "POST") {
      const body = req.postDataJSON();
      shared.pause = { id: "p1", reason: body.reason, notes: body.notes, started_at: new Date().toISOString(), started_by_name: people[who].name };
      broadcast("CARE_PAUSED", shared.pause);
      return json(shared.pause, 201);
    }
    if (path === "/care/resume") {
      shared.pause = null;
      broadcast("CARE_RESUMED", { id: "p1" });
      return json({ id: "p1" });
    }
    if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-27", schedule: [] });
    return json([]);
  });
  return page;
}

for (const locale of ["en", "ar"] as const) {
  test(`pause for hospital -> banner on both phones -> resume asks about medicine changes (${locale})`, async ({ browser, baseURL }) => {
    const shared: Shared = { pause: null, queues: new Map() };
    const hussain = await phone(await browser.newContext({ viewport: { width: 375, height: 667 } }), baseURL!, "c1", shared, locale);
    const fatima = await phone(await browser.newContext({ viewport: { width: 375, height: 667 } }), baseURL!, "c2", shared, locale);
    await hussain.goto("/");
    await fatima.goto("/vitals");

    await hussain.getByTestId("pause-care").click();
    await hussain.getByLabel(locale === "ar" ? "ملاحظة (اختياري): المستشفى، الجناح…" : "Note (optional): hospital, ward…").fill("King Fahd, ward 3");
    await hussain.getByTestId("confirm-pause").click();

    for (const p of [hussain, fatima]) {
      const banner = p.getByTestId("care-paused-banner");
      await expect(banner).toBeVisible({ timeout: 5000 });
      await expect(banner).toContainText(locale === "ar" ? "في المستشفى" : "In hospital");
      await expect(banner).toContainText(locale === "ar" ? "التذكيرات متوقفة" : "reminders paused");
      await expect(banner).toContainText("King Fahd, ward 3");
      expect(await p.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    }
    await expect(hussain.getByTestId("pause-care")).toHaveCount(0);

    await fatima.getByTestId("resume-care").click();
    const prompt = fatima.getByTestId("care-resumed");
    await expect(prompt).toContainText(locale === "ar" ? "هل غيّر المستشفى أي دواء؟" : "Did the hospital change any medicines?");
    await expect(prompt.getByRole("button", { name: locale === "ar" ? "نعم، راجع قائمة الأدوية" : "Yes, review the list" })).toBeVisible();
    await expect(hussain.getByTestId("care-paused-banner")).toHaveCount(0, { timeout: 5000 });
    await expect(fatima.getByTestId("care-paused-banner")).toHaveCount(0);
  });
}
