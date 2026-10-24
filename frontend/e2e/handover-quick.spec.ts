import { test, expect, type Page, type BrowserContext } from "@playwright/test";

// Issue #18: "Ate half, needs rest, passing to Fatima" in 4 taps, and Fatima
// gets an "Accept duty" alert.
const people = {
  c1: { id: "c1", name: "Hussain", avatar_color: "#059669" },
  c2: { id: "c2", name: "Fatima", avatar_color: "#db2777" },
};

type Shared = { notes: Record<string, unknown>[]; passes: Record<string, unknown>[]; accepted: string[]; queues: Map<Page, string[]> };

async function phone(context: BrowserContext, baseURL: string, who: "c1" | "c2", shared: Shared, locale = "en"): Promise<Page> {
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
    const me = people[who];
    if (path.startsWith("/events/stream")) {
      const q = shared.queues.get(page)!.splice(0);
      return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 200\nevent: connected\ndata: {}\n\n" + q.map((e) => `data: ${e}\n\n`).join("") });
    }
    if (path === "/auth/me") return json({ ...me, role: "caregiver", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path === "/caregivers") return json(Object.values(people));
    if (path === "/shifts") return json([{ id: "s24", name_ar: "يوم", name_en: "Day", start_time: "08:00", end_time: "08:00", duration_hours: 24, color: "#2563eb", display_order: 0, is_active: true }]);
    if (path === "/shifts/current-duty") {
      const holder = shared.passes.length ? "c2" : "c1";
      return json({ is_on_duty: true, current_time: "14:02", date: "2026-10-21", active_assignment: { id: "a1", shift_id: "s24", caregiver_id: holder, date: "2026-10-21" } });
    }
    if (path === "/shifts/handover" && req.method() === "POST") {
      if (who !== "c1" || shared.passes.length) return json({ error: "NOT_ON_DUTY" }, 403);
      const body = req.postDataJSON();
      shared.passes.push(body);
      const now = new Date().toISOString();
      shared.notes.unshift({ id: "n1", caregiver_id: "c1", caregiver_name: "Hussain", avatar_color: "#059669", created_at: now, shift_type: "s24", summary: body.summary, urgent_flag: false, tags: body.tags, duty_handover_id: "d1", passed_to_id: "c2", passed_to_name: "Fatima" });
      const ev = JSON.stringify({ type: "DUTY_PASSED", timestamp: now, actor: { id: "c1", name: "Hussain" }, payload: { duty_handover_id: "d1", to_caregiver_id: "c2", alert_id: "al1" } });
      for (const q of shared.queues.values()) q.push(ev);
      return json({ status: "success", duty_handover_id: "d1", note_id: "n1", to_name: "Fatima" }, 201);
    }
    if (path === "/shifts/handover/d1/accept") {
      shared.accepted.push(who);
      return json({ status: "success", accepted_at: new Date().toISOString() });
    }
    if (path === "/alerts/unacked") {
      if (who === "c2" && shared.passes.length && !shared.accepted.length) {
        return json([{ id: "al1", kind: "duty_handover", created_by: "c1", created_by_name: "Hussain", created_at: new Date().toISOString(), payload: { duty_handover_id: "d1", tags: shared.passes[0].tags, summary: "" } }]);
      }
      return json([]);
    }
    if (path.startsWith("/handovers")) {
      if (req.method() === "POST") return json({ id: "x" }, 201);
      return json(shared.notes);
    }
    if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
    return json([]);
  });
  return page;
}

test("Ate half + Needs rest + Pass to Fatima + Send: 4 taps, under 15 s; Fatima accepts", async ({ browser, baseURL }) => {
  const shared: Shared = { notes: [], passes: [], accepted: [], queues: new Map() };
  const hussain = await phone(await browser.newContext(), baseURL!, "c1", shared);
  const fatima = await phone(await browser.newContext(), baseURL!, "c2", shared);
  await fatima.goto("/report");
  await hussain.goto("/handovers");
  const form = hussain.getByTestId("handover-form");
  await expect(form.getByRole("button", { name: "To Fatima" })).toBeVisible();

  const started = Date.now();
  let taps = 0;
  const tap = async (name: string | RegExp) => {
    taps++;
    await form.getByRole("button", { name }).click();
  };
  await tap("Ate half");
  await tap("Needs rest");
  await tap("To Fatima");
  await tap("Send and pass duty to Fatima");
  await expect(form.getByRole("status")).toContainText("Duty passed to Fatima");
  expect(taps).toBe(4);
  expect(Date.now() - started).toBeLessThan(15_000);
  expect(shared.passes).toEqual([{ to_caregiver_id: "c2", tags: ["ate_half", "needs_rest"], summary: "", urgent_flag: false }]);

  // The note shows its chips and who has duty now; Hussain can't pass it again.
  await expect(hussain.getByText("Duty passed to Fatima — waiting to be accepted")).toBeVisible();
  await expect(form.getByRole("button", { name: "To Fatima" })).toHaveCount(0);

  // Fatima: an "Accept duty" alert with the tags, live.
  const accept = fatima.getByTestId("accept-duty");
  await expect(accept).toBeVisible({ timeout: 5000 });
  await expect(fatima.getByTestId("alert-bar")).toContainText("Duty was passed to you");
  await expect(fatima.getByTestId("alert-bar")).toContainText("Ate half · Needs rest");
  await accept.click();
  await expect(accept).toHaveCount(0);
  expect(shared.accepted).toEqual(["c2"]);

  // Report: the handover is in the day's list, marked as a handover.
  await fatima.reload();
  const item = fatima.getByTestId("report-item").first();
  await expect(item).toContainText("Hussain");
  await expect(item).toContainText("Shift handover");
  await expect(item).toContainText("Ate half");
  await expect(item).toContainText("Needs rest");
});

test("tags only is enough for a plain note; urgent tags mark it urgent; Arabic labels", async ({ browser, baseURL }) => {
  const shared: Shared = { notes: [], passes: [], accepted: [], queues: new Map() };
  const page = await phone(await browser.newContext(), baseURL!, "c1", shared, "ar");
  const posted: unknown[] = [];
  page.on("request", (r) => {
    if (r.url().endsWith("/api/v1/handovers") && r.method() === "POST") posted.push(r.postDataJSON());
  });
  await page.goto("/handovers");
  const form = page.getByTestId("handover-form");
  await form.getByRole("button", { name: "تشعر بألم" }).click();
  await expect(form).toContainText("ستُعلَّم الملاحظة كعاجلة");
  await form.getByRole("button", { name: "احفظ باسم Hussain" }).click();
  await expect.poll(() => posted.length).toBe(1);
  expect(posted[0]).toMatchObject({ tags: ["pain"], summary: "", urgent_flag: true });
});

test("nothing tapped or typed: a clear message, no request", async ({ browser, baseURL }) => {
  const shared: Shared = { notes: [], passes: [], accepted: [], queues: new Map() };
  const page = await phone(await browser.newContext(), baseURL!, "c1", shared);
  let posts = 0;
  page.on("request", (r) => r.method() === "POST" && r.url().includes("/handovers") && posts++);
  await page.goto("/handovers");
  await page.getByTestId("handover-form").getByRole("button", { name: "Save as Hussain" }).click();
  await expect(page.getByTestId("handover-form").getByRole("alert")).toContainText("Tap at least one item or write a note");
  expect(posts).toBe(0);
});
