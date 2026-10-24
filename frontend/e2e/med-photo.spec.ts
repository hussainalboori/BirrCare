import { test, expect } from "@playwright/test";

// An admin photographs the medicine box: a big phone photo is shrunk on the
// phone to a JPEG under 1 MB, uploaded, and shown as the medicine's picture.
test("admin adds a photo of the medicine box from the phone", async ({ page, baseURL }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.context().addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  const uploads: { type: string; size: number }[] = [];
  const url = "/api/v1/medications/photos/0f0e0d0c-0b0a-4908-8706-050403020100.jpg";
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const p = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (p.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
    if (p === "/auth/me") return json({ id: "ad1", name: "Fatima", role: "admin", avatar_color: "#059669", preferred_lang: "en", is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (p === "/medications/photos" && req.method() === "POST") {
      uploads.push({ type: req.headers()["content-type"], size: req.postDataBuffer()?.length ?? 0 });
      return json({ image_url: url }, 201);
    }
    if (p.startsWith("/medications/photos/")) {
      // a tiny real JPEG so the preview loads
      return route.fulfill({ status: 200, contentType: "image/jpeg", body: Buffer.from("/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAP//////////////////////////////////////////////////////////////////////////////////////wgALCAABAAEBAREA/8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAQABPxA=", "base64") });
    }
    if (p.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
    return json([]);
  });
  await page.goto("/");

  // A noisy 3000x2000 phone photo (several MB as PNG).
  const big = await page.evaluate(() => {
    const c = document.createElement("canvas");
    c.width = 3000;
    c.height = 2000;
    const ctx = c.getContext("2d")!;
    const d = ctx.createImageData(3000, 2000);
    for (let i = 0; i < d.data.length; i++) d.data[i] = i % 4 === 3 ? 255 : Math.floor(Math.random() * 256);
    ctx.putImageData(d, 0, 0);
    return c.toDataURL("image/png").split(",")[1];
  });
  const buffer = Buffer.from(big, "base64");
  expect(buffer.length).toBeGreaterThan(1024 * 1024);

  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "Prescriptions Catalog" }).first().click();
  await page.getByRole("button", { name: "Add Medication" }).click();
  await expect(page.getByTestId("med-photo-button")).toContainText("Take or choose photos (box, back, the pill)");
  await page.getByTestId("med-photo-input").setInputFiles({ name: "box.png", mimeType: "image/png", buffer });

  await expect.poll(() => uploads.length).toBe(1);
  expect(uploads[0].type).toBe("image/jpeg");
  expect(uploads[0].size).toBeLessThan(1024 * 1024);
  await expect(page.locator(`img[src="${url}"]`).first()).toBeVisible();
});

// Several photos: the box front, the back, the pill. One is the main photo.
const TINY_JPEG = Buffer.from(
  "/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAP//////////////////////////////////////////////////////////////////////////////////////wgALCAABAAEBAREA/8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAQABPxA=",
  "base64",
);
const photoUrl = (n: number) => `/api/v1/medications/photos/0000000${n}-0000-4000-8000-000000000000.jpg`;

