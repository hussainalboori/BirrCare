"use client";

import React, { useState, useEffect, useCallback } from "react";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { BloodPressureBadge } from "@/components/BloodPressureBadge";
import { useLanguage } from "@/context/LanguageContext";
import { DoctorSummary } from "@/lib/types";
import { fetchApi } from "@/lib/api";
import { ALERTNESS_METAS, classifySpO2, classifyTemperature } from "@/lib/clinical";
import { formatDateTime } from "@/lib/datetime";
import { Auto, Ltr } from "@/components/Ltr";
import { FileSpreadsheet, Printer, Activity, Heart, ShieldCheck, AlertTriangle, Droplets } from "lucide-react";

export default function DoctorExportPage() {
  const { t, locale } = useLanguage();
  const [days, setDays] = useState<number>(7);
  const [summary, setSummary] = useState<DoctorSummary | null>(null);
  const [loading, setLoading] = useState(true);
  // Detailed readings, insulin and notes print on extra pages only when asked.
  const [printDetails, setPrintDetails] = useState(false);

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

  // The "Paused" column (issue #59) only when the period had a hospital stay or time away.
  const showPaused = (summary?.per_medication ?? []).some((m) => (m.paused ?? 0) > 0);

  return (
    <div className="min-h-screen">
      <div className="no-print">
        <CaregiverHeader />
      </div>

      <main className="mx-auto max-w-4xl px-4 py-6 sm:px-6 lg:max-w-none lg:px-8">
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

          <div className="flex flex-wrap items-center gap-2">
            <select
              value={days}
              onChange={(e) => setDays(Number(e.target.value))}
              aria-label={t.coveredPeriod}
              className="min-h-12 rounded-xl border border-slate-200 bg-white px-3 py-2 text-base font-bold text-slate-800 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-200"
            >
              <option value={7}>{locale === "ar" ? "آخر 7 أيام" : "Last 7 Days"}</option>
              <option value={14}>{locale === "ar" ? "آخر 14 يوماً" : "Last 14 Days"}</option>
              <option value={30}>{locale === "ar" ? "آخر 30 يوماً" : "Last 30 Days"}</option>
            </select>

            <label className="flex min-h-12 items-center gap-2 text-sm font-semibold text-slate-700 dark:text-slate-300">
              <input type="checkbox" checked={printDetails} onChange={(e) => setPrintDetails(e.target.checked)} className="h-5 w-5" />
              {locale === "ar" ? "اطبع التفاصيل أيضاً" : "Print details too"}
            </label>

            <button
              type="button"
              onClick={handlePrint}
              className="flex min-h-12 items-center gap-2 rounded-xl bg-slate-900 hover:bg-slate-800 dark:bg-emerald-600 dark:hover:bg-emerald-700 px-4 py-2 text-xs font-bold text-white shadow-sm transition-all active:scale-95"
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
            <div className="border-b border-slate-200 pb-5 mb-6 print:pb-1.5 print:mb-1.5 dark:border-slate-800">
              <div className="flex items-center justify-between">
                <div>
                  <h2 className="text-xl font-bold text-slate-900 dark:text-white">
                    {locale === "ar" ? "تقرير المتابعة العائلية لمريض الجلطة الدماغية" : "Post-Stroke Family Care Recovery Report"}
                  </h2>
                  <p className="text-xs text-slate-500 mt-1">
                    {t.coveredPeriod}: <Ltr>{summary.start_date} → {summary.end_date}</Ltr> ({summary.days_covered} {locale === "ar" ? (summary.days_covered <= 10 ? "أيام" : "يوماً") : "days"})
                  </p>
                </div>
                <div className="text-end">
                  <span className="inline-block px-3 py-1 rounded-full bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300 text-xs font-bold">
                    Birrcare (رعاية بر)
                  </span>
                  <p className="text-xs text-slate-400 mt-1">
                    <Auto>{formatDateTime(summary.generated_at, locale)}</Auto>
                  </p>
                </div>
              </div>
            </div>

            {/* Questions for the doctor (issue #63): first, so the visit starts with them */}
            {(summary.open_questions ?? []).length > 0 && (
              <div data-testid="report-questions" className="mb-4 rounded-xl border border-sky-200 bg-sky-50 p-3 text-sm text-sky-950 break-inside-avoid print:mb-1 print:px-2 print:py-0.5 print:text-[8pt] print:leading-snug dark:border-sky-900 dark:bg-sky-950/40 dark:text-sky-50">
                {/* On paper one compact paragraph, so the summary keeps to one A4 page. */}
                <h3 className="mb-1 text-xs font-bold uppercase tracking-wider print:inline print:text-[8pt]">
                  {locale === "ar" ? "أسئلة العائلة للطبيب" : "The family's questions for the doctor"}
                  <span className="hidden print:inline">: </span>
                </h3>
                <ol className="list-decimal space-y-0.5 ps-5 print:inline print:ps-0">
                  {summary.open_questions!.map((q, i) => (
                    <li key={q.id} className="print:inline">
                      <span className="hidden print:inline">
                        <Ltr>{i + 1})</Ltr>{" "}
                      </span>
                      <bdi>{q.text}</bdi>{" "}
                    </li>
                  ))}
                </ol>
              </div>
            )}

            {/* INR history (issue #62), next to the warfarin doses below */}
            {(summary.lab_results ?? []).some((l) => l.kind === "inr") && (
              <div data-testid="report-inr" className="mb-4 break-inside-avoid">
                <h3 className="mb-2 text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300">
                  {locale === "ar" ? "تحاليل INR" : "INR results"}
                  {summary.inr_target && (
                    <span className="ms-2 normal-case">
                      ({locale === "ar" ? "المدى " : "range "}
                      <Ltr>
                        {summary.inr_target.inr_target_low}–{summary.inr_target.inr_target_high}
                      </Ltr>
                      )
                    </span>
                  )}
                </h3>
                <table className="w-full text-start text-xs print:text-[9pt]">
                  <thead>
                    <tr className="border-b border-slate-200 text-slate-500 dark:border-slate-800">
                      <th className="py-1.5 text-start font-semibold">{locale === "ar" ? "التاريخ" : "Date"}</th>
                      <th className="py-1.5 text-start font-semibold">INR</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                    {summary.lab_results!
                      .filter((l) => l.kind === "inr")
                      .map((l) => (
                        <tr key={l.id} className={l.out_of_range ? "bg-amber-50 dark:bg-amber-950/30" : undefined}>
                          <td className="py-1.5 pe-2"><Ltr>{l.taken_on}</Ltr></td>
                          <td className={`py-1.5 pe-2 ${l.out_of_range ? "font-black text-amber-800 dark:text-amber-300" : ""}`}>
                            <Ltr>{l.value}</Ltr> {l.out_of_range && (locale === "ar" ? "(خارج المدى)" : "(outside range)")}
                          </td>
                        </tr>
                      ))}
                  </tbody>
                </table>
              </div>
            )}

            {/* Falls and seizures (issue #60) */}
            {(summary.incidents ?? []).length > 0 && (
              <div data-testid="report-incidents" className="mb-4 rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-950 print:mb-2 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-50">
                <p className="font-bold">
                  {locale === "ar" ? "السقوط والنوبات" : "Falls and seizures"}:{" "}
                  <Ltr>{summary.incidents!.filter((i) => i.kind === "fall").length}</Ltr> {locale === "ar" ? "سقوط" : "fall(s)"},{" "}
                  <Ltr>{summary.incidents!.filter((i) => i.kind === "seizure").length}</Ltr> {locale === "ar" ? "نوبة" : "seizure(s)"}
                </p>
                <ul className="mt-1 space-y-0.5">
                  {summary.incidents!.map((i) => (
                    <li key={i.id}>
                      <bdi>{formatDateTime(i.at, locale)}</bdi> —{" "}
                      {i.kind === "fall"
                        ? [locale === "ar" ? "سقوط" : "Fall", i.hit_head && (locale === "ar" ? "ضربت رأسها" : "hit head"), i.awake_normal === false && (locale === "ar" ? "ليست واعية" : "not awake/normal"), i.pain_or_bleeding && (locale === "ar" ? "ألم أو نزيف" : "pain or bleeding")].filter(Boolean).join(", ")
                        : (
                          <>
                            {locale === "ar" ? "نوبة تشنج، " : "Seizure, "}
                            <Ltr>{`${Math.floor((i.duration_seconds ?? 0) / 60)}:${String((i.duration_seconds ?? 0) % 60).padStart(2, "0")}`}</Ltr>
                            {i.repeat ? (locale === "ar" ? "، الثانية خلال يوم" : ", second within a day") : ""}
                          </>
                        )}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {/* Hospital stays / time away (issue #59): doses then are "paused", not missed. */}
            {(summary.pauses ?? []).length > 0 && (
              <div data-testid="report-pauses" className="mb-4 rounded-xl border border-slate-300 bg-slate-100 p-3 text-sm text-slate-800 print:mb-2 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100">
                <p className="font-bold">{locale === "ar" ? "فترات في المستشفى أو خارج البيت (لا تُحسب الجرعات فيها فائتة)" : "Time in hospital or away (doses then are not counted as missed)"}</p>
                <ul className="mt-1 space-y-0.5">
                  {summary.pauses!.map((p) => (
                    <li key={p.id}>
                      {p.reason === "hospital" ? (locale === "ar" ? "المستشفى" : "Hospital") : p.reason === "travel" ? (locale === "ar" ? "سفر" : "Travel") : locale === "ar" ? "خارج البيت" : "Away"}:{" "}
                      <bdi>{formatDateTime(p.started_at, locale)}</bdi> → <bdi>{p.ended_at ? formatDateTime(p.ended_at, locale) : locale === "ar" ? "مستمرة" : "ongoing"}</bdi>
                      {p.notes && <> — <bdi>{p.notes}</bdi></>}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {/* Medications against the expected doses (issue #14). Critical ones first; a
                dose nobody recorded is "not recorded" in red, never counted as taken. */}
            {(summary.per_medication ?? []).length > 0 && (
              <div className="mb-5 print:mb-3 break-inside-avoid">
                <h3 className="mb-2 text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300">
                  {locale === "ar" ? "الأدوية: الجرعات المتوقعة مقابل المُعطاة" : "Medications: expected vs given"}
                </h3>
                <div className="overflow-x-auto">
                  <table className="w-full text-start text-xs print:text-[9pt]">
                    <thead>
                      <tr className="border-b border-slate-200 text-slate-500 dark:border-slate-800">
                        <th className="py-1.5 text-start font-semibold">{locale === "ar" ? "الدواء" : "Medication"}</th>
                        <th className="py-1.5 text-start font-semibold">{locale === "ar" ? "المتوقع" : "Expected"}</th>
                        <th className="py-1.5 text-start font-semibold">{locale === "ar" ? "أُعطيت" : "Given"}</th>
                        <th className="py-1.5 text-start font-semibold">{locale === "ar" ? "جزئية / استفراغ" : "Partial / vomited"}</th>
                        <th className="py-1.5 text-start font-semibold">{locale === "ar" ? "رفض / تخطي" : "Refused / skipped"}</th>
                        <th className="py-1.5 text-start font-semibold text-rose-700 dark:text-rose-300">{locale === "ar" ? "لم تُسجّل" : "Not recorded"}</th>
                        {showPaused && <th className="py-1.5 text-start font-semibold text-slate-500">{locale === "ar" ? "متوقفة" : "Paused"}</th>}
                        <th className="py-1.5 text-start font-semibold">{locale === "ar" ? "الالتزام" : "Taken %"}</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                      {[...(summary.per_medication ?? []).filter((m) => !m.stopped_on), ...(summary.per_medication ?? []).filter((m) => m.stopped_on)].map((m, i, all) => (
                        <React.Fragment key={m.medication_id}>
                        {m.stopped_on && !all[i - 1]?.stopped_on && (
                          <tr data-testid="stopped-group">
                            <th colSpan={showPaused ? 8 : 7} className="pt-3 pb-1 text-start text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300">
                              {locale === "ar" ? "أدوية أُوقفت" : "Stopped medicines"}
                            </th>
                          </tr>
                        )}
                        <tr className={m.not_recorded ? "bg-rose-50 dark:bg-rose-950/30" : undefined}>
                          <td className="py-1.5 pe-2 align-top">
                            <span className="font-semibold">
                              {m.is_critical && <span className="me-1 rounded bg-rose-700 px-1.5 py-0.5 text-xs font-bold text-white">{locale === "ar" ? "حرج" : "critical"}</span>}
                              <bdi>{locale === "ar" ? m.name_ar : m.name_en}</bdi> <Auto className="text-slate-500">{m.dosage}</Auto>
                            </span>
                            {(m.past_dosages ?? []).length > 0 && (
                              <span className="block text-xs text-amber-700 dark:text-amber-400">
                                {locale === "ar" ? "جرعة سابقة في الفترة: " : "Earlier dose in period: "}
                                <Auto>{m.past_dosages!.join(", ")}</Auto>
                              </span>
                            )}
                            {m.stopped_on && (
                              <span className="block text-xs font-bold text-slate-700 dark:text-slate-300">
                                {locale === "ar" ? "أُوقف في " : "Stopped on "}
                                <Ltr>{m.stopped_on}</Ltr>
                                {m.stopped_by && (
                                  <>
                                    {locale === "ar" ? " بواسطة " : " by "}
                                    <bdi>{m.stopped_by}</bdi>
                                  </>
                                )}
                              </span>
                            )}
                            {m.missed_dates.length > 0 && (
                              <span className="block text-xs text-rose-700 dark:text-rose-300">
                                {locale === "ar" ? "أيام فيها جرعة غير مُعطاة: " : "Days with a dose not given: "}
                                <Ltr>{m.missed_dates.map((d) => d.slice(5)).join(", ")}</Ltr>
                              </span>
                            )}
                          </td>
                          <td className="py-1.5 pe-2 align-top"><Ltr>{m.expected}</Ltr></td>
                          <td className="py-1.5 pe-2 align-top"><Ltr>{m.taken}</Ltr></td>
                          <td className={`py-1.5 pe-2 align-top ${m.partial || m.vomited ? "font-bold text-orange-700 dark:text-orange-300" : ""}`}>
                            <Ltr>{m.partial} / {m.vomited}</Ltr>
                          </td>
                          <td className="py-1.5 pe-2 align-top"><Ltr>{m.refused} / {m.skipped}</Ltr></td>
                          <td className={`py-1.5 pe-2 align-top ${m.not_recorded ? "font-black text-rose-700 dark:text-rose-300" : ""}`}><Ltr>{m.not_recorded}</Ltr></td>
                          {showPaused && <td className="py-1.5 pe-2 align-top text-slate-500"><Ltr>{m.paused ?? 0}</Ltr></td>}
                          <td className="py-1.5 align-top font-bold"><Ltr>{m.expected ? `${Math.round(m.rate)}%` : "--"}</Ltr></td>
                        </tr>
                        </React.Fragment>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}

            {/* Oxygen and temperature per household day (issue #22) */}
            {(summary.daily_oxygen_temp ?? []).length > 0 && (() => {
              const rows = summary.daily_oxygen_temp!;
              const st = summary.vitals_stats;
              const lowO2 = (n?: number) => n != null && classifySpO2(n) !== "NORMAL";
              const fever = (c?: number) => c != null && classifyTemperature(c) === "FEVER";
              return (
                <div className="mb-5 print:mb-3 break-inside-avoid" data-testid="oxygen-temp-report">
                  <h3 className="mb-1 text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300">
                    {locale === "ar" ? "الأكسجين والحرارة يومياً" : "Oxygen and temperature by day"}
                  </h3>
                  <p className="mb-2 text-xs text-slate-600 dark:text-slate-300">
                    {st.min_spo2 != null && (
                      <>{locale === "ar" ? "أدنى أكسجين" : "Lowest SpO₂"}: <Ltr><b>{st.min_spo2}%</b></Ltr> · </>
                    )}
                    {locale === "ar" ? "قراءات منخفضة" : "Low readings"}: <Ltr>{st.low_spo2_count ?? 0}</Ltr>
                    {" ("}{locale === "ar" ? "خطيرة" : "critical"}: <Ltr>{st.critical_spo2_count ?? 0}</Ltr>{")"}
                    {st.max_temperature_c != null && (
                      <> · {locale === "ar" ? "أعلى حرارة" : "Highest temp"}: <Ltr><b>{st.max_temperature_c.toFixed(1)} °C</b></Ltr></>
                    )}
                    {" · "}{locale === "ar" ? "مرات الحمّى" : "Fever readings"}: <Ltr>{st.fever_count ?? 0}</Ltr>
                  </p>
                  <table className="w-full text-start text-xs print:text-[8pt]">
                    <thead>
                      <tr className="border-b border-slate-200 text-slate-500 dark:border-slate-800">
                        <th className="py-1 text-start font-semibold">{locale === "ar" ? "اليوم" : "Day"}</th>
                        <th className="py-1 text-start font-semibold">{locale === "ar" ? "أدنى أكسجين" : "Lowest SpO₂"}</th>
                        <th className="py-1 text-start font-semibold">{locale === "ar" ? "أعلى حرارة" : "Highest temp"}</th>
                        <th className="py-1 text-start font-semibold">{locale === "ar" ? "القراءات" : "Readings"}</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                      {rows.map((r) => (
                        <tr key={r.date}>
                          <td className="py-1 pe-2"><Ltr>{r.date.slice(5)}</Ltr></td>
                          <td className={`py-1 pe-2 ${lowO2(r.min_spo2) ? "font-bold text-rose-700 dark:text-rose-300" : ""}`}>
                            {r.min_spo2 != null ? <Ltr>{r.min_spo2}%</Ltr> : "—"}
                          </td>
                          <td className={`py-1 pe-2 ${fever(r.max_temperature_c) ? "font-bold text-orange-700 dark:text-orange-300" : ""}`}>
                            {r.max_temperature_c != null ? <Ltr>{r.max_temperature_c.toFixed(1)} °C</Ltr> : "—"}
                          </td>
                          <td className="py-1"><Ltr>{r.readings}</Ltr></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              );
            })()}

            {/* PRN (As-needed) Medications usage per day with reasons (Issue #25) */}
            {(summary.prn_logs ?? []).length > 0 && (
              <div className="mb-5 print:mb-3 break-inside-avoid">
                <div className="flex items-center justify-between mb-2">
                  <h3 className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300">
                    {locale === "ar" ? "أدوية عند اللزوم (PRN) المستخدمة" : "As-Needed (PRN) Medications Given"}
                  </h3>
                  <span className="text-xs text-slate-500">
                    <Ltr>{summary.prn_logs!.length}</Ltr> {locale === "ar" ? "جرعة إجمالاً" : "total doses"}
                  </span>
                </div>
                <div className="overflow-x-auto">
                  <table className="w-full text-start text-xs print:text-[9pt]">
                    <thead>
                      <tr className="border-b border-slate-200 text-slate-500 dark:border-slate-800">
                        <th className="py-1.5 text-start font-semibold">{locale === "ar" ? "التاريخ والوقت" : "Date & Time"}</th>
                        <th className="py-1.5 text-start font-semibold">{locale === "ar" ? "الدواء والجرعة" : "Medication & Dose"}</th>
                        <th className="py-1.5 text-start font-semibold">{locale === "ar" ? "السبب" : "Reason"}</th>
                        <th className="py-1.5 text-start font-semibold">{locale === "ar" ? "ملاحظات" : "Notes"}</th>
                        <th className="py-1.5 text-start font-semibold">{locale === "ar" ? "المُسجل" : "Caregiver"}</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                      {summary.prn_logs!.map((log) => {
                        const reasonLabels: Record<string, { ar: string; en: string; badge: string }> = {
                          headache: { ar: "صداع", en: "Headache", badge: "bg-amber-100 text-amber-800 dark:bg-amber-950/60 dark:text-amber-300" },
                          pain: { ar: "ألم", en: "Pain", badge: "bg-rose-100 text-rose-800 dark:bg-rose-950/60 dark:text-rose-300" },
                          fever: { ar: "حرارة", en: "Fever", badge: "bg-orange-100 text-orange-800 dark:bg-orange-950/60 dark:text-orange-300" },
                          cough: { ar: "سعال", en: "Cough", badge: "bg-blue-100 text-blue-800 dark:bg-blue-950/60 dark:text-blue-300" },
                          other: { ar: "أخرى", en: "Other", badge: "bg-slate-100 text-slate-800 dark:bg-slate-800 dark:text-slate-300" },
                        };
                        const r = reasonLabels[log.reason_tag] ?? { ar: log.reason_tag, en: log.reason_tag, badge: "bg-slate-100 text-slate-800 dark:bg-slate-800 dark:text-slate-300" };
                        return (
                          <tr key={log.id}>
                            <td className="py-1.5 pe-2 align-top font-mono text-slate-600 dark:text-slate-400">
                              <Ltr>{formatDateTime(log.given_at, locale)}</Ltr>
                            </td>
                            <td className="py-1.5 pe-2 align-top font-semibold text-slate-900 dark:text-white">
                              <bdi>{locale === "ar" ? log.name_ar : log.name_en}</bdi>
                              <span className="ms-1 font-normal text-slate-500">
                                <Auto>({log.dosage})</Auto>
                              </span>
                            </td>
                            <td className="py-1.5 pe-2 align-top">
                              <span className={`inline-block px-1.5 py-0.5 rounded text-xs font-bold ${r.badge}`}>
                                {locale === "ar" ? r.ar : r.en}
                              </span>
                            </td>
                            <td className="py-1.5 pe-2 align-top text-slate-600 dark:text-slate-400">
                              {log.notes ? <bdi>{log.notes}</bdi> : "--"}
                            </td>
                            <td className="py-1.5 align-top text-slate-600 dark:text-slate-400">
                              {log.caregiver_name}
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              </div>
            )}

            {/* Blood pressure per household day, with a small trend line */}
            {(summary.daily_bp ?? []).length > 0 && (() => {
              const rows = summary.daily_bp!;
              const lo = 60, hi = 200, w = 300, h = 44;
              const x = (i: number) => (rows.length === 1 ? w / 2 : (i * w) / (rows.length - 1));
              const y = (v: number) => h - ((Math.min(hi, Math.max(lo, v)) - lo) / (hi - lo)) * h;
              const line = (key: "avg_sys" | "avg_dia") => rows.map((r, i) => `${x(i).toFixed(1)},${y(r[key]).toFixed(1)}`).join(" ");
              return (
                <div className="mb-5 print:mb-3 break-inside-avoid">
                  <h3 className="mb-2 text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300">
                    {locale === "ar" ? "ضغط الدم يومياً" : "Blood pressure by day"}
                  </h3>
                  <svg viewBox={`-4 -4 ${w + 8} ${h + 8}`} className="mb-2 h-12 w-full print:mb-1 print:h-10" role="img" aria-label={locale === "ar" ? "اتجاه ضغط الدم" : "Blood pressure trend"} style={{ direction: "ltr" }}>
                    <line x1="0" x2={w} y1={y(140)} y2={y(140)} stroke="#f97316" strokeDasharray="3 3" strokeWidth="0.8" />
                    <polyline points={line("avg_sys")} fill="none" stroke="#be123c" strokeWidth="2" />
                    <polyline points={line("avg_dia")} fill="none" stroke="#2563eb" strokeWidth="1.5" />
                  </svg>
                  {/* On paper: one column per day, so 14 days take a few lines instead of a page. */}
                  <table className="hidden w-full table-fixed text-center text-[8pt] print:table" dir="ltr">
                    <tbody>
                      {(
                        [
                          [locale === "ar" ? "اليوم" : "Day", (r: (typeof rows)[number]) => r.date.slice(5)],
                          [locale === "ar" ? "المتوسط" : "Avg", (r: (typeof rows)[number]) => `${Math.round(r.avg_sys)}/${Math.round(r.avg_dia)}`],
                          [locale === "ar" ? "الأعلى" : "Max", (r: (typeof rows)[number]) => String(r.max_sys)],
                          [locale === "ar" ? "القراءات" : "n", (r: (typeof rows)[number]) => String(r.readings)],
                        ] as const
                      ).map(([label, cell]) => (
                        <tr key={label} className="border-b border-slate-200">
                          <th className="w-14 py-0.5 text-start font-semibold">{label}</th>
                          {rows.map((r) => (
                            <td key={r.date} className={`py-0.5 ${label !== (locale === "ar" ? "اليوم" : "Day") && (r.max_sys >= 180 || r.min_sys < 90) ? "font-bold text-rose-700" : ""}`}>
                              {cell(r)}
                            </td>
                          ))}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  <table className="w-full text-start text-xs print:hidden">
                    <thead>
                      <tr className="border-b border-slate-200 text-slate-500 dark:border-slate-800">
                        <th className="py-1 text-start font-semibold">{locale === "ar" ? "اليوم" : "Day"}</th>
                        <th className="py-1 text-start font-semibold">{locale === "ar" ? "المتوسط" : "Average"}</th>
                        <th className="py-1 text-start font-semibold">{locale === "ar" ? "الانقباضي (أدنى–أعلى)" : "Systolic (min–max)"}</th>
                        <th className="py-1 text-start font-semibold">{locale === "ar" ? "القراءات" : "Readings"}</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                      {rows.map((r) => (
                        <tr key={r.date} className={r.max_sys >= 180 ? "bg-rose-50 dark:bg-rose-950/30" : undefined}>
                          <td className="py-1 pe-2"><Ltr>{r.date.slice(5)}</Ltr></td>
                          <td className="py-1 pe-2 font-bold"><Ltr>{Math.round(r.avg_sys)}/{Math.round(r.avg_dia)}</Ltr></td>
                          <td className={`py-1 pe-2 ${r.max_sys >= 180 || r.min_sys < 90 ? "font-bold text-rose-700 dark:text-rose-300" : ""}`}>
                            <Ltr>{r.min_sys}–{r.max_sys}</Ltr>
                          </td>
                          <td className="py-1"><Ltr>{r.readings}</Ltr></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              );
            })()}

            {/* Issue #28: 14-day Fluid Intake Table & Bowel Movement Summary */}
            {((summary.daily_fluid ?? []).length > 0 || summary.bowel_summary) && (
              <div className="mb-6 rounded-2xl border border-slate-200 p-4 dark:border-slate-800 print:mb-3 print:p-2 break-inside-avoid">
                <div className="flex items-center justify-between mb-3 print:mb-1">
                  <h3 className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300 flex items-center gap-1.5">
                    <Droplets className="h-4 w-4 text-sky-600" />
                    <span>{locale === "ar" ? "متابعة السوائل وحركة الأمعاء (آخر ١٤ يوماً)" : "Fluid Intake & Bowel Movements (14 Days)"}</span>
                  </h3>
                </div>

                {/* Bowel Movement Summary Metrics */}
                {summary.bowel_summary && (
                  <div className="mb-4 grid grid-cols-2 sm:grid-cols-4 gap-2 print:gap-1 text-xs">
                    <div className="p-3 print:p-1.5 rounded-xl bg-slate-50 dark:bg-slate-800/40 border border-slate-100 dark:border-slate-800">
                      <span className="text-slate-500 block text-xs">{locale === "ar" ? "إجمالي مرات الإخراج" : "Total Movements"}</span>
                      <span className="text-base print:text-xs font-bold text-slate-900 dark:text-white mt-0.5 block">
                        <Ltr>{summary.bowel_summary.total_movements}</Ltr>
                      </span>
                    </div>

                    <div className="p-3 print:p-1.5 rounded-xl bg-slate-50 dark:bg-slate-800/40 border border-slate-100 dark:border-slate-800">
                      <span className="text-slate-500 block text-xs">{locale === "ar" ? "قوام البراز (طبيعي / قاسي / لين)" : "Consistency (N / H / L)"}</span>
                      <span className="text-sm print:text-xs font-bold text-slate-900 dark:text-white mt-0.5 block">
                        <Ltr>{summary.bowel_summary.normal_count} / {summary.bowel_summary.hard_count} / {summary.bowel_summary.loose_count}</Ltr>
                      </span>
                    </div>

                    <div className={`p-3 print:p-1.5 rounded-xl border ${
                      summary.bowel_summary.longest_gap_days >= 3
                        ? "bg-amber-50 dark:bg-amber-950/30 border-amber-200 dark:border-amber-900 text-amber-900 dark:text-amber-200"
                        : "bg-slate-50 dark:bg-slate-800/40 border-slate-100 dark:border-slate-800"
                    }`}>
                      <span className="block text-xs">{locale === "ar" ? "أطول فترة انقطاع" : "Longest Gap"}</span>
                      <span className="text-base print:text-xs font-bold mt-0.5 block">
                        <Ltr>{summary.bowel_summary.longest_gap_days} {locale === "ar" ? (summary.bowel_summary.longest_gap_days <= 10 ? "أيام" : "يوماً") : "days"}</Ltr>
                        {summary.bowel_summary.longest_gap_days >= 3 && (
                          <span className="ms-1 text-xs font-bold text-amber-600 dark:text-amber-400">
                            ({locale === "ar" ? "تنبيه إمساك" : "Constipation alert"})
                          </span>
                        )}
                      </span>
                    </div>

                    <div className="p-3 print:p-1.5 rounded-xl bg-slate-50 dark:bg-slate-800/40 border border-slate-100 dark:border-slate-800">
                      <span className="text-slate-500 block text-xs">{locale === "ar" ? "آخر إخراج مسجل" : "Last Movement"}</span>
                      <span className="text-sm print:text-xs font-semibold text-slate-900 dark:text-white mt-0.5 block">
                        {summary.bowel_summary.days_since_last != null ? (
                          locale === "ar" ? `قبل ${summary.bowel_summary.days_since_last} يوم` : `${summary.bowel_summary.days_since_last}d ago`
                        ) : (
                          locale === "ar" ? "غير مسجل" : "None"
                        )}
                      </span>
                    </div>
                  </div>
                )}

                {/* 14-day Fluid Intake Table */}
                {(summary.daily_fluid ?? []).length > 0 && (
                  <div className="overflow-x-auto">
                    <h4 className="text-xs font-semibold text-slate-600 dark:text-slate-400 mb-1.5">
                      {locale === "ar" ? "جدول استهلاك السوائل اليومي (الهدف: ١٥٠٠ مل):" : "Daily Fluid Intake Table (Target: 1500 ml):"}
                    </h4>
                    <table className="w-full text-start text-xs print:text-[8pt]">
                      <thead>
                        <tr className="border-b border-slate-200 text-slate-500 dark:border-slate-800">
                          <th className="py-1 text-start font-semibold">{locale === "ar" ? "التاريخ" : "Date"}</th>
                          <th className="py-1 text-start font-semibold">{locale === "ar" ? "إجمالي السوائل" : "Total Fluid"}</th>
                          <th className="py-1 text-start font-semibold">{locale === "ar" ? "منها مكثف" : "Thickened"}</th>
                          <th className="py-1 text-start font-semibold">{locale === "ar" ? "نسبة الهدف" : "% of Target"}</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                        {summary.daily_fluid!.map((df) => (
                          <tr key={df.date} className={df.percent_of_target < 50 && df.total_ml > 0 ? "bg-amber-50/60 dark:bg-amber-950/20" : undefined}>
                            <td className="py-1 pe-2 font-mono"><Ltr>{df.date.slice(5)}</Ltr></td>
                            <td className="py-1 pe-2 font-bold text-slate-900 dark:text-white">
                              <Ltr>{df.total_ml} ml</Ltr>
                            </td>
                            <td className="py-1 pe-2 text-slate-600 dark:text-slate-300">
                              <Ltr>{df.thickened_ml > 0 ? `${df.thickened_ml} ml` : "—"}</Ltr>
                            </td>
                            <td className={`py-1 pe-2 font-semibold ${
                              df.percent_of_target >= 100
                                ? "text-emerald-600 dark:text-emerald-400"
                                : df.percent_of_target < 50
                                ? "text-amber-600 dark:text-amber-400 font-bold"
                                : "text-slate-700 dark:text-slate-300"
                            }`}>
                              <Ltr>{df.percent_of_target}%</Ltr>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            )}

            {/* Key Clinical Summary Metrics */}
            <div className="grid grid-cols-2 sm:grid-cols-5 print:grid-cols-5 gap-3 print:gap-2 mb-6 print:mb-2">
              {/* BP Average */}
              <div className="rounded-2xl border border-slate-100 bg-slate-50 p-4 print:p-2 dark:border-slate-800 dark:bg-slate-800/50">
                <span className="text-xs font-semibold text-slate-500 block mb-1">
                  {locale === "ar" ? "متوسط ضغط الدم" : "Avg Blood Pressure"}
                </span>
                <span className="text-lg print:text-base font-black text-slate-900 dark:text-white">
                  {/* Spaced slash inside RTL text reorders to "80 / 120" unless isolated */}
                  <Ltr>{Math.round(summary.vitals_stats.avg_systolic)} / {Math.round(summary.vitals_stats.avg_diastolic)}</Ltr>
                </span>
                <span className="text-xs text-slate-400 block mt-1">
                  {locale === "ar" ? "المدى: " : "Range: "}
                  <Ltr>{summary.vitals_stats.min_systolic}–{summary.vitals_stats.max_systolic}</Ltr>
                </span>
              </div>

              {/* Blood Sugar Average */}
              <div className="rounded-2xl border border-blue-100 bg-blue-50/50 p-4 print:p-2 dark:border-blue-950/60 dark:bg-blue-950/20">
                <span className="text-xs font-semibold text-blue-700 dark:text-blue-300 block mb-1 flex items-center gap-1">
                  <Activity className="h-3.5 w-3.5 text-blue-500" />
                  {locale === "ar" ? "متوسط السكر" : "Avg Glucose"}
                </span>
                <span className="text-lg print:text-base font-black text-blue-900 dark:text-blue-100">
                  <Ltr>
                    {summary.vitals_stats.avg_blood_sugar ? summary.vitals_stats.avg_blood_sugar.toFixed(1) : "--"}{" "}
                    <span className="text-xs font-normal">mmol/L</span>
                  </Ltr>
                </span>
                <span className="text-xs text-slate-400 block mt-1">
                  {locale === "ar" ? "سكر الدم" : "Blood Sugar"}
                  {summary.vitals_stats.unverified_sugar_count ? (
                    <>
                      {" · "}
                      <Ltr>{summary.vitals_stats.unverified_sugar_count}</Ltr>{" "}
                      {locale === "ar" ? "قراءة غير مؤكدة مستبعدة" : "unverified excluded"}
                    </>
                  ) : null}
                </span>
              </div>

              {/* Medication Adherence */}
              <div className="rounded-2xl border border-slate-100 bg-slate-50 p-4 print:p-2 dark:border-slate-800 dark:bg-slate-800/50">
                <span className="text-xs font-semibold text-slate-500 block mb-1 flex items-center gap-1">
                  <ShieldCheck className="h-3.5 w-3.5 text-emerald-500" />
                  {t.adherenceRate}
                </span>
                <span className="text-lg print:text-base font-black text-emerald-600 dark:text-emerald-400">
                  <Ltr>{Math.round(summary.adherence.adherence_rate)}%</Ltr>
                </span>
                <span className="text-xs text-slate-400 block mt-1">
                  <Ltr>{summary.adherence.total_taken}/{summary.adherence.total_scheduled}</Ltr> {locale === "ar" ? "جرعة أُعطيت" : "doses given"}
                  {summary.adherence.total_not_recorded ? (
                    <span className="block font-bold text-rose-700 dark:text-rose-300">
                      <Ltr>{summary.adherence.total_not_recorded}</Ltr> {locale === "ar" ? "لم تُسجّل" : "not recorded"}
                    </span>
                  ) : null}
                </span>
              </div>

              {/* Average Pulse */}
              <div className="rounded-2xl border border-slate-100 bg-slate-50 p-4 print:p-2 dark:border-slate-800 dark:bg-slate-800/50">
                <span className="text-xs font-semibold text-slate-500 block mb-1 flex items-center gap-1">
                  <Heart className="h-3.5 w-3.5 text-rose-500" />
                  {locale === "ar" ? "متوسط النبض" : "Average Pulse"}
                </span>
                <span className="text-lg print:text-base font-black text-slate-900 dark:text-white">
                  <Ltr>
                    {Math.round(summary.vitals_stats.avg_pulse)} <span className="text-xs font-normal">bpm</span>
                  </Ltr>
                </span>
                <span className="text-xs text-slate-400 block mt-1">
                  {summary.vitals_stats.total_readings} {locale === "ar" ? "قراءة" : "readings"}
                </span>
              </div>

              {/* Crisis Episodes */}
              <div className="rounded-2xl border border-slate-100 bg-slate-50 p-4 print:p-2 dark:border-slate-800 dark:bg-slate-800/50">
                <span className="text-xs font-semibold text-slate-500 block mb-1 flex items-center gap-1">
                  <AlertTriangle className="h-3.5 w-3.5 text-amber-500" />
                  {locale === "ar" ? "أزمات الضغط" : "Crisis Readings"}
                </span>
                <span className={`text-lg print:text-base font-black ${summary.vitals_stats.crisis_count > 0 ? "text-rose-600" : "text-slate-900 dark:text-white"}`}>
                  {summary.vitals_stats.crisis_count}
                </span>
                <span className="text-xs text-slate-400 block mt-1">
                  {summary.vitals_stats.elevated_count} {locale === "ar" ? "مرتفعة" : "elevated"}
                </span>
              </div>
            </div>

            {/* Cognitive State Taxonomy Distribution */}
            <div className="mb-6 rounded-2xl border border-slate-200 p-4 dark:border-slate-800 print:mb-2 print:p-2">
              <h3 className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300 mb-3 print:mb-1">
                {t.cognitiveDistribution}
              </h3>
              <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 print:grid-cols-5 gap-2 print:gap-1">
                {Object.entries(summary.alertness_distribution).map(([level, count]) => {
                  const meta =
                    level === "not_assessed"
                      ? { labelAr: "لم تُقيَّم", labelEn: "Not assessed" }
                      : ALERTNESS_METAS[level as keyof typeof ALERTNESS_METAS];
                  return (
                    <div key={level} className="p-3 print:p-1 rounded-xl bg-slate-50 dark:bg-slate-800/40 border border-slate-100 dark:border-slate-800">
                      <span className="text-xs font-semibold text-slate-800 dark:text-slate-200 block">
                        {locale === "ar" ? meta?.labelAr : meta?.labelEn}
                      </span>
                      <span className="text-base print:text-xs font-bold text-slate-900 dark:text-white mt-1 print:mt-0 block">
                        {count} {locale === "ar" ? (count >= 3 && count <= 10 ? "مرات" : "مرة") : count === 1 ? "time" : "times"}
                      </span>
                    </div>
                  );
                })}
              </div>
            </div>

            {/* Details: on screen always; printed only when asked, on a new page, so the
                default printout is the one-page A4 summary above. */}
            <div className={printDetails ? "print:break-before-page" : "print:hidden"}>
            {(summary.answered_questions ?? []).length > 0 && (
              <div data-testid="report-answered" className="mb-6">
                <h3 className="mb-2 text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300">
                  {locale === "ar" ? "ما قاله الطبيب (أسئلة سابقة)" : "What the doctor said (earlier questions)"}
                </h3>
                <ul className="space-y-1 text-xs text-slate-800 dark:text-slate-200">
                  {summary.answered_questions!.map((q) => (
                    <li key={q.id}>
                      <bdi className="font-bold">{q.text}</bdi> — <bdi>{q.answer}</bdi> (<Auto>{formatDateTime(q.answered_at, locale)}</Auto>)
                    </li>
                  ))}
                </ul>
              </div>
            )}
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
                      <th className="py-2 text-start font-semibold">{locale === "ar" ? "سكر الدم" : "Blood Sugar"}</th>
                      <th className="py-2 text-start font-semibold">{locale === "ar" ? "النبض" : "Pulse"}</th>
                      <th className="py-2 text-start font-semibold">{locale === "ar" ? "الحالة الإدراكية" : "Alertness"}</th>
                      <th className="py-2 text-start font-semibold">{locale === "ar" ? "المُسجل" : "Caregiver"}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                    {(summary.recent_vitals ?? []).slice(0, 10).map((v) => (
                      <tr key={v.id}>
                        <td className="py-2 pe-3 font-mono text-slate-600 dark:text-slate-400">
                          <Ltr>{formatDateTime(v.recorded_at, locale)}</Ltr>
                        </td>
                        <td className="py-2 pe-3 font-bold text-slate-900 dark:text-white">
                          {/* sugar/alertness-only rows have no BP: previously rendered "undefined/undefined mmHg" */}
                          {v.systolic != null && v.diastolic != null ? <Ltr>{v.systolic}/{v.diastolic} mmHg</Ltr> : "--"}
                          {v.correction?.original_systolic != null && (
                            <span className="block text-xs font-normal text-amber-700">
                              {locale === "ar" ? "مُصحّحة، الأصلي: " : "corrected, original: "}
                              <Ltr className="line-through">{v.correction.original_systolic}/{v.correction.original_diastolic}</Ltr>
                              {v.correction.reason && <> — {v.correction.reason}</>}
                            </span>
                          )}
                        </td>
                        <td className="py-2 pe-3 font-semibold text-blue-700 dark:text-blue-300">
                          {v.blood_sugar ? <Ltr>{Number(v.blood_sugar.toFixed(1))} mmol/L</Ltr> : "--"}
                          {v.blood_sugar && v.suspect_default_sugar ? (
                            <span className="ms-1 rounded bg-amber-100 px-1 text-xs font-bold text-amber-800 dark:bg-amber-950 dark:text-amber-300">
                              {locale === "ar" ? "غير مؤكد" : "unverified"}
                            </span>
                          ) : null}
                        </td>
                        <td className="py-2 pe-3 text-slate-700 dark:text-slate-300">
                          {v.pulse != null ? <Ltr>{v.pulse} bpm</Ltr> : "--"}
                        </td>
                        <td className="py-2">
                          <span className="px-2 py-0.5 rounded-sm bg-slate-100 dark:bg-slate-800 text-xs font-semibold">
                            {v.alertness_level
                              ? locale === "ar"
                                ? ALERTNESS_METAS[v.alertness_level]?.labelAr
                                : ALERTNESS_METAS[v.alertness_level]?.labelEn
                              : locale === "ar"
                              ? "لم تُقيَّم"
                              : "Not assessed"}
                          </span>
                        </td>
                        <td className="py-2 text-slate-600 dark:text-slate-400">{v.caregiver_name}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>

            {/* Insulin vs sugar: what the scale said, what was given, and why it differed */}
            {summary.insulin_logs && summary.insulin_logs.length > 0 && (() => {
              const logs = summary.insulin_logs;
              const given = logs.filter((l) => l.status === "given");
              const totalUnits = given.reduce((s, l) => s + l.units, 0);
              const deviations = given.filter((l) => l.scale_units === null || l.scale_units !== l.units).length;
              const lows = logs.filter((l) => l.blood_sugar != null && l.blood_sugar < 3.9).length;
              return (
                <div className="mb-6 break-inside-avoid">
                  <h3 className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300 mb-1">
                    {locale === "ar" ? "الإنسولين مقابل قراءات السكر" : "Insulin vs blood sugar"}
                  </h3>
                  <p className="mb-3 text-xs text-slate-500">
                    <Ltr>{given.length}</Ltr> {locale === "ar" ? "إبرة" : "injections"} · <Ltr>{Number(totalUnits.toFixed(1))}</Ltr>{" "}
                    {locale === "ar" ? "وحدة إجمالاً" : "units total"} · <Ltr>{deviations}</Ltr>{" "}
                    {locale === "ar" ? "مختلفة عن الجدول" : "differed from scale"} · <Ltr>{lows}</Ltr>{" "}
                    {locale === "ar" ? "قراءة أقل من 3.9" : "readings below 3.9"}
                  </p>
                  <div className="overflow-x-auto">
                    <table className="w-full text-start text-xs">
                      <thead>
                        <tr className="border-b border-slate-200 dark:border-slate-800 text-slate-400">
                          <th className="py-2 text-start font-semibold">{locale === "ar" ? "الوقت" : "Time"}</th>
                          <th className="py-2 text-start font-semibold">{locale === "ar" ? "الإنسولين" : "Insulin"}</th>
                          <th className="py-2 text-start font-semibold">{locale === "ar" ? "السكر" : "Sugar"}</th>
                          <th className="py-2 text-start font-semibold">{locale === "ar" ? "الجدول" : "Scale"}</th>
                          <th className="py-2 text-start font-semibold">{locale === "ar" ? "أُعطي" : "Given"}</th>
                          <th className="py-2 text-start font-semibold">{locale === "ar" ? "السبب" : "Reason"}</th>
                          <th className="py-2 text-start font-semibold">{locale === "ar" ? "المُسجل" : "By"}</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                        {logs.slice(0, 60).map((l) => {
                          const differed = l.status === "given" && (l.scale_units === null || l.scale_units !== l.units);
                          return (
                            <tr key={l.id} className={differed ? "bg-amber-50 dark:bg-amber-950/30" : undefined}>
                              <td className="py-2 pe-3 font-mono text-slate-600 dark:text-slate-400"><Ltr>{formatDateTime(l.given_at, locale)}</Ltr></td>
                              <td className="py-2 pe-3"><bdi>{locale === "ar" ? l.plan_name_ar : l.plan_name_en}</bdi></td>
                              <td className={`py-2 pe-3 font-semibold ${l.blood_sugar != null && l.blood_sugar < 3.9 ? "text-rose-600" : "text-blue-700 dark:text-blue-300"}`}>
                                {l.blood_sugar != null ? <Ltr>{l.blood_sugar.toFixed(1)}</Ltr> : "--"}
                              </td>
                              <td className="py-2 pe-3">{l.scale_units != null ? <Ltr>{l.scale_units}</Ltr> : "—"}</td>
                              <td className="py-2 pe-3 font-bold">{l.status === "given" ? <Ltr>{l.units}</Ltr> : locale === "ar" ? "لم تُعطَ" : "held"}</td>
                              <td className="py-2 pe-3 text-slate-600 dark:text-slate-400"><bdi>{l.reason}</bdi></td>
                              <td className="py-2 text-slate-600 dark:text-slate-400">{l.caregiver_name}</td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                </div>
              );
            })()}

            </div>

            {/* Doctor Examination Notes Area */}
            <div className="mt-8 border-t-2 border-dashed border-slate-300 pt-6 print:mt-3 print:pt-2">
              <h4 className="text-xs font-bold text-slate-700 uppercase tracking-wider mb-2">
                {t.doctorNotesPlaceholder}
              </h4>
              <div className="h-28 print:h-16 rounded-2xl border border-slate-200 p-4 bg-slate-50/50 print:border-slate-400" />
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
