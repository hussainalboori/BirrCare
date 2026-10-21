const API_BASE = process.env.NEXT_PUBLIC_API_URL || "/api/v1";

/** Fired on any 401 so the CaregiverProvider can open the PIN modal (and SSE can pause). */
export const AUTH_REQUIRED_EVENT = "birrcare:auth_required";
/** Fired after a successful PIN login so SSE can reconnect with the new session cookie. */
export const AUTH_CHANGED_EVENT = "birrcare:auth_changed";
/** Fired after a successful write (not sign-in) so a shared tablet can ask "Still Hussain?". */
export const WROTE_EVENT = "birrcare:wrote";

export interface ApiError extends Error {
  status: number;
  data: any;
  retryAfter?: number;
}

export async function fetchApi<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
  const url = endpoint.startsWith("http") ? endpoint : `${API_BASE}${endpoint.startsWith("/") ? "" : "/"}${endpoint}`;

  const response = await fetch(url, {
    ...options,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
      ...(options.headers || {}),
    },
  });

  if (!response.ok) {
    let errorData: any = {};
    try {
      errorData = await response.json();
    } catch {
      // not json
    }
    const error = new Error(errorData.message || errorData.error || `API error ${response.status}`) as ApiError;
    error.status = response.status;
    error.data = errorData;
    const retryAfter = Number(response.headers.get("Retry-After"));
    if (Number.isFinite(retryAfter) && retryAfter > 0) error.retryAfter = retryAfter;

    // Surface auth failures globally (except the PIN endpoint itself, which handles its own errors).
    if (response.status === 401 && typeof window !== "undefined" && !url.includes("/auth/verify-pin") && !url.includes("/auth/verify-2fa")) {
      window.dispatchEvent(new CustomEvent(AUTH_REQUIRED_EVENT, { detail: { url } }));
    }
    throw error;
  }

  const method = (options.method || "GET").toUpperCase();
  if (method !== "GET" && typeof window !== "undefined" && !url.includes("/auth/")) {
    window.dispatchEvent(new CustomEvent(WROTE_EVENT));
  }
  return response.json();
}
