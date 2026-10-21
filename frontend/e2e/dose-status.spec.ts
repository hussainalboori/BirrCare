import { test, expect, type Page } from "@playwright/test";

// Issue #9: partial and vomited doses show a persistent warning routed to the doctor.
const me = { id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: "en", is_active: true, created_at: "2026-10-01T00:00:00Z" };
const med = (id: string, name: string) => ({ id, name_ar: name, name_en: name, dosage: "1 tab", frequency_slots: ["morning"], is_active: true, created_at: "2026-10-01T00:00:00Z" });

async function mockApi(page: Page, events: Record<string, unknown[]>) {
  const now = new Date().toISOString();
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: 'event: connected\ndata: {}\n\n' });
    if (path === "/auth/me") return json(me);
    if (path === "/contacts") return json({ doctor_name: "Dr. Salem", doctor_phone: "+966500000000" });
    if (path === "/medications/log/L2/event") {
      const ev = { id: "e1", type: "vomited", minutes_after: 10, caregiver_name: me.name, created_at: now };
      events.L2 = [ev];
      return json(ev, 201);
    }
    if (path.startsWith("/medications/schedule")) {
      const log = (id: string, status: string) => ({
        id, medication_id: id, caregiver_id: "c1", slot: "morning", scheduled_date: "2026-10-20",
        taken_at: now, recorded_at: now, status, caregiver_name: me.name, events: events[id] ?? [],
      });
      return json({ date: "2026-10-20", schedule: [
        { slot: "morning", medication: med("L1", "Warfarin"), log: log("L1", "partial") },
        { slot: "morning", medication: med("L2", "Aspirin"), log: log("L2", "taken") },
      ]});
    }
    return json([]);
  });
}

test("partial dose warns and offers to call the doctor; vomiting is a follow-up on a given dose", async ({ page, context, baseURL }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  await mockApi(page, {});
  await page.goto("/");

  await expect(page.getByText("Partial dose", { exact: true })).toBeVisible();
  await expect(page.getByText(/Only part of this dose was taken\. Call the doctor or pharmacist/)).toBeVisible();
  await expect(page.getByRole("link", { name: /Call Dr\. Salem/ }).first()).toHaveAttribute("href", "tel:+966500000000");

  // Aspirin was taken; the family reports vomiting 10 minutes later.
  await page.getByRole("button", { name: "Vomited after?" }).last().click();
  await page.getByRole("button", { name: "10 min", exact: true }).click();
  await expect(page.getByText(/Given, then vomited 10 min later\. Call the doctor or pharmacist/)).toBeVisible();
  // No re-dose is offered: both cards stay locked.
  await expect(page.getByRole("button", { name: /^Give as / })).toHaveCount(0);
});
