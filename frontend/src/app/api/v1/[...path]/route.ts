import { NextRequest, NextResponse } from "next/server";

function getBackendUrl(): string {
  return process.env.INTERNAL_BACKEND_URL || "http://birrcare-backend:8080";
}

async function proxyRequest(request: NextRequest, pathParams: { path: string[] }) {
  const path = pathParams.path.join("/");
  const search = request.nextUrl.search;
  const targetUrl = `${getBackendUrl()}/api/v1/${path}${search}`;

  const headers = new Headers();
  request.headers.forEach((value, key) => {
    // Exclude host header to let the target host resolve correctly
    if (key.toLowerCase() !== "host" && key.toLowerCase() !== "connection") {
      headers.set(key, value);
    }
  });

  const fetchOptions: RequestInit = {
    method: request.method,
    headers: headers,
    redirect: "manual",
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

    // If SSE text/event-stream, return the body stream directly
    if (backendResponse.headers.get("content-type")?.includes("text/event-stream")) {
      return new NextResponse(backendResponse.body, {
        status: backendResponse.status,
        headers: {
          "Content-Type": "text/event-stream",
          "Cache-Control": "no-cache, no-transform",
          "Connection": "keep-alive",
          "X-Accel-Buffering": "no",
        },
      });
    }

    const responseHeaders = new Headers();
    backendResponse.headers.forEach((val, key) => {
      responseHeaders.set(key, val);
    });

    const responseData = await backendResponse.arrayBuffer();
    return new NextResponse(responseData, {
      status: backendResponse.status,
      headers: responseHeaders,
    });
  } catch (error: any) {
    console.error(`[API Proxy] Error forwarding to ${targetUrl}:`, error);
    return NextResponse.json(
      { error: "BACKEND_UNREACHABLE", message: error.message },
      { status: 502 }
    );
  }
}

export async function GET(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxyRequest(req, await params);
}

export async function POST(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxyRequest(req, await params);
}

export async function PUT(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxyRequest(req, await params);
}

export async function DELETE(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxyRequest(req, await params);
}

export async function OPTIONS(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxyRequest(req, await params);
}
