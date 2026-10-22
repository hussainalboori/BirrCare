import { test, expect, type Page, type BrowserContext } from "@playwright/test";

// Issue #43: the Super Admin sees that an update exists, reads what changed and
// installs it; everyone with the app open is told it is restarting.
type Shared = { queues: Map<Page, string[]>; installs: unknown[] };

const status = {
  version: "8ecc00c",
  available: {
    current_sha: "8ecc00c0000000000000000000000000000000aa",
    target_sha: "4f2a91c0000000000000000000000000000000bb",
    ci: "success",
    checked_at: new Date().toISOString(),
    commits: [
      { sha: "4f2a91c", subject: "fix(vitals): larger numbers on the BP card", date: "2026-10-21T10:00:00+03:00" },
      { sha: "3e1b0d2", subject: "feat(care): fluid intake tracking", date: "2026-10-21T09:00:00+03:00" },
    ],
    changes_database: true,
  },
  update_available: true,
  result: null,
  policy: { mode: "manual" },
  pending: false,
  checker_stale: false,
};

async function phone(context: BrowserContext, baseURL: string, shared: Shared, me: { id: string; name: string; role: string }, locale = "en"): Promise<Page> {
  await context.addCookies([{ name: "birrcare_lang", value: locale, url: baseURL }]);
  const page = await context.newPage();
  await page.setViewportSize({ width: 375, height: 800 });
  shared.queues.set(page, []);
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, code = 200) => route.fulfill({ status: code, contentType: "application/json", body: JSON.stringify(b) });
    if (path.startsWith("/events/stream")) {
      const q = shared.queues.get(page)!.splice(0);
      return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 200\nevent: connected\ndata: {}\n\n" + q.map((e) => `data: ${e}\n\n`).join("") });
    }
    if (path === "/auth/me") return json({ ...me, avatar_color: "#059669", preferred_lang: locale, is_active: true, totp_enabled: true, created_at: "2026-10-01T00:00:00Z" });
    if (path.startsWith("/admin/update/")) {
      if (me.role !== "super_admin") return json({ error: "FORBIDDEN" }, 403);
      if (path === "/admin/update/status") return json((globalThis as { __status?: unknown }).__status ?? status);
      if (path === "/admin/update/install") {
        shared.installs.push(req.postDataJSON());
        const ev = JSON.stringify({ type: "UPDATE_STARTING", timestamp: new Date().toISOString(), actor: { id: me.id, name: me.name, avatar_color: "#059669" }, payload: { target_sha: "4f2a91c" } });
        for (const q of shared.queues.values()) q.push(ev);
        return json({ status: "accepted" }, 202);
      }
    }
    if (path.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
    return json([]);
  });
  await page.goto("/");
  return page;
}

test("Super Admin sees the dot and the change list; installing tells every phone", async ({ browser, baseURL }) => {
  const shared: Shared = { queues: new Map(), installs: [] };
  const admin = await phone(await browser.newContext(), baseURL!, shared, { id: "sa1", name: "Hussain", role: "super_admin" });
  const sister = await phone(await browser.newContext(), baseURL!, shared, { id: "c2", name: "Fatima", role: "caregiver" });

  await expect(admin.getByTestId("menu-update-dot")).toBeVisible();
  await expect(sister.locator("header")).toContainText("Fatima");
  await expect(sister.getByTestId("menu-update-dot")).toHaveCount(0);

  await admin.getByRole("button", { name: "Menu" }).click();
  await admin.getByRole("button", { name: /^Server/ }).click();
  const card = admin.getByTestId("updates-card");
  await expect(card).toContainText("Running 8ecc00c");
  await expect(card).toContainText("Update available · 2 changes");
  await expect(admin.getByTestId("update-commits")).toContainText("fix(vitals): larger numbers on the BP card");
  await expect(admin.getByTestId("update-db-notice")).toBeVisible();
  await expect(admin.getByTestId("update-auto")).toHaveAttribute("aria-checked", "false");
  expect(await admin.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);

  // A caregiver has no Updates entry at all.
  await sister.getByRole("button", { name: "Menu" }).click();
  await expect(sister.getByRole("button", { name: /^Server/ })).toHaveCount(0);
  await sister.keyboard.press("Escape");

  await admin.getByTestId("update-install").click();
  await expect.poll(() => shared.installs.length).toBe(1);
  expect(shared.installs[0]).toMatchObject({ target_sha: status.available.target_sha, confirm_during_alert: false });
  await expect(admin.getByTestId("update-installing")).toBeVisible();

  for (const p of [admin, sister]) {
    await expect(p.getByTestId("update-starting-banner")).toContainText("Birrcare is updating", { timeout: 5000 });
  }
});

test("ar: the Updates card fits a phone", async ({ browser, baseURL }) => {
  const shared: Shared = { queues: new Map(), installs: [] };
  const admin = await phone(await browser.newContext(), baseURL!, shared, { id: "sa1", name: "Hussain", role: "super_admin" }, "ar");
  await admin.getByRole("button", { name: "القائمة" }).click();
  await admin.getByRole("button", { name: /^الخادم/ }).click();
  await expect(admin.getByTestId("updates-card")).toContainText("يوجد تحديث");
  expect(await admin.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
});

for (const locale of ["ar", "en"] as const) {
  test(`${locale}: an update that needs a new setting says so and can't be installed`, async ({ browser, baseURL }) => {
    (globalThis as { __status?: unknown }).__status = {
      ...status,
      available: {
        ...status.available,
        needs_config: [{ key: "WARD_NAME", question_en: "Name of the care ward", question_ar: "اسم جناح الرعاية" }],
      },
    };
    try {
      const shared: Shared = { queues: new Map(), installs: [] };
      const admin = await phone(await browser.newContext(), baseURL!, shared, { id: "sa1", name: "Hussain", role: "super_admin" }, locale);
      await admin.getByRole("button", { name: locale === "ar" ? "القائمة" : "Menu" }).click();
      await admin.getByRole("button", { name: locale === "ar" ? /^الخادم/ : /^Server/ }).click();
      const notice = admin.getByTestId("update-needs-config");
      await expect(notice).toContainText(locale === "ar" ? "هذا التحديث يحتاج إعداداً جديداً" : "This update needs a new setting");
      await expect(notice).toContainText(locale === "ar" ? "اسم جناح الرعاية" : "Name of the care ward");
      await expect(notice).toContainText("./scripts/deploy.sh");
      await expect(admin.getByTestId("update-install")).toBeDisabled();
      expect(await admin.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
      expect(shared.installs).toEqual([]);
    } finally {
      delete (globalThis as { __status?: unknown }).__status;
    }
  });
}
