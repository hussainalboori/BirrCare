"use client";

import React, { createContext, useContext, useEffect, useState, useCallback } from "react";
import { Caregiver, CaregiverOption, TrustStatus } from "@/lib/types";
import { fetchApi, AUTH_CHANGED_EVENT, AUTH_REQUIRED_EVENT } from "@/lib/api";

export interface VerifyPinResult {
  success: boolean;
  error?: string;
  /** HTTP status of a failure (401 wrong PIN, 429 locked out, ...). */
  status?: number;
  /** Seconds until another attempt is allowed (from Retry-After on 429). */
  retryAfter?: number;
  /** PIN was correct but the account has 2FA: pass this to verifyTwoFactor with a code. */
  twoFactorChallenge?: string;
}

interface CaregiverContextType {
  currentCaregiver: Caregiver | null;
  allCaregivers: CaregiverOption[];
  isPinModalOpen: boolean;
  openPinModal: (targetCaregiverId?: string) => void;
  closePinModal: () => void;
  targetCaregiverId: string | null;
  verifyPin: (caregiverId: string, pin: string) => Promise<VerifyPinResult>;
  verifyTwoFactor: (challenge: string, code: string) => Promise<VerifyPinResult>;
  refreshCaregiver: () => Promise<void>;
  refreshCaregivers: () => Promise<void>;
  setCurrentCaregiver: (c: Caregiver) => void;
  trustStatus: TrustStatus | null;
  checkTrustStatus: () => Promise<TrustStatus | null>;
  switchCaregiverDirect: (caregiverId: string) => Promise<boolean>;
}

const CaregiverContext = createContext<CaregiverContextType | undefined>(undefined);

export function CaregiverProvider({ children }: { children: React.ReactNode }) {
  const [currentCaregiver, setCurrentCaregiver] = useState<Caregiver | null>(null);
  const [allCaregivers, setAllCaregivers] = useState<CaregiverOption[]>([]);
  const [isPinModalOpen, setIsPinModalOpen] = useState(false);
  const [targetCaregiverId, setTargetCaregiverId] = useState<string | null>(null);
  const [trustStatus, setTrustStatus] = useState<TrustStatus | null>(null);

  const fetchCaregiversList = useCallback(async () => {
    try {
      const list = await fetchApi<CaregiverOption[]>("/caregivers");
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

  const checkTrustStatus = useCallback(async (): Promise<TrustStatus | null> => {
    try {
      // Only a paired device or the home IP is trusted; the phone's location
      // never is (issue #53), so there is no GPS prompt.
      const res = await fetchApi<TrustStatus>("/auth/trust-status");
      if (res) {
        setTrustStatus(res);
        return res;
      }
    } catch (err) {
      console.warn("Failed checking trust status:", err);
    }
    return null;
  }, []);

  const switchCaregiverDirect = async (caregiverId: string): Promise<boolean> => {
    try {
      const res = await fetchApi<{ status: string; caregiver?: Caregiver; trust?: TrustStatus }>("/auth/switch-caregiver", {
        method: "POST",
        body: JSON.stringify({ caregiver_id: caregiverId }),
      });
      if (res?.caregiver) {
        setCurrentCaregiver(res.caregiver);
        if (res.trust) setTrustStatus(res.trust);
        closePinModal();
        window.dispatchEvent(new CustomEvent(AUTH_CHANGED_EVENT));
        fetchCaregiversList();
        return true;
      }
      openPinModal(caregiverId);
      return false;
    } catch (err: any) {
      // Untrusted (403 PIN_REQUIRED) or error -> fallback to PIN modal
      openPinModal(caregiverId);
      return false;
    }
  };

  useEffect(() => {
    fetchCaregiversList();
    refreshCaregiver();
    checkTrustStatus();
  }, [fetchCaregiversList, refreshCaregiver, checkTrustStatus]);

  // Any 401 (expired session, write without session) → prompt for PIN instead of failing silently.
  // The previous fallback to allCaregivers[0] attributed actions/UI to the wrong sibling.
  useEffect(() => {
    const onAuthRequired = () => {
      setCurrentCaregiver(null);
      setIsPinModalOpen(true);
    };
    const onAuthChanged = () => {
      checkTrustStatus();
    };
    window.addEventListener(AUTH_REQUIRED_EVENT, onAuthRequired);
    window.addEventListener(AUTH_CHANGED_EVENT, onAuthChanged);
    return () => {
      window.removeEventListener(AUTH_REQUIRED_EVENT, onAuthRequired);
      window.removeEventListener(AUTH_CHANGED_EVENT, onAuthChanged);
    };
  }, [checkTrustStatus]);

  const openPinModal = (targetId?: string) => {
    setTargetCaregiverId(targetId || null);
    setIsPinModalOpen(true);
  };

  const closePinModal = () => {
    setIsPinModalOpen(false);
    setTargetCaregiverId(null);
  };

  // Shared by the PIN step and the authenticator step.
  const signIn = async (endpoint: string, body: object): Promise<VerifyPinResult> => {
    try {
      const res = await fetchApi<{ status: string; caregiver?: Caregiver; challenge?: string }>(endpoint, {
        method: "POST",
        body: JSON.stringify(body),
      });
      if (res?.status === "2fa_required" && res.challenge) {
        return { success: false, twoFactorChallenge: res.challenge };
      }
      if (res && res.caregiver) {
        setCurrentCaregiver(res.caregiver);
        closePinModal();
        // New session cookie: let SSE (paused on 401) reconnect, and lists refresh.
        window.dispatchEvent(new CustomEvent(AUTH_CHANGED_EVENT));
        fetchCaregiversList();
        checkTrustStatus();
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

  const verifyPin = (caregiverId: string, pin: string) =>
    signIn("/auth/verify-pin", { caregiver_id: caregiverId, pin });

  const verifyTwoFactor = (challenge: string, code: string) =>
    signIn("/auth/verify-2fa", { challenge, code });

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
        verifyTwoFactor,
        refreshCaregiver,
        refreshCaregivers: fetchCaregiversList,
        setCurrentCaregiver,
        trustStatus,
        checkTrustStatus,
        switchCaregiverDirect,
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
