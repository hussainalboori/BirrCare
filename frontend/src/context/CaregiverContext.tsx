"use client";

import React, { createContext, useContext, useEffect, useState, useCallback } from "react";
import { Caregiver } from "@/lib/types";
import { fetchApi, AUTH_CHANGED_EVENT, AUTH_REQUIRED_EVENT } from "@/lib/api";

export interface VerifyPinResult {
  success: boolean;
  error?: string;
  /** HTTP status of a failure (401 wrong PIN, 429 locked out, ...). */
  status?: number;
  /** Seconds until another attempt is allowed (from Retry-After on 429). */
  retryAfter?: number;
}

interface CaregiverContextType {
  currentCaregiver: Caregiver | null;
  allCaregivers: Caregiver[];
  isPinModalOpen: boolean;
  openPinModal: (targetCaregiverId?: string) => void;
  closePinModal: () => void;
  targetCaregiverId: string | null;
  verifyPin: (caregiverId: string, pin: string) => Promise<VerifyPinResult>;
  refreshCaregiver: () => Promise<void>;
  refreshCaregivers: () => Promise<void>;
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
    } catch (err: any) {
      // No silent default caregiver: an unauthenticated device must identify via PIN.
      // (fetchApi also dispatches AUTH_REQUIRED_EVENT on 401, handled below.)
      if (err?.status === 401) {
        setCurrentCaregiver(null);
        setIsPinModalOpen(true);
      }
    }
  }, []);

  useEffect(() => {
    fetchCaregiversList();
    refreshCaregiver();
  }, [fetchCaregiversList, refreshCaregiver]);

  // Any 401 (expired session, write without session) → prompt for PIN instead of failing silently.
  // The previous fallback to allCaregivers[0] attributed actions/UI to the wrong sibling.
  useEffect(() => {
    const onAuthRequired = () => {
      setCurrentCaregiver(null);
      setIsPinModalOpen(true);
    };
    window.addEventListener(AUTH_REQUIRED_EVENT, onAuthRequired);
    return () => window.removeEventListener(AUTH_REQUIRED_EVENT, onAuthRequired);
  }, []);

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
        // New session cookie: let SSE (paused on 401) reconnect, and lists refresh.
        window.dispatchEvent(new CustomEvent(AUTH_CHANGED_EVENT));
        fetchCaregiversList();
        return { success: true };
      }
      return { success: false, error: "Authentication failed" };
    } catch (err: any) {
      return {
        success: false,
        status: err?.status,
        retryAfter: err?.retryAfter,
        error: err.data?.error || err.message || "Invalid PIN",
      };
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
        refreshCaregivers: fetchCaregiversList,
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
