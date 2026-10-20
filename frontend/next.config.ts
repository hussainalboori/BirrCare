import type { NextConfig } from "next";

const isDev = process.env.NODE_ENV !== "production";

// The API is normally same-origin through the proxy route; allow an absolute
// NEXT_PUBLIC_API_URL origin too (SSE and fetch).
function apiOrigin(): string {
  try {
    return new URL(process.env.NEXT_PUBLIC_API_URL || "").origin;
  } catch {
    return "";
  }
}

// 'unsafe-inline' scripts: App Router streams inline bootstrap scripts, and nonces
// would force every page to render dynamically. No XSS sink (no
// dangerouslySetInnerHTML) exists; frame-ancestors is the main win here.
const csp = [
  "default-src 'self'",
  `script-src 'self' 'unsafe-inline'${isDev ? " 'unsafe-eval'" : ""}`,
  "style-src 'self' 'unsafe-inline'",
  // Medicine photos may be any https URL an admin pastes; the 2FA QR code is a data: URL.
  "img-src 'self' data: blob: https:",
  "font-src 'self'",
  `connect-src 'self' ${apiOrigin()}${isDev ? " ws:" : ""}`.trim(),
  "worker-src 'self'",
  "manifest-src 'self'",
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "frame-ancestors 'none'",
].join("; ");

const nextConfig: NextConfig = {
  /* Dynamic proxying is handled by src/app/api/v1/[...path]/route.ts */
  poweredByHeader: false,
  async headers() {
    return [
      {
        source: "/(.*)",
        headers: [
          { key: "Content-Security-Policy", value: csp },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "Referrer-Policy", value: "same-origin" },
          { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=(), payment=()" },
        ],
      },
    ];
  },
};

export default nextConfig;