test("several photos at once; choose the main one, remove one, save them all", async ({ page, baseURL }) => {
  await page.setViewportSize({ width: 375, height: 844 });
  await page.context().addCookies([{ name: "birrcare_lang", value: "en", url: baseURL! }]);
  let uploads = 0;
  const saved: Record<string, unknown>[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const p = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (p.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
    if (p === "/auth/me") return json({ id: "ad1", name: "Fatima", role: "admin", avatar_color: "#059669", preferred_lang: "en", is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (p === "/medications/photos" && req.method() === "POST") return json({ image_url: photoUrl(++uploads) }, 201);
    if (p.startsWith("/medications/photos/")) return route.fulfill({ status: 200, contentType: "image/jpeg", body: TINY_JPEG });
    if (p === "/medications" && req.method() === "POST") {
      saved.push(req.postDataJSON());
      return json({ id: "m1" }, 201);
    }
    if (p.startsWith("/medications/schedule")) return json({ date: "2026-10-21", schedule: [] });
    return json([]);
  });
  await page.goto("/");
  await page.getByRole("button", { name: "Menu" }).click();
  await page.getByRole("button", { name: "Prescriptions Catalog" }).first().click();
  await page.getByRole("button", { name: "Add Medication" }).click();

  const shot = await page.evaluate(() => {
    const c = document.createElement("canvas");
    c.width = 200;
    c.height = 120;
    c.getContext("2d")!.fillRect(20, 20, 80, 40);
    return c.toDataURL("image/png").split(",")[1];
  });
  const file = (name: string) => ({ name, mimeType: "image/png", buffer: Buffer.from(shot, "base64") });
  await page.getByTestId("med-photo-input").setInputFiles([file("front.png"), file("back.png"), file("pill.png")]);

  const list = page.getByTestId("med-photo-list");
  await expect(list.locator("li")).toHaveCount(3);
  await expect(page.getByTestId("med-photo-button")).toContainText("Add another photo (3 of 6)");
  // The first photo is the main one; choose the pill instead, then drop the back.
  await expect(list.locator("li").nth(0)).toContainText("Main photo");
  await list.locator("li").nth(2).getByRole("button", { name: "Make it the main one" }).click();
  await expect(list.locator("li").nth(2)).toContainText("Main photo");
  await list.locator("li").nth(1).getByRole("button", { name: "Remove" }).click();
  await expect(list.locator("li")).toHaveCount(2);
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);

  await page.getByPlaceholder("مثال: كلوبيدوغريل (بلافيكس)").fill("إليكويس");
  await page.getByPlaceholder("مثال: 75mg قرص واحد").fill("5 mg");
  await page.getByRole("button", { name: "Save Prescription" }).click();
  await expect.poll(() => saved.length).toBe(1);
  expect(saved[0].photo_urls).toEqual([photoUrl(1), photoUrl(3)]);
  expect(saved[0].image_url).toBe(photoUrl(3));
});

test("ar: the dose card opens every photo with Next / Previous", async ({ page, baseURL }) => {
  await page.setViewportSize({ width: 375, height: 844 });
  await page.context().addCookies([{ name: "birrcare_lang", value: "ar", url: baseURL! }]);
  const med = {
    id: "m1", name_ar: "إليكويس", name_en: "Eliquis", dosage: "5 mg", frequency_slots: ["morning"],
    image_url: photoUrl(2), photo_urls: [photoUrl(1), photoUrl(2), photoUrl(3)], is_active: true, created_at: "2026-10-01T00:00:00Z",
  };
  await page.route("**/api/v1/**", async (route) => {
    const p = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(b) });
    if (p.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "event: connected\ndata: {}\n\n" });
    if (p === "/auth/me") return json({ id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: "ar", is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (p.startsWith("/medications/photos/")) return route.fulfill({ status: 200, contentType: "image/jpeg", body: TINY_JPEG });
    if (p.startsWith("/medications/schedule")) {
      return json({ date: "2026-10-21", schedule: [{ slot: "morning", medication: med }] });
    }
    return json([]);
  });
  await page.goto("/");
  await expect(page.getByTestId("med-photo-count").first()).toHaveText("3");
  await page.getByRole("button", { name: "تكبير صورة إليكويس" }).first().click();
  const gallery = page.getByTestId("med-photo-gallery");
  await expect(gallery).toContainText("1 من 3");
  // The main photo comes first.
  await expect(gallery.locator("img")).toHaveAttribute("src", photoUrl(2));
  await gallery.getByRole("button", { name: "التالية" }).click();
  await expect(gallery).toContainText("2 من 3");
  await expect(gallery.locator("img")).toHaveAttribute("src", photoUrl(1));
  await gallery.getByRole("button", { name: "السابقة" }).click();
  await gallery.getByRole("button", { name: "السابقة" }).click();
  await expect(gallery).toContainText("3 من 3");
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
});
