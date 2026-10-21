"use client";

import React, { useState, useEffect, useCallback } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { fetchApi } from "@/lib/api";
import { CareStatus, BowelConsistency } from "@/lib/types";
import { useLiveSync } from "@/hooks/useLiveSync";
import { haptic } from "@/lib/theme";
import { Droplets, AlertTriangle, CheckCircle2, Waves, Sparkles } from "lucide-react";
import { Ltr } from "./Ltr";

const CUP_SIZES = [50, 100, 150, 200] as const;

const BOWEL_OPTIONS: { id: BowelConsistency; ar: string; en: string; descAr: string; descEn: string }[] = [
  { id: "hard", ar: "قاسي", en: "Hard", descAr: "صعب / جاف", descEn: "Dry / Constipated" },
  { id: "normal", ar: "طبيعي", en: "Normal", descAr: "طبيعي / متماسك", descEn: "Formed / Soft" },
  { id: "loose", ar: "لين", en: "Loose", descAr: "سائل / إسهال", descEn: "Liquid / Diarrhea" },
];

export function FluidBowelCard() {
  const { locale } = useLanguage();
  const ar = locale === "ar";

  const [fluidStatus, setFluidStatus] = useState<CareStatus | null>(null);
  const [bowelStatus, setBowelStatus] = useState<CareStatus | null>(null);
  const [loading, setLoading] = useState(true);

  // Thickened fluid toggle state
  const [isThickened, setIsThickened] = useState(false);
  const [loggingMl, setLoggingMl] = useState<number | null>(null);
  const [loggingBowel, setLoggingBowel] = useState<BowelConsistency | null>(null);

  const loadStatus = useCallback(async () => {
    try {
      const [fData, bData] = await Promise.all([
        fetchApi<CareStatus>("/care/status?kind=fluid"),
        fetchApi<CareStatus>("/care/status?kind=bowel"),
      ]);
      setFluidStatus(fData);
      setBowelStatus(bData);
    } catch (err) {
      console.warn("Failed fetching fluid/bowel care status:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadStatus();
    // Re-check periodically every 60 seconds
    const interval = setInterval(loadStatus, 60000);
    return () => clearInterval(interval);
  }, [loadStatus]);

  // Live SSE listener: instantly refresh on care events
  useLiveSync({
    onCareEvent: (e) => {
      if (e.type === "CARE_EVENT_LOGGED" || e.type === "CARE_OVERDUE") {
        loadStatus();
      }
    },
    onResync: loadStatus,
  });

  const handleLogFluid = async (ml: number) => {
    if (loggingMl !== null) return;
    haptic(35);
    setLoggingMl(ml);

    try {
      await fetchApi("/care/events", {
        method: "POST",
        body: JSON.stringify({
          kind: "fluid",
          value: String(ml),
          thickened: isThickened,
          client_request_id: `fl_${Date.now()}_${Math.random().toString(36).substring(2, 7)}`,
        }),
      });
      await loadStatus();
    } catch (err) {
      console.error("Failed logging fluid intake:", err);
    } finally {
      setLoggingMl(null);
    }
  };

  const handleLogBowel = async (consistency: BowelConsistency) => {
    if (loggingBowel !== null) return;
    haptic(35);
    setLoggingBowel(consistency);

    try {
      await fetchApi("/care/events", {
        method: "POST",
        body: JSON.stringify({
          kind: "bowel",
          value: consistency,
          client_request_id: `bw_${Date.now()}_${Math.random().toString(36).substring(2, 7)}`,
        }),
      });
      await loadStatus();
    } catch (err) {
      console.error("Failed logging bowel movement:", err);
    } finally {
      setLoggingBowel(null);
    }
  };

  const formatTimeAgo = (mins: number | null | undefined): string => {
    if (mins == null) return ar ? "لم يُسجل بعد" : "Not recorded yet";
    if (mins < 1) return ar ? "الآن" : "Just now";
    const h = Math.floor(mins / 60);
    const m = mins % 60;
    if (h === 0) return ar ? `قبل ${m} دقيقة` : `${m}m ago`;
    const days = Math.floor(h / 24);
    if (days >= 1) {
      return ar ? `قبل ${days} ${days === 1 ? "يوم" : days === 2 ? "يومين" : "أيام"}` : `${days}d ago`;
    }
    if (m === 0) return ar ? `قبل ${h} ساعة` : `${h}h ago`;
    return ar ? `قبل ${h} س و ${m} د` : `${h}h ${m}m ago`;
  };

  const bowelLabel = (val?: string | null) => {
    if (!val) return "";
    const found = BOWEL_OPTIONS.find((b) => b.id === val);
    if (!found) return val;
    return ar ? found.ar : found.en;
  };

  // Fluid calculations
  const todayTotal = fluidStatus?.today_total_ml ?? 0;
  const todayThickened = fluidStatus?.today_thickened_ml ?? 0;
  const targetMl = fluidStatus?.target_ml ?? 1500;
  const progressPercent = fluidStatus?.progress_percent ?? Math.round((todayTotal / targetMl) * 100);
  const isLowFluid = fluidStatus?.is_low_fluid ?? false;

  // SVG Progress Ring calculations
  const radius = 34;
  const strokeWidth = 7;
  const circumference = 2 * Math.PI * radius;
  const clampedProgress = Math.min(100, Math.max(0, progressPercent));
  const strokeDashoffset = circumference - (clampedProgress / 100) * circumference;

  // Bowel calculations
  const daysSinceBowel = bowelStatus?.days_since_bowel ?? null;
  const isBowelOverdue = bowelStatus?.is_bowel_overdue ?? (daysSinceBowel != null && daysSinceBowel >= 3);

  const cardBorderClass = isBowelOverdue || isLowFluid
    ? "border-amber-300 bg-amber-50/40 dark:border-amber-900/60 dark:bg-amber-950/20"
    : "border-slate-200/90 bg-white dark:border-slate-800 dark:bg-slate-900";

  return (
    <div
      data-testid="fluid-bowel-card"
      className={`mb-4 rounded-3xl border p-4 sm:p-5 transition-all shadow-xs ${cardBorderClass}`}
    >
      {/* ============================================================== */}
      {/* 1. FLUID INTAKE SECTION                                         */}
      {/* ============================================================== */}
      <div className="mb-6">
        {/* Section Header */}
        <div className="flex items-start justify-between gap-3 mb-3">
          <div className="flex items-center gap-2">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-300">
              <Droplets className="h-5 w-5" />
            </div>
            <div>
              <h2 className="text-base font-black text-slate-900 dark:text-white flex items-center gap-1.5">
                <span>{ar ? "متابعة السوائل والماء" : "Fluid & Hydration Tracker"}</span>
              </h2>
              <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
                {ar ? `الهدف اليومي: ${targetMl} مل` : `Daily target: ${targetMl} ml`}
              </p>
            </div>
          </div>

          {/* Low Fluid Alert Pill or Goal Completed Pill */}
          {isLowFluid ? (
            <span
              data-testid="low-fluid-alert"
              className="flex items-center gap-1 rounded-full bg-amber-500 px-2.5 py-1 text-xs font-bold text-white shadow-xs animate-pulse"
            >
              <AlertTriangle className="h-3.5 w-3.5" />
              <span>{ar ? "سوائل منخفضة!" : "Low Fluid!"}</span>
            </span>
          ) : progressPercent >= 100 ? (
            <span className="flex items-center gap-1 rounded-full bg-emerald-600 px-2.5 py-1 text-xs font-bold text-white shadow-xs">
              <CheckCircle2 className="h-3.5 w-3.5" />
              <span>{ar ? "اكتمل الهدف" : "Goal met"}</span>
            </span>
          ) : (
            <span className="flex items-center gap-1 rounded-full bg-slate-100 dark:bg-slate-800 px-2.5 py-1 text-xs font-semibold text-slate-600 dark:text-slate-300">
              <Sparkles className="h-3.5 w-3.5 text-sky-500" />
              <span>{progressPercent}%</span>
            </span>
          )}
        </div>

        {/* Low Fluid Warning Banner */}
        {isLowFluid && (
          <div className="mb-3 rounded-2xl bg-amber-100/90 dark:bg-amber-950/70 p-3 text-xs sm:text-sm font-semibold text-amber-900 dark:text-amber-200 border border-amber-300/80 dark:border-amber-900">
            <div className="flex items-start gap-2">
              <AlertTriangle className="h-5 w-5 shrink-0 text-amber-600 dark:text-amber-400 mt-0.5" />
              <div>
                <p className="font-bold">
                  {ar ? "تنبيه: استهلاك السوائل أقل من 50% بحلول المساء" : "Alert: Fluid intake below 50% by 18:00"}
                </p>
                <p className="text-xs mt-0.5 opacity-90">
                  {ar
                    ? `سُجل حتى الآن ${todayTotal} مل من أصل ${targetMl} مل. يرجى تقديم السوائل للوالدة لتجنب الجفاف والجلطات.`
                    : `Recorded ${todayTotal} ml of ${targetMl} ml so far. Please offer fluids to prevent dehydration.`}
                </p>
              </div>
            </div>
          </div>
        )}

        {/* Progress Meter & Totals Display */}
        <div className="mb-4 flex items-center gap-4 rounded-2xl bg-slate-50 dark:bg-slate-800/60 p-3.5 border border-slate-100 dark:border-slate-800/80">
          {/* Circular SVG Ring */}
          <div data-testid="fluid-progress-ring" className="relative flex h-20 w-20 shrink-0 items-center justify-center">
            <svg className="h-20 w-20 -rotate-90 transform" viewBox="0 0 80 80">
              <circle
                cx="40"
                cy="40"
                r={radius}
                stroke="currentColor"
                strokeWidth={strokeWidth}
                fill="transparent"
                className="text-slate-200 dark:text-slate-700"
              />
              <circle
                cx="40"
                cy="40"
                r={radius}
                stroke={isLowFluid ? "#f59e0b" : "#0284c7"}
                strokeWidth={strokeWidth}
                strokeDasharray={circumference}
                strokeDashoffset={strokeDashoffset}
                strokeLinecap="round"
                fill="transparent"
                className="transition-all duration-500 ease-out"
              />
            </svg>
            <div className="absolute inset-0 flex flex-col items-center justify-center text-center">
              <span className="text-sm font-black text-slate-900 dark:text-white">
                <Ltr>{progressPercent}%</Ltr>
              </span>
            </div>
          </div>

          {/* Details & Target Text */}
          <div className="flex-1">
            <div className="text-base font-black text-slate-900 dark:text-white">
              <Ltr>{todayTotal} / {targetMl} ml</Ltr>
            </div>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
              {todayThickened > 0 ? (
                ar ? (
                  <>منها <strong className="text-amber-700 dark:text-amber-300">{todayThickened} مل</strong> سائل مكثف</>
                ) : (
                  <>Including <strong className="text-amber-700 dark:text-amber-300">{todayThickened} ml</strong> thickened</>
                )
              ) : (
                ar ? "لم يُسجل سائل مكثف اليوم" : "No thickened fluids today"
              )}
            </p>
            {/* Last Fluid Narrative */}
            <div data-testid="fluid-status-narrative" className="text-xs text-slate-600 dark:text-slate-300 mt-1">
              {loading ? (
                <span className="text-slate-400">{ar ? "جاري التحديث..." : "Updating..."}</span>
              ) : fluidStatus?.last_at ? (
                <span>
                  {ar ? "آخر جرعة " : "Last intake "}
                  <strong className="text-slate-900 dark:text-white">
                    <Ltr>{formatTimeAgo(fluidStatus.minutes_since)}</Ltr>
                  </strong>
                  {" ("}
                  <Ltr>{fluidStatus.last_value} ml</Ltr>
                  {fluidStatus.today_thickened_ml ? (ar ? " مكثف" : " thickened") : ""}
                  {")"}
                  {fluidStatus.last_caregiver_name && (
                    <>
                      {ar ? " بواسطة " : " by "}
                      <strong style={{ color: fluidStatus.last_avatar_color || "#2563eb" }}>
                        {fluidStatus.last_caregiver_name}
                      </strong>
                    </>
                  )}
                </span>
              ) : (
                <span className="text-slate-400">{ar ? "لم تُسجل أي سوائل اليوم" : "No fluids logged yet today"}</span>
              )}
            </div>
          </div>
        </div>

        {/* Thickened Fluid Toggle Chip */}
        <div className="mb-3 flex items-center justify-between">
          <span className="text-xs font-bold text-slate-700 dark:text-slate-300">
            {ar ? "تسجيل جرعة ماء أو سائل:" : "Log cup of fluid:"}
          </span>
          <button
            type="button"
            data-testid="thickened-toggle"
            onClick={() => {
              haptic(20);
              setIsThickened((p) => !p);
            }}
            className={`flex min-h-11 items-center gap-1.5 rounded-full px-3 py-1 text-xs font-bold transition-all border ${
              isThickened
                ? "bg-amber-600 text-white border-amber-600 shadow-xs"
                : "bg-slate-100 text-slate-700 border-slate-200 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:border-slate-700"
            }`}
          >
            <Waves className="h-3.5 w-3.5" />
            <span>{ar ? "سائل مكثف (عسر بلع)" : "Thickened fluid"}</span>
            {isThickened && <CheckCircle2 className="h-3.5 w-3.5 ms-0.5" />}
          </button>
        </div>

        {/* 4 Cup Size Buttons (50 · 100 · 150 · 200 ml) */}
        <div className="grid grid-cols-4 gap-2">
          {CUP_SIZES.map((size) => (
            <button
              key={size}
              type="button"
              data-testid={`fluid-button-${size}`}
              disabled={loggingMl !== null}
              onClick={() => handleLogFluid(size)}
              className="flex min-h-12 flex-col items-center justify-center rounded-2xl border border-sky-200 bg-sky-50/70 p-2 text-center transition-all hover:bg-sky-100 active:scale-95 disabled:opacity-50 dark:border-sky-900/60 dark:bg-sky-950/40 dark:hover:bg-sky-900/60"
            >
              <span className="text-base font-black text-sky-900 dark:text-sky-100">
                <Ltr>+{size}</Ltr>
              </span>
              <span className="text-[10px] font-semibold text-sky-700 dark:text-sky-300">
                {ar ? "مل" : "ml"}
              </span>
            </button>
          ))}
        </div>
      </div>

      {/* Divider */}
      <hr className="my-5 border-slate-200 dark:border-slate-800" />

      {/* ============================================================== */}
      {/* 2. BOWEL MOVEMENT SECTION                                       */}
      {/* ============================================================== */}
      <div>
        {/* Section Header */}
        <div className="flex items-start justify-between gap-3 mb-3">
          <div>
            <h2 className="text-base font-black text-slate-900 dark:text-white flex items-center gap-1.5">
              <span>{ar ? "حركة الأمعاء والإخراج" : "Bowel Movement Tracker"}</span>
            </h2>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
              {ar ? "مراقبة الإمساك وصحة الجهاز الهضمي" : "Monitoring constipation & digestive health"}
            </p>
          </div>

          {/* Overdue Warning Pill */}
          {isBowelOverdue ? (
            <span
              data-testid="bowel-overdue-alert"
              className="flex items-center gap-1 rounded-full bg-amber-500 px-2.5 py-1 text-xs font-bold text-white shadow-xs animate-pulse"
            >
              <AlertTriangle className="h-3.5 w-3.5" />
              <span>{ar ? "انقطاع 3 أيام!" : "3 days overdue!"}</span>
            </span>
          ) : daysSinceBowel !== null ? (
            <span className="flex items-center gap-1 rounded-full bg-slate-100 dark:bg-slate-800 px-2.5 py-1 text-xs font-semibold text-slate-600 dark:text-slate-300">
              <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500" />
              <span>{ar ? `منذ ${daysSinceBowel} يوم` : `${daysSinceBowel}d ago`}</span>
            </span>
          ) : null}
        </div>

        {/* 3 Days Overdue Amber Card for all siblings */}
        {isBowelOverdue && (
          <div className="mb-3 rounded-2xl bg-amber-100/90 dark:bg-amber-950/70 p-3 text-xs sm:text-sm font-semibold text-amber-900 dark:text-amber-200 border border-amber-300/80 dark:border-amber-900">
            <div className="flex items-start gap-2">
              <AlertTriangle className="h-5 w-5 shrink-0 text-amber-600 dark:text-amber-400 mt-0.5" />
              <div>
                <p className="font-bold">
                  {ar
                    ? `تنبيه: لم يحدث إخراج بطن منذ ${daysSinceBowel ?? 3} أيام`
                    : `Alert: No bowel movement in ${daysSinceBowel ?? 3} days`}
                </p>
                <p className="text-xs mt-0.5 opacity-90">
                  {ar
                    ? "انقطاع الإخراج لـ 3 أيام فأكثر قد يشير لإمساك شديد بعد الجلطة. يرجى إعطاء الملين الموصوف أو استشارة الطبيب."
                    : "No bowel movement in 3+ days may indicate post-stroke constipation. Check with the doctor or offer prescribed laxative."}
                </p>
              </div>
            </div>
          </div>
        )}

        {/* Last Bowel Movement Narrative */}
        <div
          data-testid="bowel-status-narrative"
          className="mb-3 rounded-2xl bg-slate-50 dark:bg-slate-800/60 p-3 border border-slate-100 dark:border-slate-800/80 text-xs sm:text-sm text-slate-800 dark:text-slate-200"
        >
          {loading ? (
            <span className="text-slate-400">{ar ? "جاري التحديث..." : "Updating..."}</span>
          ) : bowelStatus?.last_at ? (
            <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-1">
              <span>
                {ar ? "آخر إخراج " : "Last movement "}
                <strong className="text-slate-900 dark:text-white">
                  <Ltr>{formatTimeAgo(bowelStatus.minutes_since)}</Ltr>
                </strong>
                {ar ? " بقوام " : " consistency: "}
                <strong className="text-emerald-700 dark:text-emerald-400">
                  {bowelLabel(bowelStatus.last_value)}
                </strong>
                {bowelStatus.last_caregiver_name && (
                  <>
                    {ar ? " بواسطة " : " by "}
                    <strong style={{ color: bowelStatus.last_avatar_color || "#2563eb" }}>
                      {bowelStatus.last_caregiver_name}
                    </strong>
                  </>
                )}
              </span>
            </div>
          ) : (
            <span className="text-slate-500">
              {ar ? "لم يُسجل أي إخراج بعد. اضغط أحد الأزرار أدناه للتسجيل بضغطة واحدة." : "No bowel movement logged yet. Tap a button below to log."}
            </span>
          )}
        </div>

        {/* 1-Tap 3-Step Stool Scale Buttons (Hard · Normal · Loose) */}
        <div className="grid grid-cols-3 gap-2">
          {BOWEL_OPTIONS.map((opt) => (
            <button
              key={opt.id}
              type="button"
              data-testid={`bowel-button-${opt.id}`}
              disabled={loggingBowel !== null}
              onClick={() => handleLogBowel(opt.id)}
              className="flex min-h-12 flex-col items-center justify-center rounded-2xl border border-slate-200 bg-white p-2.5 text-center transition-all hover:bg-slate-50 active:scale-95 disabled:opacity-50 dark:border-slate-800 dark:bg-slate-800 dark:hover:bg-slate-700"
            >
              <span className="text-base font-bold text-slate-900 dark:text-white">
                {ar ? opt.ar : opt.en}
              </span>
              <span className="text-[10px] text-slate-500 dark:text-slate-400 mt-0.5">
                {ar ? opt.descAr : opt.descEn}
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
