import { test, expect, type Page, type BrowserContext } from "@playwright/test";

// Issue #27: one tap on "Right side" logs a repositioning and the other phone
// shows it live (SSE), without a reload.
type Shared = { last: null | { value: string; at: string; by: string }; posts: Record<string, unknown>[]; queues: Map<Page, string[]> };

async function phone(context: BrowserContext, baseURL: string, shared: Shared, me: { id: string; name: string }): Promise<Page> {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL }]);
  const page = await context.newPage();
  shared.queues.set(page, []);
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, code = 200) => route.fulfill({ status: code, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) {
      const q = shared.queues.get(page)!.splice(0);
      return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 200\nevent: connected\ndata: {}\n\n" + q.map((e) => `data: ${e}\n\n`).join("") });
    }
    if (path === "/auth/me") return json({ ...me, role: "caregiver", avatar_color: "#059669", preferred_lang: "en", is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path === "/care/status") {
      const l = shared.last;
      return json({
        kind: "reposition", interval_minutes: 120, is_overdue_warning: false, is_overdue_critical: false,
        last_at: l?.at ?? null, last_value: l?.value ?? null, last_caregiver_name: l?.by ?? null, minutes_since: l ? 0 : null,
        next_due_at: l ? new Date(Date.parse(l.at) + 120 * 60_000).toISOString() : null,
      });
    }
    if (path === "/care/events" && req.method() === "POST") {
      const body = req.postDataJSON();
      shared.posts.push(body);
      const at = new Date().toISOString();
      shared.last = { value: body.value, at, by: me.name };
      const ev = JSON.stringify({ type: "CARE_EVENT_LOGGED", timestamp: at, actor: me, payload: { kind: "reposition", value: body.value } });
      for (const q of shared.queues.values()) q.push(ev);
      return json({ id: "e1", kind: "reposition", value: body.value, caregiver_id: me.id, created_at: at }, 201);
    }
    if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
    return json([]);
  });
  await page.goto("/vitals");
  return page;
}

test("one tap on Right side logs it and the other phone updates live", async ({ browser, baseURL }) => {
  const shared: Shared = { last: null, posts: [], queues: new Map() };
  const a = await phone(await browser.newContext(), baseURL!, shared, { id: "c1", name: "Hussain" });
  const b = await phone(await browser.newContext(), baseURL!, shared, { id: "c2", name: "Fatima" });
  await expect(a.getByTestId("reposition-card")).toBeVisible();
  await expect(b.getByTestId("reposition-card")).toBeVisible();

  await a.getByTestId("reposition-right").click(); // one tap
  await expect.poll(() => shared.posts.length).toBe(1);
  expect(shared.posts[0]).toMatchObject({ kind: "reposition", value: "right" });
  expect(typeof shared.posts[0].client_request_id).toBe("string");

  await expect(a.getByTestId("reposition-status")).toContainText("Hussain");
  // Fatima's phone, live via SSE (no reload).
  await expect(b.getByTestId("reposition-status")).toContainText("Hussain", { timeout: 5000 });
  await expect(b.getByTestId("reposition-status")).toContainText(/right/i);
});
