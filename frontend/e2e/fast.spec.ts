import { test, expect, type Page } from "@playwright/test";

// Issue #13: the F.A.S.T. stroke check and the low blood pressure band.
const me = { id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: "en", is_active: true, created_at: "2026-10-01T00:00:00Z" };

async function mockApi(page: Page, vitals: unknown[] = []) {
  const calls: { method: string; path: string; body: unknown }[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (req.method() !== "GET") calls.push({ method: req.method(), path, body: req.postDataJSON() });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 60000\nevent: connected\ndata: {}\n\n" });
    if (path === "/auth/me") return json(me);
    if (path === "/contacts") return json({ emergency_number: "997", doctor_name: "Dr. Salem", doctor_phone: "+966500000000" });
    if (path === "/settings/clinical") return json({ bp_low_systolic: 90, bp_low_diastolic: 60 });
    if (path === "/alerts/unacked") return json([]);
    if (path === "/vitals" && req.method() === "POST") return json({ id: "f1" }, 201);
    if (path.startsWith("/vitals")) return json(vitals);
    if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-20", schedule: [] });
    return json([]);
  });
  return calls;
}

test.beforeEach(async ({ context, baseURL, page }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  await page.setViewportSize({ width: 375, height: 667 });
});

test("Arm: yes → emergency screen with a tel: link in two taps from Home", async ({ page }) => {
  const calls = await mockApi(page);
  await page.goto("/");
  await page.getByTestId("seems-off").click(); // tap 1
  await page.getByTestId("fast-arm").getByRole("button", { name: "Yes" }).click(); // tap 2

  const screen = page.getByTestId("fast-emergency");
  await expect(screen).toBeVisible();
  const call = screen.locator('a[href="tel:997"]');
  await expect(call).toBeInViewport({ ratio: 1 });
  const box = await call.boundingBox();
  expect(box!.height).toBeGreaterThanOrEqual(64);

  await expect.poll(() => calls.find((c) => c.path === "/vitals")).toBeTruthy();
  expect(calls.find((c) => c.path === "/vitals")!.body).toMatchObject({ vital_type: "fast", fast_result: { face: false, arm: true, speech: false } });

  // Last seen normal, for the paramedics.
  await screen.getByRole("button", { name: "30 min ago" }).click();
  await expect(page.getByTestId("last-normal")).toBeVisible();
  await expect.poll(() => calls.find((c) => c.path === "/vitals/f1/last-normal")).toBeTruthy();
});

test("all no → re-check in 15 minutes with the doctor's number", async ({ page }) => {
  const calls = await mockApi(page);
  await page.goto("/fast");
  for (const k of ["face", "arm", "speech"]) await page.getByTestId(`fast-${k}`).getByRole("button", { name: "No" }).click();
  const neg = page.getByTestId("fast-negative");
  await expect(neg).toBeVisible();
  await expect(neg).toContainText(/1[45]:\d\d/);
  await expect(neg.locator('a[href="tel:+966500000000"]')).toBeVisible();
  await expect.poll(() => calls.find((c) => c.path === "/vitals")?.body).toMatchObject({ fast_result: { face: false, arm: false, speech: false } });
});

test("82/48 is shown as low blood pressure, not green", async ({ page }) => {
  await mockApi(page, [
    { id: "v1", caregiver_id: "c2", caregiver_name: "Fatima", recorded_at: "2026-10-20T08:00:00Z", vital_type: "bp", systolic: 82, diastolic: 48, alertness_level: null, bp_status: "LOW" },
  ]);
  await page.goto("/vitals");
  await expect(page.getByText("Low blood pressure").first()).toBeVisible();
});
