"use client";

import { useEffect, useState, useRef } from "react";
import { SSEEvent } from "@/lib/types";
import { AUTH_CHANGED_EVENT, AUTH_REQUIRED_EVENT } from "@/lib/api";

export type ConnectionStatus = "connected" | "connecting" | "disconnected";

interface UseLiveSyncOptions {
  onEvent?: (event: SSEEvent) => void;
  onMedLogged?: (event: SSEEvent) => void;
  onVitalLogged?: (event: SSEEvent) => void;
  onNoteAdded?: (event: SSEEvent) => void;
  onCaregiverSwitched?: (event: SSEEvent) => void;
  onShiftUpdated?: (event: SSEEvent) => void;
  onDutyUpdated?: (event: SSEEvent) => void;
  /**
   * Called whenever events may have been missed: after the stream re-opens
   * following a drop, and when the page returns from the background / bfcache.
   * Pages should silently re-fetch their current state here.
   */
  onResync?: () => void;
}

export const BACKOFF_BASE_MS = 1000;
export const BACKOFF_MAX_MS = 30000;
/** If the tab was hidden at least this long, assume the stream is dead (iOS freezes it). */
export const STALE_AFTER_HIDDEN_MS = 5000;

/** Exponential backoff with "equal jitter": uniform in [d/2, d], d = min(max, base * 2^attempt). */
export function backoffDelay(attempt: number, random: () => number = Math.random): number {
  const exp = Math.min(BACKOFF_MAX_MS, BACKOFF_BASE_MS * 2 ** Math.max(0, attempt));
  return Math.round(exp / 2 + random() * (exp / 2));
}

