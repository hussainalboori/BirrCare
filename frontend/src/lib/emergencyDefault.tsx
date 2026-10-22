"use client";

import { createContext, useContext, type ReactNode } from "react";

// The household's emergency number from the server's settings (EMERGENCY_NUMBER),
// put into every page by the root layout. It is the fallback when a phone has no
// saved contacts and can't reach the server; the page is cached for offline use,
// so it still works without a connection. It used to be compiled into the app,
// which tied each image to one household (issue: prebuilt images).
const EmergencyDefaultContext = createContext<string>("112");

export function EmergencyDefaultProvider({ value, children }: { value: string; children: ReactNode }) {
  return <EmergencyDefaultContext.Provider value={value || "112"}>{children}</EmergencyDefaultContext.Provider>;
}

/** The server's emergency number, else 112 (the GSM number most networks route). */
export function useDefaultEmergency(): string {
  return useContext(EmergencyDefaultContext);
}
