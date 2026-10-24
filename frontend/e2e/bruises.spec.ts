import { test, expect, type Page } from "@playwright/test";

// On a strong blood thinner: record where a bruise is, its size, a photo, and
// bleeding signs, which show what to do and alert everyone.
type Bruise = Record<string, unknown>;

// A 1x1 JPEG, enough for the browser to open and shrink.
const JPEG = Buffer.from(
  "/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAP//////////////////////////////////////////////////////////////////////////////////////wgALCAABAAEBAREA/8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAQABPxA=",
  "base64",
);

async function open(page: Page, baseURL: string, locale: "ar" | "en", store: Bruise[], uploads: string[]) {
  await page.context().addCookies([{ name: "birrcare_lang", value: locale, url: baseURL }]);
  page.on("dialog", (d) => {
    throw new Error(`unexpected dialog: ${d.message()}`);
  });
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request();
    const p = new URL(req.url()).pathname.replace(/^\/api\/v1/, "");
    const json = (b: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(b) });
    if (p.startsWith("/events/stream")) return route.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 200\nevent: connected\ndata: {}\n\n" });
    if (p === "/auth/me") return json({ id: "c1", name: "Hussain", role: "caregiver", avatar_color: "#059669", preferred_lang: locale, is_active: true, created_at: "2026-10-01T00:00:00Z" });
    if (p === "/bruises/photos" && req.method() === "POST") {
      uploads.push(req.headers()["content-type"]);
      return json({ photo_url: "/api/v1/bruises/photos/11111111-1111-1111-1111-111111111111.jpg" }, 201);
    }
    if (p.startsWith("/bruises/photos/")) return route.fulfill({ status: 200, contentType: "image/jpeg", body: JPEG });
    if (p === "/bruises") {
      if (req.method() === "POST") {
        const body = req.postDataJSON();
        const saved = { id: `b${store.length + 1}`, caregiver_id: "c1", caregiver_name: "Hussain", created_at: new Date().toISOString(), ...body };
        store.unshift(saved);
        return json(saved, 201);
      }
      return json(store);
    }
    return json([]);
  });
  await page.goto("/vitals");
}

test("a bruise with its place, size and photo is saved and listed with the photo", async ({ page, baseURL }) => {
  await page.setViewportSize({ width: 375, height: 900 });
  const store: Bruise[] = [];
  const uploads: string[] = [];
  await open(page, baseURL!, "en", store, uploads);
  const card = page.getByTestId("bruise-card");
  await expect(card).toContainText("No bruises or bleeding recorded");

  await card.getByRole("button", { name: "Left arm" }).click();
  await card.getByRole("button", { name: /Medium/ }).click();
  await card.getByRole("button", { name: "New", exact: true }).click();
  // A real photo the browser can open (made on a canvas).
  const photo = await page.evaluate(() => {
    const c = document.createElement("canvas");
    c.width = 400;
    c.height = 300;
    const ctx = c.getContext("2d")!;
    ctx.fillStyle = "#7c3aed";
    ctx.fillRect(100, 80, 200, 140);
    return c.toDataURL("image/png").split(",")[1];
  });
  await card.getByTestId("bruise-photo-input").setInputFiles({ name: "bruise.png", mimeType: "image/png", buffer: Buffer.from(photo, "base64") });
  await expect(card.getByRole("img", { name: "Bruise photo" })).toBeVisible();
  await card.getByLabel("Note").fill("Bumped the door");
  await card.getByRole("button", { name: "Save bruise / bleeding as Hussain" }).click();

  await expect(card.getByRole("status")).toContainText("Bruise saved");
  expect(uploads).toEqual(["image/jpeg"]);
  expect(store[0]).toMatchObject({ body_part: "arm_left", size: "medium", change: "new", bleeding_signs: [], notes: "Bumped the door" });
  expect(String(store[0].photo_url)).toContain("/api/v1/bruises/photos/");
  const item = card.getByTestId("bruise-item").first();
  await expect(item).toContainText("Left arm · Medium (palm size) · New");
  await item.getByRole("button", { name: "Open the photo" }).click();
  await expect(page.getByRole("dialog", { name: "Bruise photo" })).toBeVisible();
  await page.getByRole("button", { name: "Close" }).click();
  // Ready for the next one; nothing wider than the phone.
  await expect(card.getByRole("button", { name: "Left arm" })).toHaveAttribute("aria-pressed", "false");
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
});

test("ar: a bleeding sign alone shows what to do and the call buttons", async ({ page, baseURL }) => {
  await page.setViewportSize({ width: 375, height: 900 });
  const store: Bruise[] = [];
  await open(page, baseURL!, "ar", store, []);
  const card = page.getByTestId("bruise-card");
  await card.getByLabel("نزيف من اللثة").check();
  await expect(card.getByRole("alert")).toContainText("اتصل بالطبيب اليوم");
  await card.getByLabel("براز أسود أو فيه دم").check();
  await expect(card.getByRole("alert")).toContainText("اتصل بالإسعاف الآن");
  await card.getByRole("button", { name: /احفظ الكدمة \/ النزيف/ }).click();
  await expect(card.getByRole("status")).toContainText("وصل تنبيه لكل الإخوة");
  expect(store[0]).toMatchObject({ body_part: "", bleeding_signs: ["gums", "stool"] });
  await expect(card.getByTestId("bruise-item").first()).toContainText("علامة نزيف");
  expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
});

test("nothing chosen: a clear message and no request", async ({ page, baseURL }) => {
  const store: Bruise[] = [];
  await open(page, baseURL!, "en", store, []);
  const card = page.getByTestId("bruise-card");
  await card.getByRole("button", { name: /Save bruise/ }).click();
  await expect(card.getByRole("alert")).toContainText("Choose where the bruise is, or a bleeding sign");
  expect(store).toHaveLength(0);
});
