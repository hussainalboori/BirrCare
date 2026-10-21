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
  await expect(page.getByTestId("med-photo-button")).toContainText("Take or choose a photo of the box");
  await page.getByTestId("med-photo-input").setInputFiles({ name: "box.png", mimeType: "image/png", buffer });

  await expect.poll(() => uploads.length).toBe(1);
  expect(uploads[0].type).toBe("image/jpeg");
  expect(uploads[0].size).toBeLessThan(1024 * 1024);
  await expect(page.locator(`img[src="${url}"]`).first()).toBeVisible();
});
