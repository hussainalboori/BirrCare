import { NextRequest } from "next/server";
import { renderEmergencyCard, renderEmergencyMessage, type EmergencyCard } from "@/lib/emergencyCard";

// Issue #19: the paramedics' card. A single self-contained HTML page (inline CSS,
// no scripts, fonts or other files), so Cloudflare Access needs a Bypass for
// /emergency/* only and it opens fast on any phone, logged in or not.

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

function backendUrl(): string {
  return process.env.INTERNAL_BACKEND_URL || "http://birrcare-backend:8080";
}

function html(body: string, status = 200): Response {
  return new Response(body, {
    status,
    headers: {
      "Content-Type": "text/html; charset=utf-8",
      "Cache-Control": "no-store",
      "Referrer-Policy": "no-referrer",
      "X-Robots-Tag": "noindex, nofollow",
    },
  });
}

export async function GET(request: NextRequest, { params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  if (!/^[A-Za-z0-9_-]{32,128}$/.test(token)) return html(renderEmergencyMessage("not_found"), 404);

  const headers = new Headers({ Accept: "application/json" });
  for (const name of ["cookie", "cf-connecting-ip", "cf-access-jwt-assertion", "user-agent"]) {
    const v = request.headers.get(name);
    if (v) headers.set(name, v);
  }
  const prefetch = request.nextUrl.searchParams.get("prefetch") === "1" ? "?prefetch=1" : "";
  let res: Response;
  try {
    res = await fetch(`${backendUrl()}/api/v1/emergency/${encodeURIComponent(token)}${prefetch}`, { headers, cache: "no-store" });
  } catch {
    return html(renderEmergencyMessage("unavailable"), 503);
  }
  if (res.status === 404) return html(renderEmergencyMessage("not_found"), 404);
  if (res.status === 429) return html(renderEmergencyMessage("too_many"), 429);
  if (!res.ok) return html(renderEmergencyMessage("unavailable"), 503);
  const card = (await res.json()) as EmergencyCard;
  return html(renderEmergencyCard(card));
}
