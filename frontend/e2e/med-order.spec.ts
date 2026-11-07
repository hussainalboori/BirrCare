import { test, expect, type Page } from "@playwright/test";

// The medicines list can start from each person's own shift (Shift settings).

const med = (id: string, en: string) => ({ id, name_ar: en, name_en: en, dosage: "1", frequency_slots: [], is_active: true, created_at: "2026-10-01T00:00:00Z" });

async function mockApi(page: Page, orderFrom: string, lang: "ar" | "en", role = "caregiver") {
  await page.context().addCookies([{ name: "birrcare_lang", value: lang, url: "http://127.0.0.1:3100" }]);
  let order = "clock";
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 200\n\n" });
    if (path === "/auth/me") return json({ id: "c1", name: "Hussain", role, avatar_color: "#059669", preferred_lang: lang, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path.startsWith("/medications/schedule")) {
      return json({
        date: "2026-10-21",
        order_from: orderFrom,
        schedule: [
          { slot: "night", time: "02:00", medication: med("m1", "Night pill") },
          { slot: "morning", time: "08:00", medication: med("m2", "Morning pill") },
          { slot: "evening", time: "20:00", medication: med("m3", "Evening pill") },
        ],
      });
    }
    if (path === "/shifts/med-order") {
      if (route.request().method() === "PUT") order = route.request().postDataJSON().order;
      return json({ order });
    }
    if (path === "/shifts/current-duty") return json({ current_time: "10:00", date: "2026-10-21", is_on_duty: false });
    return json([]);
  });
}

const headings = (page: Page) => page.locator("main section h2").allTextContents();

for (const lang of ["en", "ar"] as const) {
  const names = lang === "en" ? ["Morning", "Evening", "Night"] : ["الصباح", "المساء", "منتصف الليل"];

  test(`an 8 AM shift puts Night last (${lang})`, async ({ page }) => {
    await mockApi(page, "08:00", lang);
    await page.goto("/");
    await expect(page.getByTestId("order-from")).toContainText("08:00");
    await expect.poll(() => headings(page)).toEqual(names);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
}

test("clock order is unchanged when the setting is off", async ({ page }) => {
  await mockApi(page, "", "en");
  await page.goto("/");
  await expect.poll(() => headings(page)).toEqual(["Night", "Morning", "Evening"]);
  await expect(page.getByTestId("order-from")).toHaveCount(0);
});

test("the Super Admin chooses the order in the shift settings", async ({ page }) => {
  await mockApi(page, "", "en", "super_admin");
  await page.goto("/");
  await page.getByRole("button", { name: "Menu" }).first().click();
  await page.getByRole("button", { name: /shift management/i }).first().click();
  await expect(page.getByTestId("med-order-clock")).toHaveAttribute("aria-pressed", "true");
  await page.getByTestId("med-order-shift").click();
  await expect(page.getByTestId("med-order-shift")).toHaveAttribute("aria-pressed", "true");
  const box = await page.getByTestId("med-order-shift").boundingBox();
  expect(box!.height).toBeGreaterThanOrEqual(48);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test("the list reloads when the roster changes", async ({ page }) => {
  await mockApi(page, "", "en");
  let from = "";
  await page.route("**/api/v1/medications/schedule*", (route) =>
    route.fulfill({ json: { date: "2026-10-21", order_from: from, schedule: [
      { slot: "night", time: "02:00", medication: med("m1", "Night pill") },
      { slot: "evening", time: "20:00", medication: med("m3", "Evening pill") },
    ] } }),
  );
  await page.route("**/api/v1/events/stream", (route) =>
    route.fulfill({ status: 200, contentType: "text/event-stream", body: from ? `retry: 200\ndata: {"type":"DUTY_UPDATED","payload":{}}\n\n` : "retry: 200\n\n" }),
  );
  await page.goto("/");
  await expect.poll(() => headings(page)).toEqual(["Night", "Evening"]);
  from = "20:00";
  await expect.poll(() => headings(page)).toEqual(["Evening", "Night"]);
});
