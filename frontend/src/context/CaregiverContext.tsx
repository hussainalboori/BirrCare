"use client";

import React, { createContext, useContext, useEffect, useState, useCallback } from "react";
import { Caregiver } from "@/lib/types";
import { fetchApi } from "@/lib/api";

interface CaregiverContextType {
  currentCaregiver: Caregiver | null;
  allCaregivers: Caregiver[];
  isPinModalOpen: boolean;
  openPinModal: (targetCaregiverId?: string) => void;
  closePinModal: () => void;
  targetCaregiverId: string | null;
  verifyPin: (caregiverId: string, pin: string) => Promise<{ success: boolean; error?: string }>;
  refreshCaregiver: () => Promise<void>;
  setCurrentCaregiver: (c: Caregiver) => void;
}

const CaregiverContext = createContext<CaregiverContextType | undefined>(undefined);

export function CaregiverProvider({ children }: { children: React.ReactNode }) {
  const [currentCaregiver, setCurrentCaregiver] = useState<Caregiver | null>(null);
  const [allCaregivers, setAllCaregivers] = useState<Caregiver[]>([]);
  const [isPinModalOpen, setIsPinModalOpen] = useState(false);
  const [targetCaregiverId, setTargetCaregiverId] = useState<string | null>(null);

  const fetchCaregiversList = useCallback(async () => {
    try {
      const list = await fetchApi<Caregiver[]>("/caregivers");
      setAllCaregivers(list || []);
    } catch (err) {
      console.warn("Failed fetching caregivers list:", err);
    }
  }, []);

  const refreshCaregiver = useCallback(async () => {
    try {
      const me = await fetchApi<Caregiver>("/auth/me");
      if (me && me.id) {
        setCurrentCaregiver(me);
      }
    } catch {
      // If unauthenticated or dev fallback not set, we can select first caregiver once list loads
    }
  }, []);

  useEffect(() => {
    fetchCaregiversList();
    refreshCaregiver();
  }, [fetchCaregiversList, refreshCaregiver]);

  // If currentCaregiver is null after caregivers load, fallback to first
  useEffect(() => {
    if (!currentCaregiver && allCaregivers.length > 0) {
      setCurrentCaregiver(allCaregivers[0]);
    }
  }, [currentCaregiver, allCaregivers]);

  const openPinModal = (targetId?: string) => {
    setTargetCaregiverId(targetId || null);
    setIsPinModalOpen(true);
  };

  const closePinModal = () => {
    setIsPinModalOpen(false);
    setTargetCaregiverId(null);
  };

  const verifyPin = async (caregiverId: string, pin: string) => {
    try {
      const res = await fetchApi<{ status: string; caregiver: Caregiver }>("/auth/verify-pin", {
        method: "POST",
        body: JSON.stringify({ caregiver_id: caregiverId, pin }),
      });
      if (res && res.caregiver) {
        setCurrentCaregiver(res.caregiver);
        closePinModal();
        return { success: true };
      }
      return { success: false, error: "Authentication failed" };
    } catch (err: any) {
      return { success: false, error: err.data?.error || err.message || "Invalid PIN" };
    }
  };

  return (
    <CaregiverContext.Provider
      value={{
        currentCaregiver,
        allCaregivers,
        isPinModalOpen,
        openPinModal,
        closePinModal,
        targetCaregiverId,
        verifyPin,
        refreshCaregiver,
        setCurrentCaregiver,
      }}
    >
      {children}
    </CaregiverContext.Provider>
  );
}

export function useCaregiver() {
  const ctx = useContext(CaregiverContext);
  if (!ctx) {
    throw new Error("useCaregiver must be used within a CaregiverProvider");
  }
  return ctx;
}
