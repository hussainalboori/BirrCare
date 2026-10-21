import { test, expect, type Page } from "@playwright/test";

// Issue #17: on a shared tablet the person logging is unmissable, and switching
// is one tap on a name plus 4 digits.
const siblings = [
  { id: "c1", name: "Hussain Ali", avatar_color: "#059669" },
  { id: "c2", name: "Fatima", avatar_color: "#db2777" },
  { id: "c3", name: "Omar", avatar_color: "#2563eb" },
];
const names: Record<string, Record<string, string>> = { en: { c1: "Hussain", c2: "Fatima" }, ar: { c1: "Hussain", c2: "Fatima" } };

type State = { me: string; shared: boolean; pins: unknown[]; writes: number };

async function mockApi(page: Page, state: State) {
  page.on("dialog", (d) => {
    throw new Error(`unexpected dialog: ${d.message()}`);
  });
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    const me = () => {
      const s = siblings.find((c) => c.id === state.me)!;
      return { ...s, role: "caregiver", preferred_lang: "ar", is_active: true, created_at: "2026-10-01T00:00:00Z", shared_device: state.shared, idle_minutes: state.shared ? 20 : undefined };
    };
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
    if (path === "/auth/me") return json(me());
    if (path === "/caregivers") return json(siblings);
    if (path === "/auth/verify-pin") {
      const body = req.postDataJSON();
      state.pins.push(body);
      if (body.pin !== "2580") return json({ error: "INCORRECT_PIN" }, 401);
      state.me = body.caregiver_id;
      return json({ status: "success", caregiver: me() });
    }
    if (path === "/medications/log" && req.method() === "POST") {
      state.writes++;
      return json({ status: "success", log_id: "L1", taken_at: new Date().toISOString(), caregiver_name: me().name }, 201);
    }
    if (path.startsWith("/medications/schedule")) {
      return json({
        date: "2026-10-21",
        schedule: [{ slot: "morning", medication: { id: "m1", name_ar: "أسبرين", name_en: "Aspirin", dosage: "81 mg", frequency_slots: ["morning"], is_active: true, created_at: "2026-10-01T00:00:00Z" } }],
      });
    }
    return json([]);
  });
}

for (const locale of ["ar", "en"] as const) {
  test(`${locale}: at 375px the active name is on screen and on the dose button`, async ({ page, context, baseURL }) => {
    await page.setViewportSize({ width: 375, height: 740 });
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    await mockApi(page, { me: "c1", shared: false, pins: [], writes: 0 });
    await page.goto("/");

    const active = page.getByTestId("sibling-c1");
    await expect(active).toHaveAttribute("aria-current", "true");
    await expect(active).toContainText(names[locale].c1);
    await expect(active).toBeInViewport({ ratio: 1 });
    const give = page.getByRole("button", { name: locale === "ar" ? "أعطِ باسم Hussain" : "Give as Hussain" });
    await expect(give).toBeVisible();
    // Avatars are at least 56px.
    const box = await active.locator("span").first().boundingBox();
    expect(box!.width).toBeGreaterThanOrEqual(56);
  });
}

test("tap a sibling → keypad preselected → 4 digits → everything says Fatima (5 taps)", async ({ page, context, baseURL }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  const state: State = { me: "c1", shared: false, pins: [], writes: 0 };
  await mockApi(page, state);
  await page.goto("/");
  await expect(page.getByRole("button", { name: "Give as Hussain" })).toBeVisible();

  let taps = 0;
  const tap = async (l: ReturnType<Page["getByRole"]>) => {
    taps++;
    await l.click();
  };
  await tap(page.getByTestId("sibling-c2"));
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("button", { name: /Fatima/ })).toHaveAttribute("aria-pressed", "true");
  for (const d of "2580") await tap(dialog.getByRole("button", { name: d, exact: true }));

  await expect(dialog).toHaveCount(0);
  expect(taps).toBeLessThanOrEqual(5);
  expect(state.pins).toEqual([{ caregiver_id: "c2", pin: "2580" }]);
  await expect(page.locator("header")).toContainText("Fatima");
  await expect(page.getByTestId("sibling-c2")).toHaveAttribute("aria-current", "true");
  await expect(page.getByRole("button", { name: "Give as Fatima" })).toBeVisible();
});

test("shared tablet: after a save, 'Still Hussain?' for 5 seconds with a Switch button", async ({ page, context, baseURL }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  const state: State = { me: "c1", shared: true, pins: [], writes: 0 };
  await mockApi(page, state);
  await page.goto("/");
  await page.getByRole("button", { name: "Give as Hussain" }).click();
  const chip = page.getByTestId("still-you");
  await expect(chip).toContainText("Still Hussain?");
  await chip.getByRole("button", { name: "Switch" }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(chip).toHaveCount(0);
});

test("shared tablet chip disappears by itself; a personal phone never shows it", async ({ page, context, baseURL }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  const state: State = { me: "c1", shared: true, pins: [], writes: 0 };
  await mockApi(page, state);
  await page.goto("/");
  await page.getByRole("button", { name: "Give as Hussain" }).click();
  await expect(page.getByTestId("still-you")).toBeVisible();
  await expect(page.getByTestId("still-you")).toHaveCount(0, { timeout: 7000 });

  state.shared = false;
  await page.reload();
  await page.getByRole("button", { name: "Give as Hussain" }).click();
  await expect(page.getByText(/Taken|Given by/).first()).toBeVisible();
  await page.waitForTimeout(500);
  await expect(page.getByTestId("still-you")).toHaveCount(0);
});
