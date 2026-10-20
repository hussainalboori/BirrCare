"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { formatTime } from "@/lib/datetime";
import { playAlert } from "@/lib/alertSound";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import type { SSEEvent } from "@/lib/types";
import { useContacts } from "./CrisisActions";
import { Ltr } from "./Ltr";
import { Phone, Siren, Check, AlertTriangle } from "lucide-react";

// Alerts every sibling must see (issues #11, #12): a BP crisis, a dose given inside
// its minimum interval, vomiting after a dose. They come from the server
// (/alerts/unacked), so one missed while the phone was locked or the app closed is
// replayed on the next open or reconnect, with the alarm, until this caregiver
// taps "Seen". The crisis bar keeps both call buttons on screen on every page.

export interface ServerAlert {
  id: string;
  kind: "bp_crisis" | "dose_override" | "vomited";
  created_by_name?: string;
  payload: Record<string, unknown>;
  created_at: string;
}

export function AlertCenter() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const contacts = useContacts();
  const [alerts, setAlerts] = useState<ServerAlert[]>([]);
  const announced = useRef(new Set<string>());
  const ar = locale === "ar";
  const me = currentCaregiver?.id;

  const load = useCallback(async () => {
    if (!me) return;
    const list = await fetchApi<ServerAlert[]>("/alerts/unacked").catch(() => null);
    if (!Array.isArray(list)) return; // offline or unexpected: keep what is on screen
    setAlerts(list);
    const fresh = list.filter((a) => !announced.current.has(a.id));
    if (fresh.length) {
      fresh.forEach((a) => announced.current.add(a.id));
      playAlert("crisis");
      if (typeof navigator !== "undefined" && "vibrate" in navigator) navigator.vibrate?.([400, 150, 400, 150, 400]);
    }
  }, [me]);

  // On open / profile change, when the phone wakes, and after every stream resync.
  useEffect(() => {
    if (!me) {
      setAlerts([]);
      return;
    }
    load();
    const onVisible = () => document.visibilityState === "visible" && load();
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener(RESYNC_EVENT, load);
    return () => {
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener(RESYNC_EVENT, load);
    };
  }, [me, load]);

  // Live: any event that created an alert.
  useEffect(() => {
    const onEvent = (e: Event) => {
      const ev = (e as CustomEvent<SSEEvent>).detail;
      if ((ev?.payload as { alert_id?: string } | undefined)?.alert_id) load();
    };
    window.addEventListener("birrcare:sse_event", onEvent);
    return () => window.removeEventListener("birrcare:sse_event", onEvent);
  }, [load]);

  // Keep the service worker registered so pushes reach this phone when locked.
  useEffect(() => {
    navigator.serviceWorker?.register("/sw.js").catch(() => {});
  }, []);

  const current = alerts[0];
  if (!current) return null;

  const acknowledge = async () => {
    const id = current.id;
    try {
      await fetchApi(`/alerts/${encodeURIComponent(id)}/ack`, { method: "POST" });
      setAlerts((list) => list.filter((a) => a.id !== id));
    } catch {
      // Stay on screen; the caregiver can tap again once the connection is back.
    }
  };

  const crisis = current.kind === "bp_crisis";
  const p = current.payload;
  const medName = String((ar ? p.name_ar : p.name_en) || p.name_en || p.name_ar || "");
  const when = <bdi dir="auto">{formatTime(current.created_at, locale)}</bdi>;
  const by = current.created_by_name ? <bdi>{current.created_by_name}</bdi> : null;
  const more = alerts.length > 1 ? (ar ? ` (+${alerts.length - 1} تنبيه آخر)` : ` (+${alerts.length - 1} more)`) : "";
  const doctorLabel = contacts.doctor_name || (ar ? "الطبيب" : "Doctor");

  const seenButton = (
    <button
      type="button"
      onClick={acknowledge}
      className={`flex min-h-12 w-full items-center justify-center gap-1.5 rounded-xl text-base font-bold ${
        crisis ? "bg-rose-900/50 text-white" : "bg-amber-950/30 text-white"
      }`}
    >
      <Check className="h-5 w-5" />
      {ar ? "تم الاطلاع" : "Seen"}
    </button>
  );

  return (
    <div
      role="alertdialog"
      aria-live="assertive"
      aria-label={crisis ? (ar ? "ضغط دم خطير" : "Blood pressure crisis") : ar ? "تنبيه دواء" : "Medication alert"}
      data-testid={crisis ? "crisis-bar" : "alert-bar"}
      className={`fixed inset-x-0 top-0 z-[60] px-3 pb-3 pt-[calc(0.75rem+env(safe-area-inset-top))] text-white shadow-2xl ${
        crisis ? "bg-rose-700" : "bg-amber-800"
      }`}
    >
      <div className="mx-auto max-w-3xl space-y-2">
        {crisis ? (
          <>
            <p className="text-base font-black leading-snug">
              ⚠ {ar ? "ضغط دم خطير" : "Blood pressure crisis"}:{" "}
              <Ltr>
                {String(p.systolic)}/{String(p.diastolic)}
              </Ltr>
              {more}
              <span className="block text-sm font-semibold">
                {by && (
                  <>
                    {ar ? "سجّله" : "Logged by"} {by}{" "}
                  </>
                )}
                {when}
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
                seenButton
              )}
            </div>
            {contacts.doctor_phone && seenButton}
          </>
        ) : (
          <>
            <p className="flex items-start gap-2 text-base font-bold leading-snug">
              <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0" />
              <span>
                {current.kind === "vomited"
                  ? ar
                    ? "استفرغت بعد جرعة"
                    : "Vomited after a dose"
                  : ar
                  ? "أُعطيت جرعة قبل موعدها المسموح (بأمر الطبيب)"
                  : "A dose was given inside its minimum interval (doctor's order)"}
                {medName && (
                  <>
                    : <bdi>{medName}</bdi>
                  </>
                )}
                {more}
                <span className="block text-sm font-semibold">
                  {by} {when}
                </span>
              </span>
            </p>
            {seenButton}
          </>
        )}
      </div>
    </div>
  );
}

/** Admins see this on every page while the server has no EMERGENCY_NUMBER (issue #11). */
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
