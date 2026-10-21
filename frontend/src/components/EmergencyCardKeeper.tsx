"use client";

import { useCallback, useEffect } from "react";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { emergencyPath, storeEmergencyToken } from "@/lib/emergencyLink";

const REFRESH_MS = 15 * 60_000;

/**
 * Issue #19: while someone is signed in, keep this phone's copy of the emergency
 * card fresh (every 15 minutes and when the app comes back to the front), so the
 * SOS button shows a recent card even with no signal. ?prefetch=1 from a
 * signed-in phone is not counted as "card opened".
 */
export function EmergencyCardKeeper() {
  const { currentCaregiver } = useCaregiver();
  const me = currentCaregiver?.id;

  const refresh = useCallback(async () => {
    let token: string;
    try {
      token = (await fetchApi<{ token: string }>("/patient/emergency-link")).token;
    } catch (err) {
      if ((err as { status?: number }).status === 404) storeEmergencyToken(null);
      return; // offline or signed out: keep what this phone has
    }
    storeEmergencyToken(token);
    // Goes through the service worker, which keeps the copy for offline use.
    fetch(`${emergencyPath(token)}?prefetch=1`, { credentials: "include" }).catch(() => {});
  }, []);

  useEffect(() => {
    if (!me) return;
    refresh();
    const timer = window.setInterval(refresh, REFRESH_MS);
    const onVisible = () => document.visibilityState === "visible" && refresh();
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [me, refresh]);

  return null;
}
