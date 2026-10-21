"use client";

import React, { useState, useEffect, useCallback } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLiveSync } from "@/hooks/useLiveSync";
import { fetchApi } from "@/lib/api";
import { canTakeActions, canManageMedications } from "@/lib/permissions";
import { PRNMedicationStatus, PRNReasonTag } from "@/lib/types";
import { formatTime } from "@/lib/datetime";
import { Ltr } from "@/components/Ltr";
import {
  Pill,
  Clock,
  AlertTriangle,
  CheckCircle2,
  Lock,
  Plus,
  RotateCcw,
  Sparkles,
  ChevronDown,
  ChevronUp,
  Siren,
  Loader2,
  X,
  ShieldAlert,
} from "lucide-react";

const REASON_CHIPS: { id: PRNReasonTag; ar: string; en: string; icon: string }[] = [
  { id: "headache", ar: "صداع", en: "Headache", icon: "🤕" },
  { id: "pain", ar: "ألم", en: "Pain", icon: "⚡" },
  { id: "fever", ar: "حرارة", en: "Fever", icon: "🌡️" },
  { id: "other", ar: "أخرى", en: "Other", icon: "📝" },
];

export function PRNSection() {
  const { locale } = useLanguage();
  const { currentCaregiver, openPinModal } = useCaregiver();
  const router = useRouter();

  const [prnMeds, setPrnMeds] = useState<PRNMedicationStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Modal State
  const [activeMed, setActiveMed] = useState<PRNMedicationStatus | null>(null);
  const [isOverrideMode, setIsOverrideMode] = useState(false);
  const [selectedReason, setSelectedReason] = useState<PRNReasonTag>("headache");
  const [isWorstEverHeadache, setIsWorstEverHeadache] = useState(false);
  const [notes, setNotes] = useState("");
  const [overrideReason, setOverrideReason] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [expandedMedId, setExpandedMedId] = useState<string | null>(null);

  const loadPRNMeds = useCallback(async () => {
    try {
      const data = await fetchApi<PRNMedicationStatus[]>("/medications/prn");
      setPrnMeds(data || []);
      setError(null);
    } catch (err: any) {
      console.warn("Failed fetching PRN medications:", err);
      setError(err.message || "Failed to load PRN medications");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadPRNMeds();
  }, [loadPRNMeds]);

  // Live SSE listener
  useLiveSync({
    onEvent: (e) => {
      if (e.type === "PRN_LOGGED" || e.type === "PRN_VOIDED" || e.type === "MEDICATIONS_UPDATED") {
        loadPRNMeds();
      }
    },
    onResync: () => {
      loadPRNMeds();
    },
  });

  const handleOpenLogModal = (medStatus: PRNMedicationStatus, override: boolean = false) => {
    if (!currentCaregiver) {
      openPinModal();
      return;
    }
    setActiveMed(medStatus);
    setIsOverrideMode(override);
    setSelectedReason("headache");
    setIsWorstEverHeadache(false);
    setNotes("");
    setOverrideReason("");
  };

  const handleCloseModal = () => {
    setActiveMed(null);
    setIsOverrideMode(false);
    setSelectedReason("headache");
    setIsWorstEverHeadache(false);
    setNotes("");
    setOverrideReason("");
  };

  const handleLogDose = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!activeMed || !currentCaregiver) return;

    if (!canTakeActions(currentCaregiver.role)) {
      alert(locale === "ar" ? "حسابك مخصص للمشاهدة فقط" : "Viewer accounts cannot log doses");
      return;
    }

    if (isOverrideMode && !overrideReason.trim()) {
      alert(locale === "ar" ? "يرجى كتابة سبب التجاوز السريري" : "Please provide a reason for clinical override");
      return;
    }

    setIsSubmitting(true);
    try {
      await fetchApi("/medications/prn/log", {
        method: "POST",
        body: JSON.stringify({
          medication_id: activeMed.medication.id,
          reason_tag: selectedReason,
          notes: notes.trim() || undefined,
          override_limit: isOverrideMode && activeMed.lock_reason === "limit_reached",
          override_interval: isOverrideMode && activeMed.lock_reason === "too_soon",
          override_reason: isOverrideMode ? overrideReason.trim() : undefined,
        }),
      });

      handleCloseModal();
      loadPRNMeds();
    } catch (err: any) {
      alert(err.message || (locale === "ar" ? "تعذر تسجيل الجرعة" : "Failed to log PRN dose"));
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleVoidDose = async (logId: string) => {
    if (!currentCaregiver) {
      openPinModal();
      return;
    }
    if (!canTakeActions(currentCaregiver.role)) {
      alert(locale === "ar" ? "حسابك مخصص للمشاهدة فقط" : "Viewer accounts cannot void doses");
      return;
    }

    const confirmMsg =
      locale === "ar"
        ? "هل أنت متأكد من إلغاء هذه الجرعة؟ (نقرة خطأ)"
        : "Are you sure you want to void this dose? (Mis-tap undo)";
    if (!window.confirm(confirmMsg)) return;

    try {
      await fetchApi(`/medications/prn/log/${logId}/void`, {
        method: "POST",
        body: JSON.stringify({ reason: "Mistake / cancellation" }),
      });
      loadPRNMeds();
    } catch (err: any) {
      alert(err.message || (locale === "ar" ? "تعذر إلغاء الجرعة" : "Failed to void dose"));
    }
  };

  if (loading) {
    return (
      <div className="mb-6 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900 animate-pulse">
        <div className="h-5 w-44 rounded bg-slate-200 dark:bg-slate-800 mb-3" />
        <div className="h-16 w-full rounded-xl bg-slate-100 dark:bg-slate-800" />
      </div>
    );
  }

  if (prnMeds.length === 0) {
    return null; // No PRN medications configured
  }

  const isAdmin = currentCaregiver && canManageMedications(currentCaregiver.role);

  return (
    <section className="mb-6 space-y-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300">
            <Pill className="h-4 w-4" />
          </div>
          <div>
            <h2 className="text-sm font-black text-slate-800 dark:text-slate-100">
              {locale === "ar" ? "أدوية عند الحاجة (PRN)" : "As-needed (PRN) Medications"}
            </h2>
            <p className="text-[11px] text-slate-500 dark:text-slate-400">
              {locale === "ar"
                ? "محمية بسقف ٢٤ ساعة والفاصل الزمني الآمن لمنع تراكم الجرعات"
                : "Locked with 24-hour maximum and minimum intervals against accidental overdose"}
            </p>
          </div>
        </div>
      </div>

      <div className="grid gap-3 lg:grid-cols-2">
        {prnMeds.map((medStatus) => {
          const med = medStatus.medication;
          const name = locale === "ar" ? med.name_ar : med.name_en;
          const isExpanded = expandedMedId === med.id;

          const max24 = medStatus.max_per_24h ?? 4;
          const count24 = medStatus.count_last_24h;
          const isLocked = !medStatus.can_give;

          return (
            <div
              key={med.id}
              className={`rounded-2xl border p-4 transition-all shadow-xs ${
                isLocked
                  ? "border-amber-300/80 bg-amber-50/40 dark:border-amber-900/60 dark:bg-amber-950/20"
                  : "border-slate-200 bg-white hover:border-blue-300 dark:border-slate-800 dark:bg-slate-900"
              }`}
            >
              <div className="flex items-start justify-between gap-3">
                <div className="flex items-start gap-3 flex-1 min-w-0">
                  <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-slate-50 p-1 border border-slate-200 dark:border-slate-700 dark:bg-slate-800">
                    <img
                      src={med.image_url || "/meds/pill-default.svg"}
                      alt={name}
                      className="h-full w-full object-contain"
                      onError={(e) => {
                        (e.currentTarget as HTMLImageElement).src = "/meds/pill-default.svg";
                      }}
                    />
                  </div>

                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap mb-1">
                      <span className="text-base font-black text-slate-900 dark:text-white">
                        {name}
                      </span>
                      <span className="rounded-md bg-blue-100 px-2 py-0.5 text-xs font-bold text-blue-800 dark:bg-blue-950/80 dark:text-blue-300">
                        {med.dosage}
                      </span>
                      <span className="rounded-md bg-slate-100 px-2 py-0.5 text-[11px] font-bold text-slate-700 dark:bg-slate-800 dark:text-slate-300">
                        <Ltr>{count24} / {max24}</Ltr> {locale === "ar" ? "اليوم" : "today"}
                      </span>
                    </div>

                    {/* Status string */}
                    <p className="text-xs text-slate-600 dark:text-slate-300 leading-relaxed mb-2">
                      {medStatus.last_given ? (
                        <>
                          {locale === "ar" ? "آخر جرعة: " : "Last: "}
                          <span className="font-bold text-slate-900 dark:text-white">
                            {formatTime(medStatus.last_given.given_at, locale)}
                          </span>
                          {" "}
                          {locale === "ar" ? "بواسطة" : "by"}{" "}
                          <span className="font-bold" style={{ color: medStatus.last_given.avatar_color || "#2563eb" }}>
                            {medStatus.last_given.caregiver_name}
                          </span>
                          {" · "}
                          <span className="text-slate-500">
                            ({REASON_CHIPS.find((r) => r.id === medStatus.last_given?.reason_tag)?.[locale === "ar" ? "ar" : "en"] || medStatus.last_given.reason_tag})
                          </span>
                        </>
                      ) : (
                        <span className="text-slate-400">
                          {locale === "ar" ? "لم تُعطَ أي جرعة في آخر ٢٤ ساعة" : "No doses in the last 24 hours"}
                        </span>
                      )}
                      {medStatus.next_allowed_at && (
                        <>
                          {" · "}
                          <span className="text-amber-800 dark:text-amber-300 font-bold">
                            {locale === "ar" ? "القادم متاح: " : "Next allowed: "}
                            {formatTime(medStatus.next_allowed_at, locale)}
                          </span>
                        </>
                      )}
                    </p>

                    {/* Lock Warning Pill */}
                    {isLocked && (
                      <div className="mb-2 flex items-center gap-1.5 rounded-lg bg-amber-100/80 px-2.5 py-1 text-xs font-bold text-amber-900 dark:bg-amber-950/60 dark:text-amber-200 border border-amber-300/60 dark:border-amber-800/60">
                        <Lock className="h-3.5 w-3.5 shrink-0" />
                        <span>
                          {medStatus.lock_reason === "limit_reached"
                            ? locale === "ar"
                              ? `وصل الحد الأقصى للمسكّن (${max24} جرعات / ٢٤ ساعة)`
                              : `24-hour limit reached (${max24} doses per 24h)`
                            : locale === "ar"
                            ? `أُعطي مؤخراً (فاصل الأمان الأدنى ${medStatus.min_hours ?? 4} ساعات)`
                            : `Given recently (minimum interval is ${medStatus.min_hours ?? 4} hours)`}
                        </span>
                      </div>
                    )}
                  </div>
                </div>

                {/* Primary Action Button */}
                <div className="shrink-0 flex flex-col gap-1 items-end">
                  {!isLocked ? (
                    <button
                      type="button"
                      onClick={() => handleOpenLogModal(medStatus, false)}
                      className="flex min-h-11 items-center gap-1.5 rounded-xl bg-blue-600 hover:bg-blue-700 px-3.5 py-2 text-xs font-black text-white shadow-sm active:scale-95 transition-all"
                    >
                      <Plus className="h-4 w-4 stroke-[3]" />
                      <span>{locale === "ar" ? "أعطِ الجرعة" : "Give Dose"}</span>
                    </button>
                  ) : isAdmin ? (
                    <button
                      type="button"
                      onClick={() => handleOpenLogModal(medStatus, true)}
                      className="flex min-h-11 items-center gap-1 rounded-xl bg-amber-600 hover:bg-amber-700 px-3 py-2 text-xs font-bold text-white shadow-2xs active:scale-95"
                    >
                      <ShieldAlert className="h-4 w-4" />
                      <span>{locale === "ar" ? "تجاوز استثنائي" : "Override"}</span>
                    </button>
                  ) : (
                    <div className="flex min-h-11 items-center gap-1 rounded-xl bg-slate-200 px-3 py-2 text-xs font-bold text-slate-500 dark:bg-slate-800 dark:text-slate-400">
                      <Lock className="h-3.5 w-3.5" />
                      <span>{locale === "ar" ? "مقفل سريرياً" : "Locked"}</span>
                    </div>
                  )}
                </div>
              </div>

              {/* History Toggle Button */}
              {medStatus.recent_logs && medStatus.recent_logs.length > 0 && (
                <div className="mt-2 pt-2 border-t border-slate-100 dark:border-slate-800/80">
                  <button
                    type="button"
                    onClick={() => setExpandedMedId(isExpanded ? null : med.id)}
                    className="flex w-full items-center justify-between text-[11px] font-bold text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200 py-1"
                  >
                    <span>
                      {locale === "ar" ? "سجل الجرعات السابقة" : "Previous intake history"} ({medStatus.recent_logs.length})
                    </span>
                    {isExpanded ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
                  </button>

                  {isExpanded && (
                    <div className="mt-2 space-y-1.5">
                      {medStatus.recent_logs.map((log) => {
                        const isVoided = !!log.voided_at;
                        const reasonChip = REASON_CHIPS.find((r) => r.id === log.reason_tag);
                        return (
                          <div
                            key={log.id}
                            className={`flex items-center justify-between p-2 rounded-xl text-xs ${
                              isVoided
                                ? "bg-slate-100/60 text-slate-400 line-through dark:bg-slate-800/40"
                                : "bg-slate-50 text-slate-700 dark:bg-slate-800/80 dark:text-slate-200"
                            }`}
                          >
                            <div className="flex items-center gap-2">
                              <span>{reasonChip?.icon || "💊"}</span>
                              <span className="font-bold">
                                {formatTime(log.given_at, locale)}
                              </span>
                              <span>·</span>
                              <span style={{ color: log.avatar_color || undefined }} className="font-bold">
                                {log.caregiver_name}
                              </span>
                              <span className="rounded bg-slate-200 dark:bg-slate-700 px-1.5 py-0.5 text-[10px]">
                                {reasonChip?.[locale === "ar" ? "ar" : "en"] || log.reason_tag}
                              </span>
                              {log.notes && <span className="text-[11px] text-slate-400 italic truncate max-w-[120px]">“{log.notes}”</span>}
                            </div>

                            {!isVoided && (
                              <button
                                type="button"
                                onClick={() => handleVoidDose(log.id)}
                                title={locale === "ar" ? "إلغاء نقرة خطأ" : "Void mis-tap"}
                                className="flex items-center gap-1 text-[11px] font-bold text-rose-600 hover:text-rose-700 dark:text-rose-400 px-1.5 py-0.5 rounded hover:bg-rose-50 dark:hover:bg-rose-950/50"
                              >
                                <RotateCcw className="h-3 w-3" />
                                <span>{locale === "ar" ? "تراجع" : "Undo"}</span>
                              </button>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              )}
            </div>
          );
        })}
      </div>

      {/* Give Dose Modal */}
      {activeMed && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/70 p-4 backdrop-blur-xs">
          <div className="w-full max-w-md rounded-3xl bg-white p-5 shadow-2xl dark:bg-slate-900 border border-slate-200 dark:border-slate-800 max-h-[90vh] overflow-y-auto">
            <div className="flex items-center justify-between pb-3 border-b border-slate-100 dark:border-slate-800">
              <div className="flex items-center gap-2">
                <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300">
                  <Pill className="h-5 w-5" />
                </div>
                <div>
                  <h3 className="text-base font-black text-slate-900 dark:text-white">
                    {locale === "ar" ? "تسجيل جرعة عند الحاجة" : "Log PRN Medication Dose"}
                  </h3>
                  <p className="text-xs text-slate-500 dark:text-slate-400">
                    {locale === "ar" ? activeMed.medication.name_ar : activeMed.medication.name_en} ({activeMed.medication.dosage})
                  </p>
                </div>
              </div>

              <button
                type="button"
                onClick={handleCloseModal}
                className="rounded-full p-1.5 text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800"
              >
                <X className="h-5 w-5" />
              </button>
            </div>

            <form onSubmit={handleLogDose} className="mt-4 space-y-4">
              {/* Reason Chips */}
              <div>
                <label className="block text-xs font-black text-slate-800 dark:text-slate-200 mb-2">
                  {locale === "ar" ? "ما سبب إعطاء المسكّن الآن؟ *" : "What is the reason for this dose? *"}
                </label>
                <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                  {REASON_CHIPS.map((chip) => {
                    const isSelected = selectedReason === chip.id;
                    return (
                      <button
                        key={chip.id}
                        type="button"
                        onClick={() => setSelectedReason(chip.id)}
                        className={`flex min-h-12 flex-col items-center justify-center p-2 rounded-2xl border text-xs font-bold transition-all ${
                          isSelected
                            ? "border-blue-600 bg-blue-50 text-blue-900 dark:bg-blue-950/80 dark:text-blue-200 ring-2 ring-blue-500 shadow-xs"
                            : "border-slate-200 bg-white text-slate-700 hover:border-slate-300 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-300"
                        }`}
                      >
                        <span className="text-lg">{chip.icon}</span>
                        <span>{locale === "ar" ? chip.ar : chip.en}</span>
                      </button>
                    );
                  })}
                </div>
              </div>

              {/* Red-Flag Alert for Sudden Severe Post-Stroke Headache */}
              {selectedReason === "headache" && (
                <div className="rounded-2xl border border-rose-300 bg-rose-50/80 p-3.5 dark:border-rose-900 dark:bg-rose-950/40 space-y-2.5">
                  <label className="flex items-start gap-2.5 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={isWorstEverHeadache}
                      onChange={(e) => setIsWorstEverHeadache(e.target.checked)}
                      className="h-5 w-5 rounded border-rose-300 text-rose-600 focus:ring-rose-500 mt-0.5"
                    />
                    <div className="text-xs">
                      <span className="font-black text-rose-950 dark:text-rose-200 block">
                        {locale === "ar"
                          ? "صداع حاد ومفاجئ («الأسوأ في حياتها» / انفجاري)؟"
                          : "Sudden, severe headache ('worst headache of life')?"}
                      </span>
                      <span className="text-rose-800/80 dark:text-rose-300/80 text-[11px]">
                        {locale === "ar"
                          ? "علامة تحذيرية حمراء لاحتمال نزيف دماغي حاد"
                          : "Critical red flag for potential intracranial hemorrhage"}
                      </span>
                    </div>
                  </label>

                  {isWorstEverHeadache && (
                    <div className="rounded-xl bg-rose-100 p-3 text-xs text-rose-950 dark:bg-rose-900/60 dark:text-rose-100 border border-rose-400">
                      <div className="flex items-center gap-1.5 font-black mb-1.5 text-rose-900 dark:text-rose-100">
                        <Siren className="h-4 w-4 animate-bounce text-rose-600 dark:text-rose-400" />
                        <span>
                          {locale === "ar" ? "تحذير سريري طارئ!" : "Emergency Clinical Alert!"}
                        </span>
                      </div>
                      <p className="leading-relaxed mb-3">
                        {locale === "ar"
                          ? "الصداع المفاجئ والشديد جداً لمريض الجلطة الدماغية قد يكون مؤشراً على نزيف دماغي أو تكرار الجلطة. لا يُكتفى بالمسكن أبداً! أجرِ فحص الجلطة السريع فوراً أو اتصل بالطوارئ ٩٩٧."
                          : "A sudden, thunderclap severe headache in a stroke survivor can indicate an acute intracranial bleed or secondary event. Do not treat with simple analgesics alone! Conduct an immediate F.A.S.T. stroke assessment or call 997."}
                      </p>
                      <button
                        type="button"
                        onClick={() => {
                          handleCloseModal();
                          router.push("/vitals?quick=fast");
                        }}
                        className="flex w-full min-h-11 items-center justify-center gap-2 rounded-xl bg-rose-700 hover:bg-rose-800 px-4 py-2 font-black text-white shadow-md active:scale-95 transition-all"
                      >
                        <Siren className="h-4 w-4" />
                        <span>{locale === "ar" ? "الانتقال لفحص الجلطة السريع F.A.S.T." : "Go to F.A.S.T. Stroke Check"}</span>
                      </button>
                    </div>
                  )}
                </div>
              )}

              {/* Admin Clinical Override Reason */}
              {isOverrideMode && (
                <div className="rounded-2xl border border-amber-300 bg-amber-50/80 p-3.5 dark:border-amber-900 dark:bg-amber-950/40">
                  <div className="flex items-center gap-2 font-bold text-xs text-amber-900 dark:text-amber-200 mb-1.5">
                    <ShieldAlert className="h-4 w-4 text-amber-600" />
                    <span>
                      {locale === "ar" ? "تجاوز سريري استثنائي (صلاحية مسؤول) *" : "Clinical Override Reason (Admin Only) *"}
                    </span>
                  </div>
                  <textarea
                    rows={2}
                    value={overrideReason}
                    onChange={(e) => setOverrideReason(e.target.value)}
                    placeholder={locale === "ar" ? "اكتب سبب إعطاء الجرعة رغم القفل (مثال: أمر هاتفي من الطبيب المعالج)..." : "Explain why dose is given despite lock (e.g. physician order)..."}
                    className="w-full rounded-xl border border-amber-300 bg-white p-2.5 text-base text-slate-900 focus:border-amber-500 focus:outline-none dark:border-amber-700 dark:bg-slate-900 dark:text-white"
                    required
                  />
                </div>
              )}

              {/* Optional Notes */}
              <div>
                <label className="block text-xs font-bold text-slate-700 dark:text-slate-300 mb-1">
                  {locale === "ar" ? "ملاحظات إضافية (اختياري)" : "Additional Notes (Optional)"}
                </label>
                <input
                  type="text"
                  value={notes}
                  onChange={(e) => setNotes(e.target.value)}
                  placeholder={locale === "ar" ? "مثال: بعد وجبة الغداء، درجة الحرارة ٣٨.٢..." : "e.g. after lunch, temperature 38.2..."}
                  className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-base text-slate-900 focus:border-blue-500 focus:outline-none dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                />
              </div>

              {/* Actions */}
              <div className="flex items-center justify-end gap-2 pt-2 border-t border-slate-100 dark:border-slate-800">
                <button
                  type="button"
                  onClick={handleCloseModal}
                  className="rounded-xl px-4 py-2.5 text-xs font-bold text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800"
                >
                  {locale === "ar" ? "إلغاء" : "Cancel"}
                </button>
                <button
                  type="submit"
                  disabled={isSubmitting}
                  className={`flex min-h-11 items-center gap-1.5 rounded-xl px-5 py-2.5 text-xs font-black text-white shadow-md active:scale-95 disabled:opacity-50 ${
                    isOverrideMode ? "bg-amber-600 hover:bg-amber-700" : "bg-blue-600 hover:bg-blue-700"
                  }`}
                >
                  {isSubmitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}
                  <span>
                    {isOverrideMode
                      ? locale === "ar"
                        ? "تأكيد التجاوز وإعطاء الجرعة"
                        : "Confirm Override & Give Dose"
                      : locale === "ar"
                      ? "إعطاء الجرعة وتوثيقها"
                      : "Confirm & Give Dose"}
                  </span>
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </section>
  );
}
