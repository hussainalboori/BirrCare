import { test, expect, type Page } from "@playwright/test";

// Issue #51: the Super Admin sees whether last night's off-site backup worked,
// and can start one; nobody else sees the Server screen.
const hoursAgo = (h: number) => new Date(Date.now() - h * 3600_000).toISOString();

const states = {
  green: {
    configured: true, stale: false, reason: "ok", pending: false,
    schedule: { times: ["03:00", "09:00", "15:00", "21:00"], avoid_busy: true }, next_at: new Date(Date.now() + 3 * 3600_000).toISOString(),
    status: {
      status: "success", last_success_at: hoursAgo(2), last_attempt_at: hoursAgo(2),
      destinations: [{ name: "Google Drive", snapshot_id: "a1b2c3d4" }, { name: "Cloudflare R2", snapshot_id: "a1b2c3d4" }],
      last_restore_test_at: hoursAgo(30), last_restore_test_ok: true,
    },
  },
  red: {
    configured: true, stale: true, reason: "invalid_grant", pending: false,
    schedule: { times: ["03:00", "15:00"], avoid_busy: true },
    status: { status: "failed", last_success_at: hoursAgo(27), last_attempt_at: hoursAgo(1), error_code: "invalid_grant", error: "Google sign-in for backups expired. Run: rclone config reconnect gdrive:", destinations: [{ name: "Google Drive" }] },
  },
} as const;

async function open(page: Page, baseURL: string, locale: "ar" | "en", role: string, state: unknown, runs: string[] = [], puts: unknown[] = []) {
  await page.setViewportSize({ width: 375, height: 812 });
  await page.context().addCookies([{ name: "birrcare_lang", value: locale, url: baseURL }]);
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const p = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (p.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
    if (p === "/auth/me") return json({ id: "u1", name: "Hussain", role, totp_enabled: true, avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (p.startsWith("/admin/")) {
      if (role !== "super_admin") return json({ error: "FORBIDDEN" }, 403);
      if (p === "/admin/backup/status") return json(state);
      if (p === "/admin/backup/schedule") {
        const body = req.postDataJSON();
        puts.push(body);
        return json({ ...(state as object), schedule: body });
      }
      if (p === "/admin/backup/run") {
        runs.push(req.method());
        return json({ ...(state as object), pending: true }, 202);
      }
      if (p === "/admin/update/status") return json({ version: "606fb10", available: null, update_available: false, result: null, policy: { mode: "manual" }, pending: false, checker_stale: false });
    }
    if (p.startsWith("/medications/schedule")) return json({ date: "2026-10-22", schedule: [] });
    return json([]);
  });
  await page.goto("/");
}

const menu = (locale: "ar" | "en") => (locale === "ar" ? "القائمة" : "Menu");
const server = (locale: "ar" | "en") => (locale === "ar" ? /^الخادم/ : /^Server/);

for (const locale of ["ar", "en"] as const) {
  test(`${locale}: green state, then Back up now`, async ({ page, baseURL }) => {
    const runs: string[] = [];
    await open(page, baseURL!, locale, "super_admin", states.green, runs);
    await page.getByRole("button", { name: menu(locale) }).click();
    await page.getByRole("button", { name: server(locale) }).click();
    const card = page.getByTestId("backups-card");
    await expect(card).toHaveAttribute("data-reason", "ok");
    await expect(card).toContainText(locale === "ar" ? "النسخ الاحتياطي يعمل" : "Backups are working");
    await expect(page.getByTestId("backup-last")).toContainText("Google Drive");
    await expect(page.getByTestId("backup-last")).toContainText("Cloudflare R2");
    await expect(page.getByTestId("backup-restore")).toContainText(locale === "ar" ? "اختبار الاسترجاع نجح" : "Restore test passed");
    expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
    const btn = page.getByTestId("backup-now");
    expect((await btn.boundingBox())!.height).toBeGreaterThanOrEqual(48);
    await btn.click();
    await expect.poll(() => runs).toEqual(["POST"]);
    await expect(page.getByTestId("backup-running")).toBeVisible();
  });

  test(`${locale}: red state when Google sign-in expired, with a dot on the menu`, async ({ page, baseURL }) => {
    await open(page, baseURL!, locale, "super_admin", states.red);
    await expect(page.getByTestId("menu-update-dot")).toBeVisible();
    await page.getByRole("button", { name: menu(locale) }).click();
    await page.getByRole("button", { name: server(locale) }).click();
    const card = page.getByTestId("backups-card");
    await expect(card).toHaveAttribute("data-reason", "invalid_grant");
    await expect(card).toContainText(locale === "ar" ? "انتهى تسجيل دخول Google" : "Google sign-in for backups expired");
    await expect(card).toContainText("rclone config reconnect gdrive:");
    expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
  });
}

for (const role of ["admin", "caregiver"]) {
  test(`${role}: no Server screen and no Back up now`, async ({ page, baseURL }) => {
    await open(page, baseURL!, "en", role, states.red);
    await page.getByRole("button", { name: "Menu" }).click();
    await expect(page.getByRole("button", { name: /^Server/ })).toHaveCount(0);
    await expect(page.getByTestId("backup-now")).toHaveCount(0);
    await expect(page.getByTestId("menu-update-dot")).toHaveCount(0);
  });
}

test("Super Admin picks the backup times and the busy-time rule", async ({ page, baseURL }) => {
  const puts: unknown[] = [];
  await open(page, baseURL!, "en", "super_admin", states.green, [], puts);
  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: /^Server/ }).click();
  const times = page.getByTestId("backup-times");
  await expect(times).toContainText("Backup times (household time)");
  await expect(page.getByTestId("backup-next")).toContainText("Next backup");
  for (const t of ["03:00", "09:00", "15:00", "21:00"]) await expect(times).toContainText(t);

  // Drop the busy afternoon slot, add a quiet one, turn off waiting, save.
  await times.locator("li", { hasText: "15:00" }).getByRole("button", { name: "Remove" }).click();
  await page.getByTestId("backup-new-time").fill("14:30");
  await page.getByRole("button", { name: "Add time" }).click();
  await page.getByTestId("backup-avoid-busy").click();
  await page.getByTestId("backup-times-save").click();
  await expect.poll(() => puts).toEqual([{ times: ["03:00", "09:00", "14:30", "21:00"], avoid_busy: false }]);
  await expect(times).toContainText("Times saved");
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
});

for (const locale of ["ar", "en"] as const) {
  test(`${locale}: a postponed backup says why and when it will run anyway`, async ({ page, baseURL }) => {
    const slot = new Date(Date.now() - 10 * 60_000).toISOString();
    await open(page, baseURL!, locale, "super_admin", { ...states.green, postponed: { slot, reason: "in_use", at: new Date().toISOString() } });
    await page.getByRole("button", { name: locale === "ar" ? "القائمة" : "Menu" }).click();
    await page.getByRole("button", { name: locale === "ar" ? /^الخادم/ : /^Server/ }).click();
    await expect(page.getByTestId("backup-postponed")).toContainText(locale === "ar" ? "لأن التطبيق قيد الاستخدام" : "the app is in use");
    expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
  });
}
