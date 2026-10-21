import { test, expect } from "@playwright/test";

// Issue #25 review: cancelling a PRN dose frees a place in the 24h cap, so
// "Undo" appears only on your own entry for 10 minutes (admins can cancel any
// entry, with a reason). The server enforces the same rule.
for (const role of ["caregiver", "admin"] as const) {
  test(`${role}: Undo only where allowed`, async ({ page, context, baseURL }) => {
    await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
    const now = Date.now();
    const log = (id: string, by: string, minsAgo: number) => ({
      id, medication_id: "m_para", caregiver_id: by, caregiver_name: by === "c1" ? "Hussain" : "Fatima",
      given_at: new Date(now - minsAgo * 60_000).toISOString(), recorded_at: new Date(now - minsAgo * 60_000).toISOString(), reason_tag: "headache",
    });
    await page.route("**/api/v1/**", async (route) => {
      const path = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, "");
      const json = (b: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(b) });
      if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
      if (path === "/auth/me") return json({ id: "c1", name: "Hussain", role, avatar_color: "#059669", preferred_lang: "en", is_active: true, created_at: "2026-10-01T00:00:00Z" });
      if (path === "/medications/prn") {
        return json([{
          medication: { id: "m_para", name_ar: "باراسيتامول", name_en: "Paracetamol", dosage: "500 mg", frequency_slots: [], is_active: true, is_prn: true, created_at: "2026-10-01T00:00:00Z" },
          count_last_24h: 3, max_per_24h: 4, min_hours: 4, can_give: false, lock_reason: "too_soon",
          recent_logs: [log("mine_new", "c1", 2), log("mine_old", "c1", 60), log("hers", "c2", 3)],
        }]);
      }
      if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
      return json([]);
    });
    await page.goto("/");
    await page.getByRole("button", { name: /Previous intake history/ }).click();
    const undo = page.getByRole("button", { name: "Undo" });
    await expect(page.getByText("Fatima").first()).toBeVisible();
    // Own entry from 2 minutes ago: yes. Own entry from an hour ago and Fatima's: only for an admin.
    await expect(undo).toHaveCount(role === "admin" ? 3 : 1);
  });
}
