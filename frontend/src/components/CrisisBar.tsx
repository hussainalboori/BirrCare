"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { formatTime, parseServerTime } from "@/lib/datetime";
import type { SSEEvent, VitalsLog } from "@/lib/types";
import { useContacts } from "./CrisisActions";
import { Ltr } from "./Ltr";
import { Phone, Siren, Check } from "lucide-react";

// Issue #11: when a sibling logs a blood-pressure CRISIS, every page shows this bar
// with the call buttons for 30 minutes, until someone on this phone acknowledges
// it. It is rebuilt from recent readings on load and when the phone wakes, so a
// phone that was asleep or reloaded still shows it.

const WINDOW_MS = 30 * 60_000;
const ACK_KEY = "birrcare_crisis_ack";

interface Crisis {
  id: string;
  systolic: number;
  diastolic: number;
  by?: string;
  byId?: string;
  at: number;
}

function ackedIds(): string[] {
  try {
    return JSON.parse(localStorage.getItem(ACK_KEY) || "[]") as string[];
  } catch {
    return [];
  }
}

export function CrisisBar() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const contacts = useContacts();
  const [crisis, setCrisis] = useState<Crisis | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const meRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    meRef.current = currentCaregiver?.id;
  }, [currentCaregiver?.id]);
  const ar = locale === "ar";

  const consider = useCallback((c: Crisis | null) => {
    if (!c) return;
    // The person who logged it already sees the crisis screen on /vitals.
    if (c.byId && c.byId === meRef.current) return;
    if (Date.now() - c.at > WINDOW_MS || ackedIds().includes(c.id)) return;
    setCrisis((prev) => (prev && prev.at >= c.at ? prev : c));
  }, []);

  // Recent readings: covers reloads and phones that missed the live event.
  const loadRecent = useCallback(async () => {
    if (!meRef.current) return;
    const list = await fetchApi<VitalsLog[]>("/vitals?limit=10").catch(() => null);
    const v = list?.find((x) => x.bp_status === "CRISIS");
    const at = parseServerTime(v?.recorded_at as unknown as string)?.getTime();
    if (v && at && v.systolic != null && v.diastolic != null) {
      consider({ id: v.id, systolic: v.systolic, diastolic: v.diastolic, by: v.caregiver_name, byId: v.caregiver_id, at });
    }
  }, [consider]);

  useEffect(() => {
    if (!currentCaregiver?.id) return;
    loadRecent();
    const onVisible = () => document.visibilityState === "visible" && loadRecent();
    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, [currentCaregiver?.id, loadRecent]);

  // Live: the same window event SseToast listens to.
  useEffect(() => {
    const onEvent = (e: Event) => {
      const ev = (e as CustomEvent<SSEEvent>).detail;
      if (ev?.type !== "VITAL_LOGGED") return;
      const p = ev.payload as { id?: string; bp_status?: string; systolic?: number; diastolic?: number; recorded_at?: string } | undefined;
      if (p?.bp_status !== "CRISIS" || !p.id || p.systolic == null || p.diastolic == null) return;
      consider({
        id: p.id,
        systolic: p.systolic,
        diastolic: p.diastolic,
        by: ev.actor?.name,
        byId: ev.actor?.id,
        at: parseServerTime(p.recorded_at)?.getTime() ?? Date.now(),
      });
    };
    window.addEventListener("birrcare:sse_event", onEvent);
    return () => window.removeEventListener("birrcare:sse_event", onEvent);
  }, [consider]);

  useEffect(() => {
    if (!crisis) return;
    const id = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(id);
  }, [crisis]);

  if (!crisis || now - crisis.at > WINDOW_MS) return null;

  const acknowledge = () => {
    try {
      localStorage.setItem(ACK_KEY, JSON.stringify([...ackedIds(), crisis.id].slice(-20)));
    } catch {}
    setCrisis(null);
  };
  const doctorLabel = contacts.doctor_name || (ar ? "الطبيب" : "Doctor");

  return (
    <div
      role="alertdialog"
      aria-live="assertive"
      aria-label={ar ? "ضغط دم خطير" : "Blood pressure crisis"}
      data-testid="crisis-bar"
      className="fixed inset-x-0 top-0 z-[60] bg-rose-700 px-3 pb-3 pt-[calc(0.75rem+env(safe-area-inset-top))] text-white shadow-2xl"
    >
      <div className="mx-auto max-w-3xl space-y-2">
        <p className="text-base font-black leading-snug">
          ⚠ {ar ? "ضغط دم خطير" : "Blood pressure crisis"}: <Ltr>{crisis.systolic}/{crisis.diastolic}</Ltr>
          <span className="block text-sm font-semibold">
            {crisis.by && (
              <>
                {ar ? "سجّله" : "Logged by"} <bdi>{crisis.by}</bdi>{" "}
              </>
            )}
            <bdi dir="auto">{formatTime(new Date(crisis.at).toISOString(), locale)}</bdi>
          </span>
        </p>
        <div className="grid grid-cols-2 gap-2">
          <a
            href={`tel:${contacts.emergency}`}
            className="flex min-h-12 items-center justify-center gap-1.5 rounded-xl bg-white px-2 text-base font-black text-rose-800"
          >
            <Siren className="h-5 w-5 shrink-0" />
            <span>{ar ? "الإسعاف" : "Emergency"}</span>
            <Ltr>{contacts.emergency}</Ltr>
          </a>
          {contacts.doctor_phone ? (
            <a
              href={`tel:${contacts.doctor_phone}`}
              className="flex min-h-12 items-center justify-center gap-1.5 rounded-xl border-2 border-white px-2 text-base font-black text-white"
            >
              <Phone className="h-5 w-5 shrink-0" />
              <bdi className="truncate">{doctorLabel}</bdi>
            </a>
          ) : (
            <button
              type="button"
              onClick={acknowledge}
              className="flex min-h-12 items-center justify-center gap-1.5 rounded-xl border-2 border-white px-2 text-base font-bold text-white"
            >
              <Check className="h-5 w-5" />
              {ar ? "تم الاطلاع" : "Seen"}
            </button>
          )}
        </div>
        {contacts.doctor_phone && (
          <button
            type="button"
            onClick={acknowledge}
            className="flex min-h-12 w-full items-center justify-center gap-1.5 rounded-xl bg-rose-900/50 text-base font-bold text-white"
          >
            <Check className="h-5 w-5" />
            {ar ? "تم الاطلاع" : "Seen"}
          </button>
        )}
      </div>
    </div>
  );
}

/** Admins see this on every page while the server has no EMERGENCY_NUMBER. */
export function EmergencySetupBanner() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const contacts = useContacts();
  const role = currentCaregiver?.role;
  if (contacts.configured !== false || (role !== "admin" && role !== "super_admin")) return null;
  return (
    <div role="alert" data-testid="emergency-setup-banner" className="bg-rose-700 px-4 py-2 text-center text-sm font-bold text-white">
      {locale === "ar"
        ? `رقم الطوارئ غير مضبوط. زر الاتصال يستخدم الرقم الاحتياطي ${contacts.emergency}. اضبط EMERGENCY_NUMBER في ملف .env على الخادم.`
        : `Emergency number not configured. The call button uses the fallback ${contacts.emergency}. Set EMERGENCY_NUMBER in the server's .env.`}
    </div>
  );
}
