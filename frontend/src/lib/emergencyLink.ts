"use client";

import { useEffect, useState } from "react";

/**
 * Issue #19: the emergency card link on this phone. Kept in localStorage so the
 * SOS button still works in airplane mode (the card itself is in the service
 * worker cache, refreshed by EmergencyCardKeeper).
 */
const KEY = "birrcare_emergency_token";
export const EMERGENCY_LINK_EVENT = "birrcare:emergency-link";

export function readEmergencyToken(): string | null {
  try {
    return localStorage.getItem(KEY);
  } catch {
    return null;
  }
}

export function storeEmergencyToken(token: string | null) {
  try {
    if (token) localStorage.setItem(KEY, token);
    else localStorage.removeItem(KEY);
  } catch {
    // private mode: the SOS button then needs the network
  }
  window.dispatchEvent(new Event(EMERGENCY_LINK_EVENT));
}

export function emergencyPath(token: string): string {
  return `/emergency/${token}`;
}

export function useEmergencyToken(): string | null {
  const [token, setToken] = useState<string | null>(null);
  useEffect(() => {
    const read = () => setToken(readEmergencyToken());
    read();
    window.addEventListener(EMERGENCY_LINK_EVENT, read);
    return () => window.removeEventListener(EMERGENCY_LINK_EVENT, read);
  }, []);
  return token;
}
