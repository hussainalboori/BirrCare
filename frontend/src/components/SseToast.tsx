"use client";

import { haptic } from "@/lib/theme";
import React, { useEffect, useRef, useState } from "react";
import { SSEEvent } from "@/lib/types";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { installAudioUnlock, playAlert } from "@/lib/alertSound";
import { DOSE_COPY } from "@/lib/doseWarnings";
import { tagLabel } from "@/lib/handoverTags";
import { Ltr } from "./Ltr";
import { Pill, Activity, ClipboardList, UserCheck, X, AlertTriangle, Syringe } from "lucide-react";

interface MedPayload {
  name_ar?: string;
  name_en?: string;
  slot?: string;
  status?: string;
  interval_override?: boolean;
  override_reason?: string;
}
interface VitalPayload {
  systolic?: number;
  diastolic?: number;
  bp_status?: string;
  spo2?: number;
  spo2_status?: string;
  temperature_c?: number;
}
interface InsulinPayload {
  name_ar?: string;
  name_en?: string;
  status?: string;
  units?: number;
  blood_sugar?: number | null;
  reason?: string;
}
interface NotePayload {
  summary?: string;
  urgent_flag?: boolean;
  tags?: string[];
  passed_to_name?: string;
}

// Amber, with a stronger vibration, so nobody misses it: an admin gave a dose inside the
// minimum interval (#7), a dose was only partly taken, or she vomited after it (#9).
function isAmber(event: SSEEvent): boolean {
  const p = event.payload as MedPayload | undefined;
  return (event.type === "MED_LOGGED" && (!!p?.interval_override || p?.status === "partial")) || event.type === "MED_EVENT";
}

function isCrisis(event: SSEEvent): boolean {
  const p = event.payload as VitalPayload | undefined;
  return event.type === "VITAL_LOGGED" && (p?.bp_status === "CRISIS" || p?.spo2_status === "CRITICAL");
}

