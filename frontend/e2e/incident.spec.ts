import { test, expect, type Page } from "@playwright/test";

// Issue #60: fall and seizure guidance. The seizure timer turns the screen red at
// 5:00 with the call button in view; usable one-handed at 375 px in Night mode.

async function mockApi(page: Page, locale: string, posts: Record<string, unknown>[]) {
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 60000\nevent: connected\ndata: {}\n\n" });
    if (path === "/auth/me") return json({ id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path === "/contacts") return json({ emergency_number: "997", doctor_name: "Dr. Salem", doctor_phone: "+966500000000" });
    if (path === "/care/incidents" && req.method() === "POST") {
      const body = req.postDataJSON();
      posts.push(body);
      const escalate = body.kind === "fall" ? body.hit_head || !body.awake_normal || body.pain_or_bleeding : body.duration_seconds >= 300;
      return json({ id: "i1", kind: body.kind, escalate, blood_thinners: ["Apixaban"] }, 201);
    }
    if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-27", schedule: [] });
    return json([]);
  });
}

for (const locale of ["en", "ar"] as const) {
  test(`seizure timer turns red at 5:00 with the call button in view, Night mode, 375 px (${locale})`, async ({ page, context, baseURL }) => {
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    await page.setViewportSize({ width: 375, height: 667 });
    await page.emulateMedia({ colorScheme: "dark" });
    await page.clock.install({ time: new Date("2026-10-27T03:00:00Z") });
    const posts: Record<string, unknown>[] = [];
    await mockApi(page, locale, posts);
    await page.goto("/");

    await page.getByTestId("something-happened").click();
    await page.getByTestId("choose-seizure").click();
    const screen = page.getByTestId("seizure");
    await page.getByTestId("start-timer").click();
    await expect(screen).toHaveAttribute("data-red", "false");
    await expect(screen.getByTestId("call-emergency")).toHaveAttribute("href", "tel:997");

    await page.clock.runFor(4 * 60_000 + 50_000);
    await expect(page.getByTestId("seizure-timer")).toContainText("4:5");
    await expect(screen).toHaveAttribute("data-red", "false");
    await page.clock.runFor(15_000);
    await expect(screen).toHaveAttribute("data-red", "true");
    await expect(screen).toContainText(locale === "ar" ? "اتصل بالإسعاف الآن" : "Call emergency now");
    const call = screen.getByTestId("call-emergency");
    await expect(call).toBeInViewport({ ratio: 1 });
    const box = await call.boundingBox();
    expect(box!.height).toBeGreaterThanOrEqual(48);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

    await page.getByTestId("stop-timer").click();
    await expect.poll(() => posts.length).toBe(1);
    expect(posts[0]).toMatchObject({ kind: "seizure" });
    expect(Number(posts[0].duration_seconds)).toBeGreaterThanOrEqual(300);
  });
}

test("fall with a head knock: call emergency, with the line for the dispatcher", async ({ page, context, baseURL }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  await page.setViewportSize({ width: 375, height: 667 });
  const posts: Record<string, unknown>[] = [];
  await mockApi(page, "en", posts);
  await page.goto("/incident");
  await page.getByTestId("choose-fall").click();
  await page.getByRole("button", { name: "Yes", exact: true }).click(); // hit head
  await page.getByRole("button", { name: "Yes", exact: true }).click(); // awake and talking
  await page.getByRole("button", { name: "No", exact: true }).click(); // no pain / bleeding
  await expect(page.getByTestId("fall-emergency")).toBeVisible();
  await expect(page.getByTestId("dispatcher-line")).toContainText("hit her head");
  await expect(page.getByTestId("dispatcher-line")).toContainText("Apixaban");
  expect(posts[0]).toMatchObject({ kind: "fall", hit_head: true, awake_normal: true, pain_or_bleeding: false });
});
