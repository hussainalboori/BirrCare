"use client";

import React, { createContext, useContext, useEffect, useState } from "react";
import { fetchApi } from "@/lib/api";
import { getHouseholdZone, setHouseholdZone } from "@/lib/householdTime";

// Issue #58: the household time zone, from /health, for everything that decides
// "today" or "overdue". Components that use it re-render once it is known.
const HouseholdTimeContext = createContext<string>("");

export function HouseholdTimeProvider({ children }: { children: React.ReactNode }) {
  const [zone, setZone] = useState<string>(() => getHouseholdZone());
  useEffect(() => {
    let alive = true;
    fetchApi<{ timezone?: string }>("/health")
      .then((h) => {
        if (alive && setHouseholdZone(h?.timezone)) setZone(getHouseholdZone());
      })
      .catch(() => {}); // offline: keep the saved (or this phone's) zone
    return () => {
      alive = false;
    };
  }, []);
  return <HouseholdTimeContext.Provider value={zone}>{children}</HouseholdTimeContext.Provider>;
}

/** The household zone name; changes once when /health answers. */
export function useHouseholdZone(): string {
  return useContext(HouseholdTimeContext) || getHouseholdZone();
}
