import { NextRequest, NextResponse } from "next/server";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

function getBackendUrl(): string {
  return process.env.INTERNAL_BACKEND_URL || "http://birrcare-backend:8080";
}

/**
 * Only these request headers are forwarded to the Go backend. Forwarding
 * everything (the previous behaviour) passed through hop-by-hop headers,
 * Accept-Encoding (backend gzip would be decoded by fetch but the
 * Content-Encoding header still forwarded → corrupted body), and arbitrary
 * client-controlled X-* headers.
 *
 * NOTE: Cf-Access-* identity headers are injected by the Cloudflare edge and
 * must reach the backend for auth. They are only trustworthy because the
 * services are bound to 127.0.0.1 behind the tunnel; the backend should
 * verify Cf-Access-Jwt-Assertion rather than trust the email header alone.
 */
const FORWARDED_REQUEST_HEADERS = [
  "accept",
  "accept-language",
  "authorization",
  "content-type",
  "cookie",
  "if-none-match",
  "last-event-id",
  "user-agent",
  "x-csrf-token",
  "cf-access-authenticated-user-email",
  "cf-access-jwt-assertion",
  "cf-connecting-ip",
];

/** Response headers that must not be copied from the upstream response. */
const HOP_BY_HOP_RESPONSE_HEADERS = new Set([
  "connection",
  "keep-alive",
  "transfer-encoding",
  "content-encoding", // fetch() already decoded the body
  "content-length", // may no longer match after decoding
  "proxy-authenticate",
  "proxy-authorization",
  "te",
  "trailer",
  "upgrade",
]);

async function proxyRequest(request: NextRequest, pathParams: { path: string[] }) {
  if (pathParams.path.some((seg) => seg === "." || seg === "..")) {
    return NextResponse.json({ error: "BAD_PATH" }, { status: 400 });
  }
  const path = pathParams.path.map(encodeURIComponent).join("/");
  const search = request.nextUrl.search;
  const targetUrl = `${getBackendUrl()}/api/v1/${path}${search}`;

  const headers = new Headers();
  for (const name of FORWARDED_REQUEST_HEADERS) {
    const value = request.headers.get(name);
    if (value !== null) headers.set(name, value);
  }
  const clientIp = request.headers.get("cf-connecting-ip") || request.headers.get("x-forwarded-for");
  if (clientIp) headers.set("x-forwarded-for", clientIp);

  const fetchOptions: RequestInit = {
    method: request.method,
    headers,
    redirect: "manual",
    cache: "no-store",
    // Abort the upstream request (incl. long-lived SSE streams) when the browser disconnects,
    // so reconnecting clients don't leave orphaned subscribers in the Go broker.
    signal: request.signal,
  };

  if (request.method !== "GET" && request.method !== "HEAD") {
    try {
      fetchOptions.body = await request.arrayBuffer();
    } catch {
      // no body
    }
  }

  try {
    const backendResponse = await fetch(targetUrl, fetchOptions);

    // SSE: pipe the body stream straight through, unbuffered.
    if (backendResponse.headers.get("content-type")?.includes("text/event-stream")) {
      return new NextResponse(backendResponse.body, {
        status: backendResponse.status,
        headers: {
          "Content-Type": "text/event-stream; charset=utf-8",
          "Cache-Control": "no-cache, no-transform",
          Connection: "keep-alive",
          "X-Accel-Buffering": "no",
        },
      });
    }

    const responseHeaders = new Headers();
    backendResponse.headers.forEach((val, key) => {
      if (HOP_BY_HOP_RESPONSE_HEADERS.has(key.toLowerCase())) return;
      // append (not set) so multiple Set-Cookie headers survive
      responseHeaders.append(key, val);
    });

    const responseData = await backendResponse.arrayBuffer();
    return new NextResponse(responseData, {
      status: backendResponse.status,
      headers: responseHeaders,
    });
  } catch (error: unknown) {
    if (request.signal.aborted) {
      // Client went away; nothing to respond to.
      return new NextResponse(null, { status: 499 });
    }
    console.error(`[API Proxy] Error forwarding to ${targetUrl}:`, error);
    return NextResponse.json(
      { error: "BACKEND_UNREACHABLE", message: "Backend service is unreachable" },
      { status: 502 }
    );
  }
}

type RouteContext = { params: Promise<{ path: string[] }> };

export async function GET(req: NextRequest, { params }: RouteContext) {
  return proxyRequest(req, await params);
}

export async function POST(req: NextRequest, { params }: RouteContext) {
  return proxyRequest(req, await params);
}

export async function PUT(req: NextRequest, { params }: RouteContext) {
  return proxyRequest(req, await params);
}

export async function PATCH(req: NextRequest, { params }: RouteContext) {
  return proxyRequest(req, await params);
}

export async function DELETE(req: NextRequest, { params }: RouteContext) {
  return proxyRequest(req, await params);
}

export async function OPTIONS(req: NextRequest, { params }: RouteContext) {
  return proxyRequest(req, await params);
}
