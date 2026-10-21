import { test, expect, type Page } from "@playwright/test";

// Issue #22: oxygen and temperature. A pulse-ox reading in the dark is 3 taps
// (Record → Pulse-ox → Save) plus typing the two numbers.

async function mockApi(page: Page, posted: Record<string, unknown>[]) {
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, code = 200) => route.fulfill({ status: code, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
    if (path === "/auth/me") return json({ id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: "en", is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path === "/vitals" && req.method() === "POST") {
      const body = req.postDataJSON();
      posted.push(body);
      return json({ id: `v${posted.length}`, vital_type: body.vital_type, spo2_status: body.spo2 <= 88 ? "CRITICAL" : "NORMAL", recorded_at: new Date().toISOString() }, 201);
    }
    if (path.startsWith("/vitals")) {
      return json(
        posted.map((b, i) => ({ id: `v${i + 1}`, caregiver_id: "c1", caregiver_name: "Hussain", recorded_at: new Date().toISOString(), alertness_level: null, ...b }))
      );
    }
    if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
    return json([]);
  });
}

test("Night: pulse-ox 87% + pulse 104 → 3 taps, call buttons shown, saved and listed", async ({ browser, baseURL }) => {
  const ctx = await browser.newContext({ viewport: { width: 375, height: 800 } });
  await ctx.addCookies([
    { name: "birrcare_theme", value: "night", url: baseURL! },
    { name: "birrcare_lang", value: "en", url: baseURL! },
  ]);
  const page = await ctx.newPage();
  page.on("dialog", (d) => {
    throw new Error(`unexpected dialog: ${d.message()}`);
  });
  const posted: Record<string, unknown>[] = [];
  await mockApi(page, posted);
  await page.goto("/vitals");
  await expect(page.locator("html")).toHaveClass(/night/);

  let taps = 0;
  const tap = async (l: ReturnType<Page["getByRole"]> | ReturnType<Page["getByTestId"]>) => {
    taps++;
    await l.click();
  };
  await tap(page.getByRole("button", { name: /Record New Vitals/ }).first());
  await tap(page.getByTestId("kind-spo2"));
  // The oxygen field already has the cursor: type, move to pulse, type.
  await expect(page.getByLabel("Oxygen SpO₂ (%)")).toBeFocused();
  await page.keyboard.type("87");
  await page.getByLabel("Pulse (optional)").fill("104");
  await expect(page.getByTestId("spo2-advice")).toContainText("Very low — call emergency");
  await expect(page.getByTestId("spo2-advice").getByRole("link", { name: /Emergency|Call/ }).first()).toBeVisible();
  await tap(page.getByRole("button", { name: "Save as Hussain" }));

  await expect.poll(() => posted.length).toBe(1);
  expect(taps).toBe(3);
  expect(posted[0]).toMatchObject({ vital_type: "spo2", spo2: 87, pulse: 104 });
  expect(posted[0]).not.toHaveProperty("systolic");
  await expect(page.getByTestId("spo2-badge")).toContainText("SpO₂ 87%");
  await ctx.close();
});

test("temperature 38.4 shows Fever; out-of-range oxygen is refused before saving", async ({ page, context, baseURL }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "ar", url: baseURL! }]);
  const posted: Record<string, unknown>[] = [];
  await mockApi(page, posted);
  const dialogs: string[] = [];
  page.on("dialog", async (d) => {
    dialogs.push(d.message());
    await d.dismiss();
  });
  await page.goto("/vitals");
  await page.getByRole("button", { name: /تسجيل قراءة جديدة/ }).first().click();
  await page.getByTestId("kind-temp").click();
  await expect(page.getByLabel("الحرارة (°م)")).toBeFocused();
  await page.keyboard.type("38.4");
  await expect(page.getByTestId("temp-advice")).toContainText("حمّى");
  await page.getByRole("button", { name: "احفظ باسم Hussain" }).click();
  await expect.poll(() => posted.length).toBe(1);
  expect(posted[0]).toMatchObject({ vital_type: "temp", temperature_c: 38.4 });
  await expect(page.getByTestId("temp-badge")).toContainText("38.4 °C");

  await page.getByRole("button", { name: /تسجيل قراءة جديدة/ }).first().click();
  await page.getByTestId("kind-spo2").click();
  await page.keyboard.type("101");
  await page.getByRole("button", { name: "احفظ باسم Hussain" }).click();
  // The field itself refuses 101 (max 100), so nothing is sent.
  expect(await page.getByLabel("الأكسجين SpO₂ (%)").evaluate((el: HTMLInputElement) => el.validity.valid)).toBe(false);
  await page.waitForTimeout(300);
  expect(posted).toHaveLength(1);
});
