import { test, expect, type Page } from "@playwright/test";

// Issue #64: "Can I crush this one?" — crush / split / food guidance on the card.

const med = (id: string, name: string, extra: Record<string, unknown>) => ({
  id,
  name_ar: name,
  name_en: name,
  dosage: "30 mg",
  frequency_slots: ["morning"],
  slot_times: { morning: "08:00" },
  is_active: true,
  ...extra,
});

async function mockApi(page: Page, locale: string, opts: { swallowing?: boolean; role?: string; posts?: Record<string, unknown>[] } = {}) {
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 60000\nevent: connected\ndata: {}\n\n" });
    if (path === "/auth/me") return json({ id: "c1", name: "Hussain", role: opts.role ?? "caregiver", avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path.startsWith("/medications/schedule"))
      return json({
        date: url.searchParams.get("date"),
        schedule: [
          { medication: med("m_nif", "Nifedipine XL", { crush: "do_not_crush", split_ok: false, with_food: "with_food" }), slot: "morning", time: "08:00" },
          { medication: med("m_aml", "Amlodipine", { crush: "ok", split_ok: true, with_food: "any" }), slot: "morning", time: "08:00" },
          { medication: med("m_new", "Citicoline", { crush: "ask_pharmacist", with_food: "any" }), slot: "morning", time: "08:00" },
        ],
        swallowing_difficulty: opts.swallowing ? { at: "2026-10-26T08:00:00Z", by: "Fatima" } : null,
      });
    if (path === "/medications" && req.method() === "GET") return json([]);
    if (path === "/medications" && req.method() === "POST") {
      opts.posts?.push(req.postDataJSON());
      return json({ id: "m_x" }, 201);
    }
    return json([]);
  });
}

async function fontPx(page: Page, testId: string): Promise<number> {
  return page.getByTestId(testId).first().evaluate((el) => parseFloat(getComputedStyle(el).fontSize));
}

for (const locale of ["en", "ar"] as const) {
  for (const theme of ["light", "night"] as const) {
    test(`do-not-crush is a red badge of at least 16 px (${locale}, ${theme})`, async ({ page, context, baseURL }) => {
      const cookies: { name: string; value: string; url: string }[] = [{ name: "birrcare_lang", value: locale, url: baseURL! }];
      if (theme === "night") cookies.push({ name: "birrcare_theme", value: "night", url: baseURL! });
      await context.addCookies(cookies);
      await page.setViewportSize({ width: 375, height: 740 });
      await mockApi(page, locale);
      await page.goto("/");
      const badge = page.getByTestId("crush-do_not_crush").first();
      await expect(badge).toBeVisible();
      await expect(badge).toContainText(locale === "ar" ? "لا تُطحن" : "Do not crush");
      expect(await fontPx(page, "crush-do_not_crush")).toBeGreaterThanOrEqual(16);
      // Tailwind v4 colours are lab(); read the painted pixel through a canvas.
      const [r, g, b, bg] = await badge.evaluate((el) => {
        const css = getComputedStyle(el).backgroundColor;
        const c = document.createElement("canvas").getContext("2d")!;
        c.fillStyle = css;
        c.fillRect(0, 0, 1, 1);
        const d = c.getImageData(0, 0, 1, 1).data;
        return [d[0], d[1], d[2], css] as const;
      });
      expect(r, `red background, got ${bg}`).toBeGreaterThan(g + 60);
      expect(r).toBeGreaterThan(b + 40);
      await expect(page.getByTestId("crush-ok")).toContainText(locale === "ar" ? "يمكن طحنها" : "Can be crushed");
      await expect(page.getByText(locale === "ar" ? "لا تُقسم" : "Do not split").first()).toBeVisible();
      await expect(page.getByText(locale === "ar" ? "مع الطعام" : "With food").first()).toBeVisible();
      // Unknown and no swallowing trouble: no "ask the pharmacist" clutter.
      await expect(page.getByTestId("crush-ask_pharmacist")).toHaveCount(0);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    });
  }

  test(`swallowing difficulty this week: do-not-crush first, and ask about a liquid form (${locale})`, async ({ page, context, baseURL }) => {
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    await page.setViewportSize({ width: 375, height: 740 });
    await mockApi(page, locale, { swallowing: true });
    await page.goto("/");
    await expect(page.getByTestId("crush-first")).toHaveCount(1);
    await expect(page.getByTestId("crush-ask_pharmacist")).toHaveCount(1);
    await page.getByText(locale === "ar" ? "كبّر" : "Enlarge").first().click();
    await expect(page.getByTestId("liquid-form-hint")).toContainText(locale === "ar" ? "شراباً" : "liquid");
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
}

test("the admin records crush, split and food from the box", async ({ page, context, baseURL }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  await page.setViewportSize({ width: 375, height: 844 });
  const posts: Record<string, unknown>[] = [];
  await mockApi(page, "en", { role: "admin", posts });
  await page.goto("/");
  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "Prescriptions Catalog" }).first().click();
  await page.getByRole("button", { name: "Add Medication" }).click();
  await expect(page.getByTestId("med-swallow")).toBeVisible();
  await page.getByTestId("med-crush-do_not_crush").click();
  await page.getByTestId("med-split").selectOption("no");
  await page.getByTestId("med-with-food").selectOption("with_food");
  // Issue #65: the pharmacy, and a new prescription first.
  await page.getByTestId("med-pharmacy-name").fill("Al-Nahdi");
  await page.getByTestId("med-pharmacy-phone").fill("+966110000000");
  await page.getByTestId("med-needs-prescription").check();
  await page.getByPlaceholder("مثال: كلوبيدوغريل (بلافيكس)").fill("نيفيديبين");
  await page.getByPlaceholder("مثال: 75mg قرص واحد").fill("30 mg");
  await page.getByRole("button", { name: "Save Prescription" }).click();
  await expect.poll(() => posts.length).toBe(1);
  expect(posts[0]).toMatchObject({ crush: "do_not_crush", split_ok: false, with_food: "with_food", pharmacy_name: "Al-Nahdi", pharmacy_phone: "+966110000000", needs_prescription: true });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
