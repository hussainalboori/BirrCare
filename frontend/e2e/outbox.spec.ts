import { test, expect, type Page, type BrowserContext } from "@playwright/test";

// Issue #16: offline taps are kept on the phone, shown honestly, and sent later.
const me = { id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669" };

type State = { online: boolean; log: null | Record<string, unknown>; posts: unknown[]; queues: Map<Page, string[]> };

async function device(context: BrowserContext, state: State, baseURL: string): Promise<Page> {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL }]);
  const page = await context.newPage();
  state.queues.set(page, []);
  page.on("dialog", (d) => {
    throw new Error(`unexpected dialog: ${d.message()}`);
  });
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) {
      const q = state.queues.get(page)!.splice(0);
      return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 200\nevent: connected\ndata: {}\n\n" + q.map((e) => `data: ${e}\n\n`).join("") });
    }
    if (path === "/auth/me") return json(me);
    if (path === "/medications/log" && req.method() === "POST") {
      if (!state.online) return route.abort("internetdisconnected");
      state.posts.push(req.postDataJSON());
      const now = new Date().toISOString();
      state.log = { id: "L1", medication_id: "m1", caregiver_id: "c1", slot: "morning", scheduled_date: "2026-10-20", taken_at: now, recorded_at: now, status: "taken", caregiver_name: me.name };
      const ev = JSON.stringify({ type: "MED_LOGGED", timestamp: now, actor: { id: me.id, name: me.name }, payload: { medication_id: "m1", slot: "morning", scheduled_date: "2026-10-20", status: "taken" } });
      for (const q of state.queues.values()) q.push(ev);
      return json({ status: "success", log_id: "L1", taken_at: now, caregiver_name: me.name }, 201);
    }
    if (path.startsWith("/medications/schedule")) {
      return json({
        date: "2026-10-20",
        schedule: [
          {
            slot: "morning",
            medication: { id: "m1", name_ar: "أسبرين", name_en: "Aspirin", dosage: "81 mg", frequency_slots: ["morning"], is_active: true, created_at: "2026-10-01T00:00:00Z" },
            ...(state.log ? { log: state.log } : {}),
          },
        ],
      });
    }
    return json([]);
  });
  await page.goto("/");
  return page;
}

test("offline tap → amber 'not sent', no alert; back online → green here and on another phone", async ({ browser, baseURL }) => {
  const state: State = { online: false, log: null, posts: [], queues: new Map() };
  const a = await device(await browser.newContext(), state, baseURL!);
  const b = await device(await browser.newContext(), state, baseURL!);
  const mark = (p: Page) => p.getByRole("button", { name: "Mark as Taken" });

  await mark(a).click();
  await expect(a.getByTestId("dose-queued")).toContainText("Saved on this phone — not sent yet");
  await expect(a.getByTestId("outbox-bar")).toContainText("1 saved on this phone, not sent yet");
  await expect(a.getByText("Taken Successfully")).toHaveCount(0); // never green while unsent
  expect(state.posts).toHaveLength(0);

  state.online = true;
  await a.evaluate(() => window.dispatchEvent(new Event("online")));
  await expect(a.getByTestId("dose-queued")).toHaveCount(0, { timeout: 3000 });
  await expect(mark(a)).toHaveCount(0, { timeout: 3000 });
  await expect(a.getByTestId("outbox-bar")).toHaveCount(0);
  expect(state.posts).toHaveLength(1);
  expect(state.posts[0]).toMatchObject({ medication_id: "m1", slot: "morning", status: "taken" });
  expect((state.posts[0] as { given_at?: string }).given_at).toBeTruthy(); // the time of the tap

  await expect(mark(b)).toHaveCount(0, { timeout: 5000 }); // the sibling's phone, via SSE
});

test("a dose waiting more than 30 minutes asks before it is sent", async ({ page, context, baseURL }) => {
  await page.clock.install({ time: new Date("2026-10-20T03:47:00+03:00") });
  const state: State = { online: false, log: null, posts: [], queues: new Map() };
  page.on("dialog", (d) => {
    throw new Error(`unexpected dialog: ${d.message()}`);
  });
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  const p = await device(context, state, baseURL!);
  await p.getByRole("button", { name: "Mark as Taken" }).click();
  await expect(p.getByTestId("dose-queued")).toBeVisible();

  await p.clock.fastForward("31:00");
  state.online = true;
  await p.evaluate(() => window.dispatchEvent(new Event("online")));
  const confirm = p.getByTestId("outbox-confirm");
  await expect(confirm).toBeVisible();
  await expect(confirm).toContainText("A sibling may have logged it since");
  await p.waitForTimeout(500);
  expect(state.posts).toHaveLength(0); // not sent automatically

  await confirm.getByRole("button", { name: "Send it" }).click();
  await expect.poll(() => state.posts.length).toBe(1);
});
