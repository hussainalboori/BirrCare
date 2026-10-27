import { test, expect, type Page, type Locator } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

// Issue #11: in a blood-pressure crisis the call buttons appear at once, work
// offline and without configuration, and reach siblings on every page.

type Opts = {
  role?: "caregiver" | "admin";
  contacts?: Record<string, string> | "offline";
  sse?: object[];
  alerts?: object[]; // server-side unacked alerts (issue #12)
};

async function mockApi(page: Page, opts: Opts = {}) {
  const me = { id: "c1", name: "Hussain", role: opts.role ?? "caregiver", avatar_color: "#059669", preferred_lang: "en", is_active: true, created_at: "2026-10-01T00:00:00Z" };
  const posts: string[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (req.method() === "POST") posts.push(path);
    if (path.startsWith("/events/stream")) {
      const events = (opts.sse ?? []).map((e) => `data: ${JSON.stringify(e)}\n\n`).join("");
      return route.fulfill({ status: 200, contentType: "text/event-stream", body: 'retry: 60000\nevent: connected\ndata: {}\n\n' + events });
    }
    if (path === "/auth/me") return json(me);
    if (path === "/contacts") return opts.contacts === "offline" ? route.abort("internetdisconnected") : json(opts.contacts ?? {});
    if (path === "/alerts/unacked") return json(opts.alerts ?? []);
    if (/^\/alerts\/[^/]+\/ack$/.test(path)) {
      opts.alerts = [];
      return json({ status: "acknowledged" });
    }
    if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-20", schedule: [] });
    return json([]);
  });
  return posts;
}

async function expectTapTargets(scope: Locator) {
  for (const el of await scope.locator("a, button").all()) {
    const box = await el.boundingBox();
    expect(box, await el.innerText()).not.toBeNull();
    expect(box!.height, `${await el.innerText()} height`).toBeGreaterThanOrEqual(48);
    expect(box!.width, `${await el.innerText()} width`).toBeGreaterThanOrEqual(48);
  }
}

async function expectContrast(page: Page, selector: string) {
  const results = await new AxeBuilder({ page }).include(selector).withRules(["color-contrast"]).analyze();
  expect(results.violations.map((v) => v.nodes.map((n) => n.failureSummary)).flat()).toEqual([]);
}

test.beforeEach(async ({ context, baseURL }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
});

test("offline: typing 185/115 in Quick BP shows both call links before any request is sent", async ({ page }) => {
  // This phone saw the contacts before; now the server can't be reached.
  await page.addInitScript(() => localStorage.setItem("birrcare_contacts", JSON.stringify({ emergency_number: "997", doctor_name: "Dr. Salem", doctor_phone: "+966500000000" })));
  const posts = await mockApi(page, { contacts: "offline" });
  await page.goto("/vitals?quick=bp");

  const systolic = page.getByLabel("Systolic (mmHg)", { exact: true });
  await expect(systolic).toBeFocused();
  await page.keyboard.type("185");
  // Auto-advance: three digits move to diastolic.
  await expect(page.getByLabel("Diastolic (mmHg)", { exact: true })).toBeFocused();
  await page.keyboard.type("115");

  const crisis = page.getByTestId("live-crisis");
  await expect(crisis.locator('a[href="tel:997"]')).toBeVisible();
  await expect(crisis.locator('a[href="tel:+966500000000"]')).toBeVisible();
  expect(posts).toEqual([]);

  // Calls come first: both links are on screen without scrolling.
  for (const link of [crisis.locator('a[href="tel:997"]'), crisis.locator('a[href="tel:+966500000000"]')]) {
    await expect(link).toBeInViewport();
  }
  await expectTapTargets(crisis);
});

test("no EMERGENCY_NUMBER: the call button still renders from the fallback, and admins see a setup banner", async ({ page }) => {
  await mockApi(page, { role: "admin", contacts: {} });
  await page.goto("/vitals?quick=bp");
  await expect(page.getByLabel("Systolic (mmHg)", { exact: true })).toBeFocused();
  await page.keyboard.type("190");
  await page.keyboard.type("100");
  const fallback = page.getByTestId("live-crisis").locator('a[href^="tel:"]');
  await expect(fallback).toHaveCount(1);
  // The server's EMERGENCY_NUMBER (999 in the test server), sent inside the page,
  // not a number compiled into the build.
  await expect(fallback).toHaveAttribute("href", "tel:999");
  await expect(page.getByTestId("emergency-setup-banner")).toBeVisible();
});