export function SseToast() {
  const { t, locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const [toast, setToast] = useState<{ event: SSEEvent; id: number } | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Read inside the listener without re-subscribing on every profile switch.
  const selfIdRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    selfIdRef.current = currentCaregiver?.id;
  }, [currentCaregiver?.id]);

  useEffect(() => {
    installAudioUnlock();

    const handleEvent = (e: Event) => {
      const event = (e as CustomEvent<SSEEvent>).detail;
      if (!event || !event.actor) return;
      // The person who just tapped already sees the result on their own card.
      if (event.actor.id && event.actor.id === selfIdRef.current) return;

      // Events that created an alert (crisis, dose override, vomited) are shown by the
      // AlertCenter bar with its alarm until acknowledged; no toast on top of it.
      if ((event.payload as { alert_id?: string } | undefined)?.alert_id) return;
      const crisis = isCrisis(event);
      const amber = isAmber(event);
      const urgentNote = event.type === "NOTE_ADDED" && !!(event.payload as NotePayload | undefined)?.urgent_flag;

      setToast({ event, id: Date.now() });
      if (timerRef.current) clearTimeout(timerRef.current);
      // A crisis stays on screen until someone dismisses it.
      if (!crisis) timerRef.current = setTimeout(() => setToast(null), urgentNote || amber ? 15000 : 6000);

      playAlert(crisis ? "crisis" : "chime");
      haptic(crisis ? [400, 150, 400, 150, 400] : amber ? [200, 100, 200] : 60, { crisis });
    };

    window.addEventListener("birrcare:sse_event", handleEvent);
    return () => {
      window.removeEventListener("birrcare:sse_event", handleEvent);
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  if (!toast) return null;

  const { event } = toast;
  const actor = event.actor;
  const crisis = isCrisis(event);

  let Icon = Pill;
  let actionText = "";
  let detail: React.ReactNode = null;

  switch (event.type) {
    case "MED_LOGGED": {
      const p = (event.payload || {}) as MedPayload;
      Icon = Pill;
      actionText = p.interval_override
        ? locale === "ar" ? `أعطى جرعة قبل موعدها المسموح: ${p.override_reason || ""}` : `gave a dose inside the minimum interval: ${p.override_reason || ""}`
        : p.status === "partial"
          ? DOSE_COPY[locale].toastPartial
          : p.status === "refused"
          ? DOSE_COPY[locale].toastRefused
          : p.status === "skipped"
          ? locale === "ar" ? "سجّل تخطي جرعة" : "skipped a dose"
          : locale === "ar" ? "أعطى جرعة" : "gave a dose";
      const name = locale === "ar" ? p.name_ar || p.name_en : p.name_en || p.name_ar;
      const slot = p.slot ? (t.slots as Record<string, string>)[p.slot] || p.slot : "";
      if (name) detail = <><bdi>{name}</bdi>{slot && ` · ${slot}`}</>;
      break;
    }
    case "MED_EVENT": {
      const p = (event.payload || {}) as MedPayload & { minutes_after?: number | null };
      Icon = AlertTriangle;
      actionText = DOSE_COPY[locale].toastVomited(p.minutes_after ?? null);
      const name = locale === "ar" ? p.name_ar || p.name_en : p.name_en || p.name_ar;
      const slot = p.slot ? (t.slots as Record<string, string>)[p.slot] || p.slot : "";
      if (name) detail = <><bdi>{name}</bdi>{slot && ` · ${slot}`}</>;
      break;
    }
    case "MED_VOIDED": {
      const p = (event.payload || {}) as MedPayload & { reason?: string };
      Icon = Pill;
      actionText = locale === "ar" ? `ألغى تسجيل جرعة: ${p.reason || ""}` : `cancelled a dose entry: ${p.reason || ""}`;
      const name = locale === "ar" ? p.name_ar || p.name_en : p.name_en || p.name_ar;
      const slot = p.slot ? (t.slots as Record<string, string>)[p.slot] || p.slot : "";
      if (name) detail = <><bdi>{name}</bdi>{slot && ` · ${slot}`}</>;
      break;
    }
    case "VITAL_LOGGED": {
      const p = (event.payload || {}) as VitalPayload;
      Icon = crisis ? AlertTriangle : Activity;
      actionText = !crisis
        ? t.toastVitalsRecorded
        : p.spo2_status === "CRITICAL"
          ? locale === "ar" ? "سجّل أكسجين منخفضاً جداً — طوارئ" : "logged VERY LOW oxygen"
          : locale === "ar" ? "سجّل أزمة ضغط دم — طوارئ" : "logged a blood pressure CRISIS";
      if (p.systolic && p.diastolic) detail = <Ltr>{p.systolic}/{p.diastolic} mmHg</Ltr>;
      else if (p.spo2 != null) detail = <Ltr>SpO₂ {p.spo2}%</Ltr>;
      else if (p.temperature_c != null) detail = <Ltr>{p.temperature_c.toFixed(1)} °C</Ltr>;
      break;
    }
    case "INSULIN_LOGGED": {
      const p = (event.payload || {}) as InsulinPayload;
      Icon = Syringe;
      const name = locale === "ar" ? p.name_ar || p.name_en : p.name_en || p.name_ar;
      actionText =
        p.status === "given"
          ? locale === "ar" ? "أعطى إبرة إنسولين" : "gave an insulin injection"
          : locale === "ar" ? "سجّل السكر بدون إبرة" : "checked sugar, no injection";
      detail = (
        <>
          <bdi>{name}</bdi>
          {p.status === "given" && <> · <Ltr>{p.units}</Ltr>{locale === "ar" ? " وحدة" : " units"}</>}
          {p.blood_sugar != null && <> · <Ltr>{p.blood_sugar} mmol/L</Ltr></>}
        </>
      );
      break;
    }
    case "NOTE_ADDED": {
      const p = (event.payload || {}) as NotePayload;
      Icon = ClipboardList;
      actionText = t.toastHandoverAdded;
      const text = [(p.tags || []).map((k) => tagLabel(k, locale === "ar")).join(" · "), p.summary || ""].filter(Boolean).join(" — ");
      if (p.passed_to_name) actionText = locale === "ar" ? `سلّم المناوبة إلى ${p.passed_to_name}` : `passed duty to ${p.passed_to_name}`;
      if (text) detail = <bdi>{text}</bdi>;
      break;
    }
    case "CAREGIVER_SWITCHED":
      Icon = UserCheck;
      actionText = locale === "ar" ? "بدّل حسابه" : "switched profile";
      break;
    default:
      return null;
  }

  return (
    // inset-x-0 + mx-auto centers in both directions (start-1/2 + -translate-x-1/2 pushed it off-screen in RTL)
    <div
      role={crisis ? "alertdialog" : "status"}
      aria-live={crisis ? "assertive" : "polite"}
      className="fixed top-16 inset-x-0 mx-auto z-50 w-11/12 max-w-sm animate-in slide-in-from-top-4 duration-300"
    >
      <div
        className={`rounded-2xl border p-3 text-white shadow-xl backdrop-blur-md ${
          crisis
            ? "border-rose-300 bg-rose-700"
            : isAmber(event)
              ? "border-amber-300 bg-amber-700"
              : "border-emerald-500/30 bg-slate-900/95"
        }`}
      >
        <div className="flex items-center gap-3">
          <div
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-xs font-bold text-white shadow-xs"
            style={{ backgroundColor: actor.avatar_color || "#059669" }}
          >
            {actor.name?.slice(0, 1)}
          </div>

          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-1.5">
              <span className="text-sm font-bold truncate">{actor.name}</span>
              <Icon className={`h-4 w-4 shrink-0 ${crisis ? "text-white animate-pulse" : isAmber(event) ? "text-white" : "text-emerald-400"}`} />
            </div>
            <p className={`text-xs ${crisis || isAmber(event) ? "font-bold text-white" : "text-slate-300"}`}>{actionText}</p>
            {detail && <p className="mt-0.5 text-sm font-semibold line-clamp-2">{detail}</p>}
          </div>

          <button
            type="button"
            onClick={() => setToast(null)}
            aria-label={locale === "ar" ? "إغلاق التنبيه" : "Dismiss notification"}
            className={`flex min-h-12 min-w-12 items-center justify-center rounded-full ${crisis ? "text-rose-100 hover:text-white" : "text-slate-400 hover:text-white"}`}
          >
            <X className="h-5 w-5" />
          </button>
        </div>

      </div>
    </div>
  );
}
