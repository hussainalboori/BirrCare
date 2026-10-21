import { test, expect, type Page } from "@playwright/test";

// The medicines page shows only medicines (plus the one-tap stroke check); the
// day's other care lives on the Report page, one tap away in the bottom bar.
async function open(page: Page, baseURL: string, path: string, locale = "ar") {
  await page.context().addCookies([{ name: "birrcare_lang", value: locale, url: baseURL }]);
  await page.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(b) });
    if (p.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
    if (p === "/auth/me") return json({ id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (p.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
    return json([]);
  });
  await page.goto(path);
}

test("medicines page is medicines only; Report holds the rest", async ({ page, baseURL }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  await open(page, baseURL!, "/");
  await expect(page.getByTestId("seems-off")).toBeVisible();
  await expect(page.getByTestId("reposition-card")).toHaveCount(0);
  await expect(page.getByTestId("fluid-bowel-card")).toHaveCount(0);

  await page.locator("nav").getByRole("link", { name: "التقرير" }).click();
  await expect(page).toHaveURL(/\/report$/);
  await expect(page.getByTestId("reposition-card")).toBeVisible();
  await expect(page.getByTestId("fluid-bowel-card")).toBeVisible();
  await expect(page.locator("nav").getByRole("link", { name: "التقرير" })).toHaveAttribute("aria-current", "page");
});

for (const locale of ["ar", "en"] as const) {
  test(`${locale}: at 375px every bottom-bar label is whole and apart from its neighbours`, async ({ page, baseURL }) => {
    await page.setViewportSize({ width: 375, height: 800 });
    await open(page, baseURL!, "/report", locale);
    await expect(page.locator("nav a")).toHaveCount(7);
    const boxes = await page.evaluate(() =>
      [...document.querySelectorAll("nav a")].map((a) => {
        const r = document.createRange();
        r.selectNodeContents(a.querySelectorAll("span")[1]);
        const b = r.getBoundingClientRect();
        return [b.left, b.right];
      })
    );
    boxes.sort((a, b) => a[0] - b[0]);
    for (let i = 1; i < boxes.length; i++) expect(boxes[i][0]).toBeGreaterThan(boxes[i - 1][1]);
    expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
  });
}

test("the PIN keypad no longer advertises a default PIN", async ({ page, baseURL }) => {
  await open(page, baseURL!, "/", "en");
  await page.getByRole("button", { name: "Switch Profile" }).click();
  await expect(page.getByRole("dialog")).toContainText("Forgot your PIN? Ask the admin to reset it.");
  await expect(page.getByRole("dialog")).not.toContainText("Default PIN");
});
