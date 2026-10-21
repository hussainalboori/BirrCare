import { test, expect, type Page, type BrowserContext } from "@playwright/test";

// Issue #28: one tap on "150 ml" logs the drink and the other phone's ring
// updates live (SSE), without a reload.
type Shared = { totalMl: number; posts: Record<string, unknown>[]; queues: Map<Page, string[]> };

async function phone(context: BrowserContext, baseURL: string, shared: Shared, me: { id: string; name: string }): Promise<Page> {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL }]);
  const page = await context.newPage();
  shared.queues.set(page, []);
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, code = 200) => route.fulfill({ status: code, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) {
      const q = shared.queues.get(page)!.splice(0);
      return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 200\nevent: connected\ndata: {}\n\n" + q.map((e) => `data: ${e}\n\n`).join("") });
    }
    if (path === "/auth/me") return json({ ...me, role: "caregiver", avatar_color: "#059669", preferred_lang: "en", is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path === "/care/status") {
      const kind = url.searchParams.get("kind") ?? "reposition";
      const base = { kind, interval_minutes: 120, is_overdue_warning: false, is_overdue_critical: false, last_at: null, minutes_since: null };
      if (kind === "fluid") {
        return json({ ...base, target_ml: 1500, today_total_ml: shared.totalMl, today_thickened_ml: 0, progress_percent: Math.floor((shared.totalMl / 1500) * 100), is_low_fluid: false });
      }
      if (kind === "bowel") return json({ ...base, last_at: "2026-10-21T08:00:00Z", days_since_bowel: 0, is_bowel_overdue: false });
      return json(base);
    }
    if (path === "/care/events" && req.method() === "POST") {
      const body = req.postDataJSON();
      shared.posts.push(body);
      shared.totalMl += Number(body.value);
      const at = new Date().toISOString();
      const ev = JSON.stringify({ type: "CARE_EVENT_LOGGED", timestamp: at, actor: me, payload: { kind: "fluid", value: body.value } });
      for (const q of shared.queues.values()) q.push(ev);
      return json({ id: "e1", kind: "fluid", value: body.value, caregiver_id: me.id, created_at: at }, 201);
    }
    if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
    return json([]);
  });
  await page.goto("/");
  return page;
}

test("one tap on 150 ml logs it and the other phone's ring updates live", async ({ browser, baseURL }) => {
  const shared: Shared = { totalMl: 300, posts: [], queues: new Map() };
  const a = await phone(await browser.newContext(), baseURL!, shared, { id: "c1", name: "Hussain" });
  const b = await phone(await browser.newContext(), baseURL!, shared, { id: "c2", name: "Fatima" });
  await expect(a.getByTestId("fluid-progress-ring")).toContainText("20%");
  await expect(b.getByTestId("fluid-progress-ring")).toContainText("20%");

  await a.getByTestId("fluid-button-150").click(); // one tap
  await expect.poll(() => shared.posts.length).toBe(1);
  expect(shared.posts[0]).toMatchObject({ kind: "fluid", value: "150" });
  expect(typeof shared.posts[0].client_request_id).toBe("string");

  await expect(a.getByTestId("fluid-progress-ring")).toContainText("30%");
  await expect(a.getByTestId("fluid-bowel-card")).toContainText("450 / 1500 ml");
  // Fatima's phone, live via SSE (no reload).
  await expect(b.getByTestId("fluid-progress-ring")).toContainText("30%", { timeout: 5000 });
  await expect(b.getByTestId("fluid-bowel-card")).toContainText("450 / 1500 ml");
});
