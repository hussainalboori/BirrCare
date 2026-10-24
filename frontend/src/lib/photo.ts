import { fetchApi } from "@/lib/api";

// Medicine photos (the box or the pill) taken with the phone camera. A phone
// photo is 3-8 MB; it is shrunk here to at most MAX_SIDE pixels as a JPEG of
// a few hundred KB, which also keeps it under the server's 1 MB body limit.
// The server re-saves it again, dropping any location data.

const MAX_SIDE = 1280;
const MAX_BYTES = 900 * 1024;

function toJpeg(canvas: HTMLCanvasElement, quality: number): Promise<Blob> {
  return new Promise((resolve, reject) =>
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error("encode failed"))), "image/jpeg", quality)
  );
}

/** Any photo the browser can open (JPEG, PNG, HEIC on iPhone, WebP) → a small JPEG. */
export async function shrinkPhoto(file: File): Promise<Blob> {
  // Applies the camera's rotation, so a photo taken upright stays upright.
  const bitmap = await createImageBitmap(file, { imageOrientation: "from-image" });
  let side = MAX_SIDE;
  for (let attempt = 0; attempt < 4; attempt++) {
    const scale = Math.min(1, side / Math.max(bitmap.width, bitmap.height));
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("no canvas");
    ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    const blob = await toJpeg(canvas, attempt === 0 ? 0.82 : 0.7);
    if (blob.size <= MAX_BYTES) {
      bitmap.close();
      return blob;
    }
    side = Math.round(side * 0.75);
  }
  bitmap.close();
  throw new Error("photo too large");
}

/** Uploads a medicine photo and returns the image_url to save on the medicine. */
export async function uploadMedicinePhoto(file: File): Promise<string> {
  const blob = await shrinkPhoto(file);
  const res = await fetchApi<{ image_url: string }>("/medications/photos", {
    method: "POST",
    headers: { "Content-Type": "image/jpeg" },
    body: blob,
  });
  return res.image_url;
}

/** Uploads a bruise photo (kept private, like medicine photos) and returns its photo_url. */
export async function uploadBruisePhoto(file: File): Promise<string> {
  const blob = await shrinkPhoto(file);
  const res = await fetchApi<{ photo_url: string }>("/bruises/photos", {
    method: "POST",
    headers: { "Content-Type": "image/jpeg" },
    body: blob,
  });
  return res.photo_url;
}
