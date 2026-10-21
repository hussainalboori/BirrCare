"use client";

import React, { useState, useEffect, useCallback } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { CareStatus, RepositionSide, SkinCondition } from "@/lib/types";
import { useLiveSync } from "@/hooks/useLiveSync";
import { haptic } from "@/lib/theme";
import { Clock, AlertTriangle, ShieldAlert, CheckCircle2, RotateCw } from "lucide-react";
import { Ltr } from "./Ltr";

const SIDES: { id: RepositionSide; ar: string; en: string }[] = [
  { id: "left", ar: "الجانب الأيسر", en: "Left side" },
  { id: "back", ar: "الظهر", en: "Back" },
  { id: "right", ar: "الجانب الأيمن", en: "Right side" },
];

export function RepositionCard() {
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const { currentCaregiver } = useCaregiver();

  const [status, setStatus] = useState<CareStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [loggingSide, setLoggingSide] = useState<RepositionSide | null>(null);
  const [skinCondition, setSkinCondition] = useState<SkinCondition>("ok");

  const loadStatus = useCallback(async () => {
    try {
      const data = await fetchApi<CareStatus>("/care/status?kind=reposition");
      setStatus(data);
    } catch (err) {
      console.warn("Failed fetching reposition status:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadStatus();
    // Periodically update timer every minute
    const interval = setInterval(() => {
      loadStatus();
    }, 60000);
    return () => clearInterval(interval);
  }, [loadStatus]);

  // Live SSE listener: updates instantly when another sibling logs a reposition
  useLiveSync({
    onCareEvent: (e) => {
      if (e.type === "CARE_EVENT_LOGGED" || e.type === "CARE_OVERDUE") {
        loadStatus();
      }
    },
    onResync: loadStatus,
  });

  const handleTurn = async (side: RepositionSide) => {
    if (loggingSide) return;
    haptic(35);
    setLoggingSide(side);

    try {
      await fetchApi("/care/events", {
        method: "POST",
        body: JSON.stringify({
          kind: "reposition",
          value: side,
          skin_condition: skinCondition,
          client_request_id: `rep_${Date.now()}_${Math.random().toString(36).substring(2, 7)}`,
        }),
      });
      // Reset skin condition to default ok after logging
      setSkinCondition("ok");
      await loadStatus();
    } catch (err) {
      console.error("Failed logging reposition event:", err);
    } finally {
      setLoggingSide(null);
    }
  };

  const minutesSince = status?.minutes_since ?? null;
  const intervalMinutes = status?.interval_minutes ?? 120;
  const isOverdueWarning = status?.is_overdue_warning ?? false;
  const isOverdueCritical = status?.is_overdue_critical ?? false;

  // Format time ago string: "1 h 40 m ago" / "قبل ساعة و٤٠ دقيقة"
  const formatTimeAgo = (mins: number | null): string => {
    if (mins === null) return ar ? "لم يُسجل بعد" : "Not recorded yet";
    if (mins < 1) return ar ? "الآن" : "Just now";
    const h = Math.floor(mins / 60);
    const m = mins % 60;
    if (h === 0) return ar ? `قبل ${m} دقيقة` : `${m}m ago`;
    if (m === 0) return ar ? `قبل ${h} ساعة` : `${h}h ago`;
    return ar ? `قبل ${h} س و ${m} د` : `${h}h ${m}m ago`;
  };

  const sideLabel = (sideVal?: string | null) => {
    if (!sideVal) return "";
    const found = SIDES.find((s) => s.id === sideVal);
    if (!found) return sideVal;
    return ar ? found.ar : found.en;
  };

  return (
    <div
      data-testid="reposition-card"
      className={`mb-4 rounded-3xl border p-4 sm:p-5 transition-all shadow-xs ${
        isOverdueCritical
          ? "border-rose-400 bg-rose-50/70 dark:border-rose-900/80 dark:bg-rose-950/30"
          : isOverdueWarning
          ? "border-amber-300 bg-amber-50/60 dark:border-amber-900/60 dark:bg-amber-950/20"
          : "border-slate-200/90 bg-white dark:border-slate-800 dark:bg-slate-900"
      }`}
    >
      {/* Header & Status Indicator */}
      <div className="flex items-start justify-between gap-3 mb-3">
        <div className="flex items-center gap-2">
          <div
            className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl ${
              isOverdueCritical
                ? "bg-rose-600 text-white animate-pulse"
                : isOverdueWarning
                ? "bg-amber-500 text-white"
                : "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300"
            }`}
          >
            <RotateCw className="h-5 w-5" />
          </div>
          <div>
            <h2 className="text-base font-black text-slate-900 dark:text-white flex items-center gap-1.5">
              <span>{ar ? "مؤقت تقليب المريض (قرح الفراش)" : "Repositioning Timer (Pressure Sores)"}</span>
            </h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
              {ar ? `الفاصل الموصى به: كل ${intervalMinutes / 60} ساعات` : `Recommended interval: every ${intervalMinutes / 60}h`}
            </p>
          </div>
        </div>

        {/* Overdue / Due Pill */}
        {isOverdueCritical ? (
          <span className="flex items-center gap-1 rounded-full bg-rose-600 px-2.5 py-1 text-xs font-black text-white shadow-xs">
            <AlertTriangle className="h-3.5 w-3.5" />
            <span>{ar ? "تأخر حرج!" : "Overdue!"}</span>
          </span>
        ) : isOverdueWarning ? (
          <span className="flex items-center gap-1 rounded-full bg-amber-500 px-2.5 py-1 text-xs font-bold text-white shadow-xs">
            <Clock className="h-3.5 w-3.5" />
            <span>{ar ? "تأخر التقليب" : "Due soon"}</span>
          </span>
        ) : (
          <span className="flex items-center gap-1 rounded-full bg-slate-100 dark:bg-slate-800 px-2.5 py-1 text-xs font-semibold text-slate-600 dark:text-slate-300">
            <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500" />
            <span>{ar ? "منتظم" : "On schedule"}</span>
          </span>
        )}
      </div>

      {/* Narrative Status Line */}
      <div
        data-testid="reposition-status"
        className="mb-4 rounded-2xl bg-slate-50 dark:bg-slate-800/60 p-3 border border-slate-100 dark:border-slate-800/80 text-xs sm:text-sm text-slate-800 dark:text-slate-200"
      >
        {loading ? (
          <span className="text-slate-400">{ar ? "جاري التحديث..." : "Updating..."}</span>
        ) : status?.last_at ? (
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-1">
            <span>
              {ar ? "آخر تقليب " : "Last turned "}
              <strong className="text-slate-900 dark:text-white">
                <Ltr>{formatTimeAgo(minutesSince)}</Ltr>
              </strong>
              {ar ? " إلى " : " to "}
              <strong className="text-blue-600 dark:text-blue-400">
                {sideLabel(status.last_value)}
              </strong>
              {status.last_caregiver_name && (
                <>
                  {ar ? " بواسطة " : " by "}
                  <strong style={{ color: status.last_avatar_color || "#2563eb" }}>
                    {status.last_caregiver_name}
                  </strong>
                </>
              )}
            </span>
            {status.last_skin_condition === "red_area" && (
              <span className="inline-flex items-center gap-1 rounded-md bg-rose-100 px-2 py-0.5 text-xs font-bold text-rose-800 dark:bg-rose-950/80 dark:text-rose-300">
                <ShieldAlert className="h-3 w-3" />
                {ar ? "لوحظ احمرار" : "Red area noted"}
              </span>
            )}
          </div>
        ) : (
          <span className="text-slate-500">
            {ar ? "لم يتم تسجيل أي تقليب بعد اليوم. اضغط أحد الأزرار أدناه لتسجيل الجهة." : "No repositioning logged yet today. Tap a side below to record."}
          </span>
        )}
      </div>

      {/* Optional Skin-Check Chip */}
      <div className="mb-3 flex items-center justify-between gap-2 flex-wrap">
        <span className="text-xs font-bold text-slate-600 dark:text-slate-400">
          {ar ? "فحص الجلد أثناء التقليب:" : "Skin check on turn:"}
        </span>
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            data-testid="skin-ok"
            onClick={() => setSkinCondition("ok")}
            className={`rounded-xl px-3 py-1.5 text-xs font-bold transition-all ${
              skinCondition === "ok"
                ? "bg-emerald-600 text-white shadow-2xs"
                : "bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-300"
            }`}
          >
            {ar ? "بشرة سليمة ✓" : "Skin OK ✓"}
          </button>
          <button
            type="button"
            data-testid="skin-red-area"
            onClick={() => setSkinCondition("red_area")}
            className={`rounded-xl px-3 py-1.5 text-xs font-bold transition-all ${
              skinCondition === "red_area"
                ? "bg-rose-600 text-white shadow-2xs"
                : "bg-rose-50 text-rose-700 hover:bg-rose-100 dark:bg-rose-950/50 dark:text-rose-300 border border-rose-200 dark:border-rose-900/60"
            }`}
          >
            {ar ? "احمرار / منطقة حمراء ⚠️" : "Red area ⚠️"}
          </button>
        </div>
      </div>

      {skinCondition === "red_area" && (
        <div className="mb-3 rounded-xl bg-rose-50 dark:bg-rose-950/40 p-2.5 text-xs font-semibold text-rose-800 dark:text-rose-200 border border-rose-200 dark:border-rose-800 flex items-center gap-1.5">
          <ShieldAlert className="h-4 w-4 shrink-0 text-rose-600" />
          <span>
            {ar
              ? "سيتم إنشاء ملاحظة عاجلة في تسليم المناوبة تلقائياً لتنبيه المناوب القادم بفحص الموضع."
              : "An urgent handover flag will be automatically raised for the next caregiver."}
          </span>
        </div>
      )}

      {/* 3 Huge Buttons: Left · Back · Right */}
      <div className="grid grid-cols-3 gap-2 sm:gap-3">
        {SIDES.map((side) => {
          const isCurrentSide = status?.last_value === side.id;
          const isPending = loggingSide === side.id;

          return (
            <button
              key={side.id}
              type="button"
              data-testid={`reposition-${side.id}`}
              disabled={loggingSide !== null}
              onClick={() => handleTurn(side.id)}
              className={`flex min-h-16 flex-col items-center justify-center rounded-2xl border-2 p-2 font-black transition-all active:scale-95 text-center ${
                isCurrentSide
                  ? "border-blue-600 bg-blue-50 text-blue-900 ring-2 ring-blue-500/20 dark:border-blue-500 dark:bg-blue-950/50 dark:text-blue-100"
                  : "border-slate-200 bg-white text-slate-800 hover:border-slate-300 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-800 dark:text-white dark:hover:bg-slate-750"
              } ${isPending ? "opacity-60" : ""}`}
            >
              <span className="text-base sm:text-lg">{ar ? side.ar : side.en}</span>
              {isCurrentSide && (
                <span className="mt-0.5 text-[10px] font-bold text-blue-600 dark:text-blue-400">
                  {ar ? "(الجهة الحالية)" : "(Current)"}
                </span>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
