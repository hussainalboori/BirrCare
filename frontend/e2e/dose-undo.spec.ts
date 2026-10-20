import { test, expect, type Page, type BrowserContext } from "@playwright/test";

// Issue #8: tap → Undo → the card returns to unlogged on a second device via SSE.
// One in-memory "server" is shared by both browser contexts; each context's SSE
// stream drains its own queue and asks the browser to reconnect quickly.

const me = {
  id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669",
  preferred_lang: "en", is_active: true, created_at: "2026-10-01T00:00:00Z",
};

type ServerState = { active: null | { id: string; taken_at: string; recorded_at: string }; queues: Map<Page, string[]> };

function broadcast(state: ServerState, type: string, payload: object) {
  const ev = JSON.stringify({ type, timestamp: new Date().toISOString(), actor: { id: me.id, name: me.name, avatar_color: me.avatar_color }, payload });
  for (const q of state.queues.values()) q.push(ev);
}

async function openDevice(context: BrowserContext, state: ServerState, baseURL: string): Promise<Page> {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL }]);
  const page = await context.newPage();
  state.queues.set(page, []);
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const method = route.request().method();
    const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (path.startsWith("/events/stream")) {
      const queued = state.queues.get(page)!.splice(0);
      const body = "retry: 200\n" + 'event: connected\ndata: {"status":"connected"}\n\n' + queued.map((e) => `data: ${e}\n\n`).join("");
      return route.fulfill({ status: 200, contentType: "text/event-stream", body });
    }
    if (path === "/auth/me") return json(me);
    if (path === "/caregivers") return json([{ id: me.id, name: me.name, avatar_color: me.avatar_color }]);
    if (path === "/medications/log" && method === "POST") {
      const now = new Date().toISOString();
      state.active = { id: "L1", taken_at: now, recorded_at: now };
      broadcast(state, "MED_LOGGED", { medication_id: "m1", slot: "morning", scheduled_date: "2026-10-20", status: "taken" });
      return json({ status: "success", log_id: "L1", taken_at: now, caregiver_name: me.name, avatar_color: me.avatar_color }, 201);
    }
    if (path === "/medications/log/L1/void" && method === "POST") {
      state.active = null;
      broadcast(state, "MED_VOIDED", { medication_id: "m1", slot: "morning", scheduled_date: "2026-10-20", reason: "Undo" });
      return json({ status: "voided", log_id: "L1" });
    }
    if (path.startsWith("/medications/schedule")) {
      return json({
        date: "2026-10-20",
        schedule: [
          {
            slot: "morning",
            medication: { id: "m1", name_ar: "أسبرين", name_en: "Aspirin", dosage: "81 mg", frequency_slots: ["morning"], is_active: true, created_at: "2026-10-01T00:00:00Z" },
            ...(state.active
              ? { log: { ...state.active, medication_id: "m1", caregiver_id: me.id, slot: "morning", scheduled_date: "2026-10-20", status: "taken", caregiver_name: me.name } }
              : {}),
          },
        ],
      });
    }
    return json([]);
  });
  await page.goto("/");
  return page;
}

test("Undo on one device returns the card to unlogged on another via SSE", async ({ browser, baseURL }) => {
  const state: ServerState = { active: null, queues: new Map() };
  const ctxA = await browser.newContext();
  const ctxB = await browser.newContext();
  const a = await openDevice(ctxA, state, baseURL!);
  const b = await openDevice(ctxB, state, baseURL!);
  const markOn = (p: Page) => p.getByRole("button", { name: "Mark as Taken" });

  await expect(markOn(a)).toBeVisible();
  await expect(markOn(b)).toBeVisible();

  await markOn(a).click();
  const undo = a.getByRole("button", { name: /Undo/ });
  await expect(undo).toBeVisible();
  const box = await undo.boundingBox();
  expect(box!.height).toBeGreaterThanOrEqual(48);

  // B sees the dose arrive live, then sees it disappear after A's Undo.
  await expect(markOn(b)).toHaveCount(0, { timeout: 10_000 });
  await undo.click();
  await expect(markOn(a)).toBeVisible();
  await expect(markOn(b)).toBeVisible({ timeout: 10_000 });

  await ctxA.close();
  await ctxB.close();
});