export function useLiveSync(options: UseLiveSyncOptions = {}) {
  const [status, setStatus] = useState<ConnectionStatus>("connecting");
  const [lastEvent, setLastEvent] = useState<SSEEvent | null>(null);
  const optionsRef = useRef(options);

  // Keep the latest callbacks without re-creating the connection (avoids stale closures).
  useEffect(() => {
    optionsRef.current = options;
  });

  useEffect(() => {
    if (typeof window === "undefined" || typeof EventSource === "undefined") return;

    const apiUrl = process.env.NEXT_PUBLIC_API_URL || "/api/v1";
    const streamUrl = `${apiUrl}/events/stream`;

    let es: EventSource | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    let attempt = 0;
    let hasOpenedOnce = false;
    let needsResync = false;
    let hiddenAt: number | null = null;
    let disposed = false;
    // EventSource hides HTTP status codes, so a 401 looks like any network error. While
    // paused for auth we stop reconnecting entirely and wait for a PIN login.
    let authPaused = false;

    const clearRetry = () => {
      if (retryTimer) {
        clearTimeout(retryTimer);
        retryTimer = null;
      }
    };

    const teardown = () => {
      if (es) {
        es.onopen = null;
        es.onmessage = null;
        es.onerror = null;
        es.close();
        es = null;
      }
    };

    const handleOpen = () => {
      attempt = 0;
      setStatus("connected");
      if (hasOpenedOnce && needsResync) {
        needsResync = false;
        optionsRef.current.onResync?.();
      }
      hasOpenedOnce = true;
    };

    const connect = () => {
      if (disposed || authPaused) return;
      clearRetry();
      teardown(); // never keep two streams open

      setStatus("connecting");
      const source = new EventSource(streamUrl, { withCredentials: true });
      es = source;

      source.onopen = () => {
        if (source !== es) return;
        handleOpen();
      };
      source.addEventListener("connected", () => {
        if (source !== es) return;
        setStatus("connected");
      });

      source.onmessage = (event) => {
        if (source !== es) return;
        let parsed: SSEEvent;
        try {
          parsed = JSON.parse(event.data);
        } catch (err) {
          console.warn("[SSE] Error parsing SSE payload:", err);
          return;
        }
        setLastEvent(parsed);
        const o = optionsRef.current;
        o.onEvent?.(parsed);
        switch (parsed.type) {
          case "MED_LOGGED":
            o.onMedLogged?.(parsed);
            break;
          case "VITAL_LOGGED":
            o.onVitalLogged?.(parsed);
            break;
          case "NOTE_ADDED":
            o.onNoteAdded?.(parsed);
            break;
          case "CAREGIVER_SWITCHED":
            o.onCaregiverSwitched?.(parsed);
            break;
          case "SHIFT_UPDATED":
            o.onShiftUpdated?.(parsed);
            break;
          case "DUTY_UPDATED":
            o.onDutyUpdated?.(parsed);
            break;
        }
        // Dispatch browser window event for toasts
        window.dispatchEvent(new CustomEvent("birrcare:sse_event", { detail: parsed }));
      };

      source.onerror = () => {
        if (source !== es) return;
        // Take over from the browser's fixed-interval auto-reconnect so we control backoff.
        teardown();
        setStatus("disconnected");
        needsResync = true;
        scheduleReconnect();
        if (!hasOpenedOnce || attempt >= 2) probeAuth();
      };
    };

    const pauseForAuth = () => {
      authPaused = true;
      clearRetry();
      teardown();
      setStatus("disconnected");
    };

    /** On repeated stream failures, check whether the cause is a missing/expired session. */
    const probeAuth = () => {
      if (typeof fetch === "undefined") return;
      fetch(`${apiUrl}/auth/me`, { credentials: "include", headers: { Accept: "application/json" } })
        .then((res) => {
          if (disposed || res.status !== 401) return;
          pauseForAuth();
          window.dispatchEvent(new CustomEvent(AUTH_REQUIRED_EVENT, { detail: { url: streamUrl } }));
        })
        .catch(() => {
          // network down: keep normal backoff
        });
    };

    const scheduleReconnect = () => {
      if (disposed) return;
      clearRetry();
      if (typeof navigator !== "undefined" && navigator.onLine === false) return; // wait for "online"
      const delay = backoffDelay(attempt);
      attempt += 1;
      retryTimer = setTimeout(connect, delay);
    };

    /** Reconnect immediately (reset backoff), e.g. on wake or network return. */
    const reconnectNow = () => {
      if (authPaused) return;
      attempt = 0;
      needsResync = true;
      connect();
    };

    const onVisibility = () => {
      if (document.visibilityState === "hidden") {
        hiddenAt = Date.now();
        return;
      }
      const hiddenFor = hiddenAt === null ? 0 : Date.now() - hiddenAt;
      hiddenAt = null;
      if (!es || es.readyState !== EventSource.OPEN || hiddenFor >= STALE_AFTER_HIDDEN_MS) {
        reconnectNow();
      } else if (hiddenFor > 0) {
        // Stream looks alive but we may have been frozen briefly: re-hydrate anyway.
        optionsRef.current.onResync?.();
      }
    };

    const onPageShow = (e: PageTransitionEvent) => {
      // Restored from bfcache: the old stream is gone.
      if (e.persisted) reconnectNow();
    };

    const onOnline = () => reconnectNow();
    const onAuthRequired = () => pauseForAuth();
    const onAuthChanged = () => {
      authPaused = false;
      reconnectNow();
    };
    const onOffline = () => {
      clearRetry();
      teardown();
      needsResync = true;
      setStatus("disconnected");
    };

    connect();
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("pageshow", onPageShow);
    window.addEventListener("online", onOnline);
    window.addEventListener("offline", onOffline);
    window.addEventListener(AUTH_REQUIRED_EVENT, onAuthRequired);
    window.addEventListener(AUTH_CHANGED_EVENT, onAuthChanged);

    return () => {
      disposed = true;
      clearRetry();
      teardown();
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("pageshow", onPageShow);
      window.removeEventListener("online", onOnline);
      window.removeEventListener("offline", onOffline);
      window.removeEventListener(AUTH_REQUIRED_EVENT, onAuthRequired);
      window.removeEventListener(AUTH_CHANGED_EVENT, onAuthChanged);
    };
  }, []);

  return { status, lastEvent };
}
