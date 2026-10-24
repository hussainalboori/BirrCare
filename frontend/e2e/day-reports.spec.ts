import { test, expect, type Page } from "@playwright/test";

// The family reports problems as they happen, several times a day: every
// report of the day is listed (not only the latest) and the form stays ready.
type Note = Record<string, unknown>;

async function open(page: Page, baseURL: string, locale: "ar" | "en", notes: Note[], queries: string[]) {
  await page.context().addCookies([{ name: "birrcare_lang", value: locale, url: baseURL }]);
  page.on("dialog", (d) => {
    throw new Error(`unexpected dialog: ${d.message()}`);
  });
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const p = url.pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (p.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 200\nevent: connected\ndata: {}\n\n" });
    if (p === "/auth/me") return json({ id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (p === "/handovers") {
      if (req.method() === "POST") {
        const body = req.postDataJSON();
        notes.unshift({ id: `n${notes.length + 1}`, caregiver_id: "c1", caregiver_name: "Hussain", avatar_color: "#059669", created_at: new Date().toISOString(), ...body });
        return json({ id: "x" }, 201);
      }
      queries.push(url.search);
      return json(notes);
    }
    return json([]);
  });
  await page.goto("/report");
}

test("three reports in one day: all listed, newest first, form ready again", async ({ page, baseURL }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const notes: Note[] = [];
  const queries: string[] = [];
  await open(page, baseURL!, "en", notes, queries);
  const form = page.getByTestId("report-form");
  await expect(page.getByTestId("day-reports")).toContainText("No reports yet today");

  await form.getByRole("button", { name: "Low appetite" }).click();
  await form.getByRole("button", { name: /Send report as Hussain/ }).click();
  await expect(form.getByRole("status")).toContainText("Report sent");

  await form.getByLabel("What happened").fill("Vomited after breakfast");
  await form.getByRole("button", { name: /Send report/ }).click();

  await form.getByRole("button", { name: "Pain" }).click();
  await expect(form).toContainText("This report will be marked urgent");
  await form.getByLabel("What happened").fill("Headache");
  await form.getByRole("button", { name: /Send report/ }).click();

  const items = page.getByTestId("report-item");
  await expect(items).toHaveCount(3);
  await expect(items.nth(0)).toContainText("Headache");
  await expect(items.nth(0)).toContainText("Urgent");
  await expect(items.nth(2)).toContainText("Low appetite");
  await expect(page.getByTestId("day-reports")).toContainText("Today's reports (3)");
  expect(notes.every((n) => n.shift_type === "report")).toBe(true);
  expect(queries[0]).toMatch(/^\?date=\d{4}-\d{2}-\d{2}$/);
  // The form is empty and ready for the next one.
  await expect(form.getByLabel("What happened")).toHaveValue("");
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
});

test("ar: an earlier day can be opened and the way back to today is labelled", async ({ page, baseURL }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const queries: string[] = [];
  await open(page, baseURL!, "ar", [], queries);
  await expect(page.getByTestId("report-form")).toContainText("بلّغ عمّا يحدث الآن");
  await page.getByLabel("اختر اليوم").fill("2026-10-20");
  await expect(page.getByTestId("day-reports")).toContainText("لا بلاغات في هذا اليوم");
  await expect.poll(() => queries.at(-1)).toBe("?date=2026-10-20");
  await page.getByRole("button", { name: "عودة لليوم" }).click();
  await expect(page.getByTestId("day-reports")).toContainText("لا بلاغات اليوم بعد");
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
});
