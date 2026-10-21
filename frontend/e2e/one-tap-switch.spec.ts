import { test, expect, type Page } from "@playwright/test";

// Issue #29: on a trusted home device a sibling switches with one tap and no
// keypad; anywhere else the tap falls back to the 4-digit PIN keypad.
const siblings = [
  { id: "c1", name: "Hussain", avatar_color: "#059669" },
  { id: "c2", name: "Fatima", avatar_color: "#db2777" },
];

type State = { me: string; trusted: boolean; switches: unknown[] };

async function mockApi(page: Page, state: State) {
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    const me = () => ({ ...siblings.find((c) => c.id === state.me)!, role: "caregiver", preferred_lang: "en", is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (path.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
    if (path === "/auth/me") return json(me());
    if (path === "/caregivers") return json(siblings);
    if (path === "/auth/trust-status") return json({ trusted: state.trusted, reason: state.trusted ? "paired_device" : "untrusted", device_name: state.trusted ? "Bedside iPad" : undefined });
    if (path === "/auth/switch-caregiver") {
      const body = req.postDataJSON();
      state.switches.push(body);
      if (!state.trusted) return json({ error: "PIN_REQUIRED" }, 403);
      state.me = body.caregiver_id;
      return json({ status: "success", caregiver: me() });
    }
    if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
    return json([]);
  });
}

for (const locale of ["ar", "en"] as const) {
  test(`${locale}: paired device switches in one tap with no keypad`, async ({ page, context, baseURL }) => {
    await page.setViewportSize({ width: 375, height: 740 });
    await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL! }]);
    const state: State = { me: "c1", trusted: true, switches: [] };
    await mockApi(page, state);
    await page.goto("/");
    await expect(page.getByTestId("network-status-badge")).toContainText(locale === "ar" ? "شبكة المنزل" : "Home Network");

    await page.getByTestId("sibling-c2").click(); // one tap
    await expect(page.getByTestId("sibling-c2")).toHaveAttribute("aria-current", "true");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    expect(state.switches).toEqual([{ caregiver_id: "c2" }]);
    expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
  });
}

test("outside the home the tap opens the PIN keypad", async ({ page, context, baseURL }) => {
  await context.addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  const state: State = { me: "c1", trusted: false, switches: [] };
  await mockApi(page, state);
  await page.goto("/");
  await expect(page.getByTestId("network-status-badge")).toContainText("PIN");

  await page.getByTestId("sibling-c2").click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("button", { name: /Fatima/ })).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByTestId("sibling-c1")).toHaveAttribute("aria-current", "true");
});