test("a caregiver (not admin) never sees the setup banner", async ({ page }) => {
  await mockApi(page, { role: "caregiver", contacts: {} });
  await page.goto("/");
  await expect(page.getByRole("heading").first()).toBeVisible();
  await expect(page.getByTestId("emergency-setup-banner")).toHaveCount(0);
});

for (const scheme of ["light", "dark"] as const) {
  test(`sibling on / gets the crisis bar with call buttons in view at 375×667 (${scheme})`, async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.emulateMedia({ colorScheme: scheme });
    await mockApi(page, {
      contacts: { emergency_number: "997", doctor_name: "Dr. Salem", doctor_phone: "+966500000000" },
      sse: [
        {
          type: "VITAL_LOGGED",
          timestamp: new Date().toISOString(),
          actor: { id: "c2", name: "Fatima", avatar_color: "#2563eb" },
          payload: { id: "v1", alert_id: "a1", vital_type: "bp", systolic: 185, diastolic: 115, bp_status: "CRISIS", recorded_at: new Date().toISOString() },
        },
      ],
      alerts: [{ id: "a1", kind: "bp_crisis", created_by_name: "Fatima", payload: { systolic: 185, diastolic: 115 }, created_at: new Date().toISOString() }],
    });
    await page.goto("/");

    const bar = page.getByTestId("crisis-bar");
    await expect(bar).toBeVisible();
    await expect(bar).toContainText("185/115");
    await expect(bar).toContainText("Fatima");
    for (const href of ["tel:997", "tel:+966500000000"]) {
      await expect(bar.locator(`a[href="${href}"]`)).toBeInViewport({ ratio: 1 });
    }
    await expectTapTargets(bar);
    await expectContrast(page, '[data-testid="crisis-bar"]');

    // Acknowledged on the server: gone, and it stays gone after a reload.
    await bar.getByRole("button", { name: "Seen" }).click();
    await expect(bar).toHaveCount(0);
    await page.reload();
    await expect(page.getByRole("heading").first()).toBeVisible();
    await expect(page.getByTestId("crisis-bar")).toHaveCount(0);
  });
}

for (const scheme of ["light", "dark"] as const) {
  test(`live crisis view meets contrast (${scheme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await mockApi(page, { contacts: { emergency_number: "997", doctor_phone: "+966500000000" } });
    await page.goto("/vitals?quick=bp");
    await expect(page.getByLabel("Systolic (mmHg)", { exact: true })).toBeFocused();
    await page.keyboard.type("200");
    await page.keyboard.type("125");
    await expect(page.getByTestId("live-crisis")).toBeVisible();
    await expectContrast(page, '[data-testid="live-crisis"]');
  });
}

// Issue #54: a low sugar logged by a sibling turns this phone's screen into the red
// crisis bar with the 15-15 steps, a recheck timer and the call buttons.
for (const lang of ["en", "ar"] as const) {
  test(`sibling gets the sugar_low crisis bar with the 15-minute timer (${lang})`, async ({ page, context, baseURL }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await context.addCookies([{ name: "birrcare_lang", value: lang, url: baseURL! }]);
    const now = new Date().toISOString();
    await mockApi(page, {
      contacts: { emergency_number: "997", doctor_name: "Dr. Salem", doctor_phone: "+966500000000" },
      sse: [
        {
          type: "VITAL_LOGGED",
          timestamp: now,
          actor: { id: "c2", name: "Fatima", avatar_color: "#2563eb" },
          payload: { id: "v1", alert_id: "a1", vital_type: "sugar", blood_sugar: 2.9, sugar_status: "LOW", recorded_at: now },
        },
      ],
      alerts: [{ id: "a1", kind: "sugar_low", emergency: true, created_by_name: "Fatima", payload: { blood_sugar: 2.9 }, created_at: now }],
    });
    await page.goto("/");

    const bar = page.getByTestId("crisis-bar");
    await expect(bar).toBeVisible();
    await expect(bar).toHaveAttribute("data-kind", "sugar_low");
    await expect(bar).toContainText("2.9 mmol/L");
    await expect(bar).toContainText(lang === "ar" ? "هبوط السكر" : "Low blood sugar");
    await expect(bar.getByTestId("sugar-recheck")).toContainText(lang === "ar" ? "أعد قياس السكر بعد" : "Recheck sugar in");
    await expect(bar.getByTestId("sugar-recheck")).toContainText(/1[45]:\d\d/);
    for (const href of ["tel:997", "tel:+966500000000"]) {
      await expect(bar.locator(`a[href="${href}"]`)).toBeInViewport({ ratio: 1 });
    }
    await expectTapTargets(bar);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
}
