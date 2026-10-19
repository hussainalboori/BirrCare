"use client";

import React, { useState, useEffect, useCallback } from "react";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { BloodPressureBadge } from "@/components/BloodPressureBadge";
import { useLanguage } from "@/context/LanguageContext";
import { DoctorSummary } from "@/lib/types";
import { fetchApi } from "@/lib/api";
import { ALERTNESS_METAS } from "@/lib/clinical";
import { FileSpreadsheet, Printer, Activity, Heart, ShieldCheck, AlertTriangle } from "lucide-react";

export default function DoctorExportPage() {
  const { t, locale } = useLanguage();
  const [days, setDays] = useState<number>(7);
  const [summary, setSummary] = useState<DoctorSummary | null>(null);
  const [loading, setLoading] = useState(true);

  const loadSummary = useCallback(async (d: number) => {
    try {
      setLoading(true);
      const data = await fetchApi<DoctorSummary>(`/export/summary?days=${d}`);
      setSummary(data);
    } catch (err) {
      console.warn("Failed fetching summary:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadSummary(days);
  }, [days, loadSummary]);

  const handlePrint = () => {
    if (typeof window !== "undefined") {
      window.print();
    }
  };

  return (
    <div className="min-h-screen">
      <div className="no-print">
        <CaregiverHeader />
      </div>

      <main className="mx-auto max-w-4xl px-4 py-6 sm:px-6">
        {/* Actions Bar */}
        <div className="no-print mb-6 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-xl sm:text-2xl font-black text-slate-900 dark:text-white">
                {t.doctorExportTitle}
              </h1>
              <FileSpreadsheet className="h-5 w-5 text-emerald-600" />
            </div>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-1">
              {t.doctorExportSubtitle}
            </p>
          </div>

          <div className="flex items-center gap-2">
            <select
              value={days}
              onChange={(e) => setDays(Number(e.target.value))}
              className="rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs font-bold text-slate-800 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-200"
            >
              <option value={7}>{locale === "ar" ? "آخر 7 أيام" : "Last 7 Days"}</option>
              <option value={14}>{locale === "ar" ? "آخر 14 يوماً" : "Last 14 Days"}</option>
              <option value={30}>{locale === "ar" ? "آخر 30 يوماً" : "Last 30 Days"}</option>
            </select>

            <button
              onClick={handlePrint}
              className="flex items-center gap-2 rounded-xl bg-slate-900 hover:bg-slate-800 dark:bg-emerald-600 dark:hover:bg-emerald-700 px-4 py-2 text-xs font-bold text-white shadow-sm transition-all active:scale-95"
            >
              <Printer className="h-4 w-4" />
              <span>{t.printReport}</span>
            </button>
          </div>
        </div>

        {/* Printable Medical Sheet */}
        {loading || !summary ? (
          <div className="h-96 w-full animate-pulse rounded-3xl bg-slate-200 dark:bg-slate-800" />
        ) : (
          <div className="rounded-3xl border border-slate-200 bg-white p-6 sm:p-8 shadow-sm dark:border-slate-800 dark:bg-slate-900 print:border-none print:shadow-none print:p-0">
            {/* Medical Header */}
            <div className="border-b border-slate-200 pb-5 mb-6 dark:border-slate-800">
              <div className="flex items-center justify-between">
                <div>
                  <h2 className="text-xl font-bold text-slate-900 dark:text-white">
                    {locale === "ar" ? "تقرير المتابعة العائلية لمريض الجلطة الدماغية" : "Post-Stroke Family Care Recovery Report"}
                  </h2>
                  <p className="text-xs text-slate-500 mt-1">
                    {t.coveredPeriod}: {summary.start_date} → {summary.end_date} ({summary.days_covered} {locale === "ar" ? "أيام" : "days"})
                  </p>
                </div>
                <div className="text-end">
                  <span className="inline-block px-3 py-1 rounded-full bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300 text-xs font-bold">
                    Birrcare (رعاية بر)
                  </span>
                  <p className="text-[10px] text-slate-400 mt-1">
                    {new Date(summary.generated_at).toLocaleString()}
                  </p>
                </div>
              </div>
            </div>

            {/* Key Clinical Summary Metrics */}
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-6">
              {/* BP Average */}
              <div className="rounded-2xl border border-slate-100 bg-slate-50 p-4 dark:border-slate-800 dark:bg-slate-800/50">
                <span className="text-[11px] font-semibold text-slate-500 block mb-1">
                  {locale === "ar" ? "متوسط ضغط الدم" : "Avg Blood Pressure"}
                </span>
                <span className="text-lg font-black text-slate-900 dark:text-white">
                  {Math.round(summary.vitals_stats.avg_systolic)} / {Math.round(summary.vitals_stats.avg_diastolic)}
                </span>
                <span className="text-[10px] text-slate-400 block mt-1">
                  {locale === "ar" ? `المدى: ${summary.vitals_stats.min_systolic}-${summary.vitals_stats.max_systolic}` : `Range: ${summary.vitals_stats.min_systolic}-${summary.vitals_stats.max_systolic}`}
                </span>
              </div>

              {/* Medication Adherence */}
              <div className="rounded-2xl border border-slate-100 bg-slate-50 p-4 dark:border-slate-800 dark:bg-slate-800/50">
                <span className="text-[11px] font-semibold text-slate-500 block mb-1 flex items-center gap-1">
                  <ShieldCheck className="h-3.5 w-3.5 text-emerald-500" />
                  {t.adherenceRate}
                </span>
                <span className="text-lg font-black text-emerald-600 dark:text-emerald-400">
                  {Math.round(summary.adherence.adherence_rate)}%
                </span>
                <span className="text-[10px] text-slate-400 block mt-1">
                  {summary.adherence.total_taken} {locale === "ar" ? "جرعة أُعطيت" : "doses taken"}
                </span>
              </div>

              {/* Average Pulse */}
              <div className="rounded-2xl border border-slate-100 bg-slate-50 p-4 dark:border-slate-800 dark:bg-slate-800/50">
                <span className="text-[11px] font-semibold text-slate-500 block mb-1 flex items-center gap-1">
                  <Heart className="h-3.5 w-3.5 text-rose-500" />
                  {locale === "ar" ? "متوسط النبض" : "Average Pulse"}
                </span>
                <span className="text-lg font-black text-slate-900 dark:text-white">
                  {Math.round(summary.vitals_stats.avg_pulse)} <span className="text-xs font-normal">bpm</span>
                </span>
                <span className="text-[10px] text-slate-400 block mt-1">
                  {summary.vitals_stats.total_readings} {locale === "ar" ? "قراءة" : "readings"}
                </span>
              </div>

              {/* Crisis Episodes */}
              <div className="rounded-2xl border border-slate-100 bg-slate-50 p-4 dark:border-slate-800 dark:bg-slate-800/50">
                <span className="text-[11px] font-semibold text-slate-500 block mb-1 flex items-center gap-1">
                  <AlertTriangle className="h-3.5 w-3.5 text-amber-500" />
                  {locale === "ar" ? "أزمات الضغط" : "Crisis Readings"}
                </span>
                <span className={`text-lg font-black ${summary.vitals_stats.crisis_count > 0 ? "text-rose-600" : "text-slate-900 dark:text-white"}`}>
                  {summary.vitals_stats.crisis_count}
                </span>
                <span className="text-[10px] text-slate-400 block mt-1">
                  {summary.vitals_stats.elevated_count} {locale === "ar" ? "مرتفعة" : "elevated"}
                </span>
              </div>
            </div>

            {/* Cognitive State Taxonomy Distribution */}
            <div className="mb-6 rounded-2xl border border-slate-200 p-4 dark:border-slate-800">
              <h3 className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300 mb-3">
                {t.cognitiveDistribution}
              </h3>
              <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
                {Object.entries(summary.alertness_distribution).map(([level, count]) => {
                  const meta = ALERTNESS_METAS[level as keyof typeof ALERTNESS_METAS];
                  return (
                    <div key={level} className="p-3 rounded-xl bg-slate-50 dark:bg-slate-800/40 border border-slate-100 dark:border-slate-800">
                      <span className="text-xs font-semibold text-slate-800 dark:text-slate-200 block">
                        {locale === "ar" ? meta?.labelAr : meta?.labelEn}
                      </span>
                      <span className="text-base font-bold text-slate-900 dark:text-white mt-1 block">
                        {count} {locale === "ar" ? "مرات" : "times"}
                      </span>
                    </div>
                  );
                })}
              </div>
            </div>

            {/* Recent Vitals Table */}
            <div className="mb-6">
              <h3 className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300 mb-3">
                {locale === "ar" ? "آخر القياسات الحيوية التفصيلية" : "Detailed Vitals Records"}
              </h3>
              <div className="overflow-x-auto">
                <table className="w-full text-start text-xs">
                  <thead>
                    <tr className="border-b border-slate-200 dark:border-slate-800 text-slate-400">
                      <th className="py-2 text-start font-semibold">{locale === "ar" ? "التاريخ والوقت" : "Date & Time"}</th>
                      <th className="py-2 text-start font-semibold">{locale === "ar" ? "ضغط الدم" : "Blood Pressure"}</th>
                      <th className="py-2 text-start font-semibold">{locale === "ar" ? "النبض" : "Pulse"}</th>
                      <th className="py-2 text-start font-semibold">{locale === "ar" ? "الحالة الإدراكية" : "Alertness"}</th>
                      <th className="py-2 text-start font-semibold">{locale === "ar" ? "المُسجل" : "Caregiver"}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                    {summary.recent_vitals.slice(0, 10).map((v) => (
                      <tr key={v.id}>
                        <td className="py-2 font-mono text-slate-600 dark:text-slate-400">
                          {new Date(v.recorded_at).toLocaleDateString()} {new Date(v.recorded_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
                        </td>
                        <td className="py-2 font-bold text-slate-900 dark:text-white">
                          {v.systolic}/{v.diastolic} mmHg
                        </td>
                        <td className="py-2 text-slate-700 dark:text-slate-300">{v.pulse} bpm</td>
                        <td className="py-2">
                          <span className="px-2 py-0.5 rounded-sm bg-slate-100 dark:bg-slate-800 text-[10px] font-semibold">
                            {locale === "ar" ? ALERTNESS_METAS[v.alertness_level]?.labelAr : ALERTNESS_METAS[v.alertness_level]?.labelEn}
                          </span>
                        </td>
                        <td className="py-2 text-slate-600 dark:text-slate-400">{v.caregiver_name}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>

            {/* Doctor Examination Notes Area */}
            <div className="mt-8 border-t-2 border-dashed border-slate-300 pt-6">
              <h4 className="text-xs font-bold text-slate-700 uppercase tracking-wider mb-2">
                {t.doctorNotesPlaceholder}
              </h4>
              <div className="h-28 rounded-2xl border border-slate-200 p-4 bg-slate-50/50 print:border-slate-400" />
              <div className="mt-4 flex justify-between text-xs text-slate-400">
                <span>{locale === "ar" ? "توقيع الطبيب المعالج: ___________________" : "Attending Physician Signature: ___________________"}</span>
                <span>{locale === "ar" ? "التاريخ: ____ / ____ / ________" : "Date: ____ / ____ / ________"}</span>
              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}
