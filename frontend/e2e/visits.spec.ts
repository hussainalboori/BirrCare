import { test, expect, type Page } from "@playwright/test";

// Issue #63: appointments with who takes her, "Add to my calendar", and questions
// for the doctor closed after the visit with what the doctor said.

function tomorrow(): string {
  const d = new Date(Date.now() + 86_400_000);
  return d.toISOString().slice(0, 10);
}

async function mockApi(page: Page, locale: string, role: string, posts: { path: string; body: Record<string, unknown> }[]) {
  const date = tomorrow();
  const upcoming = [
    { id: "a1", kind: "doctor", title: "Neurology follow-up", starts_at: `${date}T07:00:00Z`, date, time: "10:00", location: "King Fahd Hospital, clinic 4", assigned_caregiver_id: "c1", assigned_name: "Hussain", status: "scheduled", created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z" },
  ];
  const open = [{ id: "q1", text: "Can she walk with the frame alone?", by: "Fatima", created_by: "c2", created_at: "2026-10-20T08:00:00Z" }];
  const answered: Record<string, unknown>[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 60000\nevent: connected\ndata: {}\n\n" });
    if (path === "/auth/me") return json({ id: "c1", name: "Hussain", role, avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path === "/caregivers/options" || path === "/caregivers")
      return json([
        { id: "c1", name: "Hussain", avatar_color: "#059669" },
        { id: "c2", name: "Fatima", avatar_color: "#2563eb" },
      ]);
    if (path === "/appointments" && req.method() === "GET") return json({ upcoming, recent: [] });
    if (path === "/doctor-questions" && req.method() === "GET") return json({ open, answered });
    if (req.method() !== "GET") {
      const body = (req.postDataJSON() ?? {}) as Record<string, unknown>;
      posts.push({ path, body });
      if (path === "/doctor-questions") {
        const q = { id: `q${open.length + 2}`, text: body.text, by: "Hussain", created_by: "c1", created_at: "2026-10-21T08:00:00Z" };
        open.push(q as (typeof open)[number]);
        return json(q, 201);
      }
      if (path.endsWith("/answer")) {
        const q = open.shift()!;
        answered.push({ ...q, answer: body.answer, answered_at: "2026-10-22T10:00:00Z", answered_by_name: "Hussain" });
        return json(answered[answered.length - 1]);
      }
      if (path.endsWith("/status")) {
        upcoming.length = 0;
        return json({ id: "a1", status: body.status });
      }
      if (path === "/appointments") return json({ id: "a2", ...body, status: "scheduled" }, 201);
    }
    return json([]);
  });
}

const noScroll = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);

for (const locale of ["en", "ar"] as const) {
  for (const scheme of ["light", "dark"] as const) {
    test(`home card and visits page fit 375 px (${locale}, ${scheme})`, async ({ page, context, baseURL }) => {
      await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
      await page.emulateMedia({ colorScheme: scheme });
      await page.setViewportSize({ width: 375, height: 740 });
      await mockApi(page, locale, "caregiver", []);
      await page.goto("/");
      const card = page.getByTestId("next-appointment");
      await expect(card).toContainText("Neurology follow-up");
      await expect(card).toContainText("10:00");
      await expect(card).toContainText(locale === "ar" ? "يأخذها" : "Hussain takes her");
      await expect(card.getByTestId("open-questions-count")).toContainText(locale === "ar" ? "أسئلة للطبيب: 1" : "1 question for the doctor");
      expect(await noScroll(page)).toBe(true);

      await card.click();
      await expect(page).toHaveURL(/\/visits$/);
      await expect(page.getByTestId("appointment")).toContainText("King Fahd Hospital");
      await expect(page.getByTestId("add-to-calendar")).toHaveAttribute("href", "/api/v1/appointments/a1/ics");
      expect(await noScroll(page)).toBe(true);
      await page.setViewportSize({ width: 1280, height: 800 });
      expect(await noScroll(page)).toBe(true);
    });
  }

  test(`add a question, then after the visit: doctor said, and did medicines change (${locale})`, async ({ page, context, baseURL }) => {
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    await page.setViewportSize({ width: 375, height: 740 });
    const posts: { path: string; body: Record<string, unknown> }[] = [];
    await mockApi(page, locale, "admin", posts);
    await page.goto("/visits");

    await page.getByTestId("question-input").fill("Should the BP tablet move to bedtime?");
    await page.getByTestId("add-question").click();
    await expect(page.getByTestId("open-question")).toHaveCount(2);
    expect(posts[0]).toMatchObject({ path: "/doctor-questions", body: { text: "Should the BP tablet move to bedtime?" } });

    await page.getByTestId("appointment-done").click();
    const after = page.getByTestId("after-visit");
    await expect(after).toContainText(locale === "ar" ? "هل غيّر الطبيب أي دواء؟" : "Did the doctor change any medicines?");
    await page.getByTestId("answer-question").first().click();
    await page.getByTestId("answer-input").fill("Yes, indoors with someone near.");
    await page.getByTestId("save-answer").click();
    await expect(page.getByTestId("open-question")).toHaveCount(1);
    expect(posts.find((p) => p.path.endsWith("/answer"))?.body).toMatchObject({ answer: "Yes, indoors with someone near.", appointment_id: "a1" });

    await page.getByTestId("meds-changed-yes").click();
    await page.getByTestId("open-medicines").click();
    await expect(page.getByRole("dialog").first()).toBeVisible(); // the medicine list (schedule changes start today or tomorrow)
    expect(await noScroll(page)).toBe(true);
  });

  test(`add an appointment with who takes her (${locale})`, async ({ page, context, baseURL }) => {
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    await page.setViewportSize({ width: 375, height: 740 });
    const posts: { path: string; body: Record<string, unknown> }[] = [];
    await mockApi(page, locale, "caregiver", posts);
    await page.goto("/visits");
    await page.getByTestId("add-appointment").click();
    await page.getByRole("radio", { name: locale === "ar" ? "تحليل" : "Blood test" }).click();
    await page.getByTestId("appointment-title").fill("INR test");
    await page.getByTestId("appointment-date").fill(tomorrow());
    await page.getByTestId("appointment-time").fill("08:30");
    await page.getByTestId("save-appointment").click();
    await expect(page.getByTestId("appointment-form")).toHaveCount(0);
    expect(posts[0]).toMatchObject({ path: "/appointments", body: { kind: "lab", title: "INR test", date: tomorrow(), time: "08:30" } });
    expect(await noScroll(page)).toBe(true);
  });
}

test("a viewer sees appointments and questions but no buttons to change them", async ({ page, context, baseURL }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  await mockApi(page, "en", "viewer", []);
  await page.goto("/visits");
  await expect(page.getByTestId("appointment")).toBeVisible();
  await expect(page.getByTestId("add-to-calendar")).toBeVisible();
  await expect(page.getByTestId("add-appointment")).toHaveCount(0);
  await expect(page.getByTestId("question-input")).toHaveCount(0);
  await expect(page.getByTestId("appointment-done")).toHaveCount(0);
});
