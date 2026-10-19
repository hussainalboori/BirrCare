"use client";

import React, { useState, useEffect } from "react";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLanguage } from "@/context/LanguageContext";
import { Medication } from "@/lib/types";
import { canManageMedications, isSuperAdmin } from "@/lib/permissions";
import { fetchApi } from "@/lib/api";
import {
  X,
  Calendar,
  Clock,
  Check,
  Save,
  AlertCircle,
  Loader2,
  Sparkles,
  ShieldCheck,
  Info,
} from "lucide-react";

interface ScheduleManagementModalProps {
  isOpen: boolean;
  onClose: () => void;
  onUpdated?: () => void;
}

const DAYS_LIST = [
  { id: "sun", labelAr: "الأحد", labelEn: "Sun" },
  { id: "mon", labelAr: "الإثنين", labelEn: "Mon" },
  { id: "tue", labelAr: "الثلاثاء", labelEn: "Tue" },
  { id: "wed", labelAr: "الأربعاء", labelEn: "Wed" },
  { id: "thu", labelAr: "الخميس", labelEn: "Thu" },
  { id: "fri", labelAr: "الجمعة", labelEn: "Fri" },
  { id: "sat", labelAr: "السبت", labelEn: "Sat" },
];

const SLOT_LABELS: Record<string, { ar: string; en: string; defaultTime: string }> = {
  morning: { ar: "الصباح", en: "Morning", defaultTime: "08:00" },
  afternoon: { ar: "الظهيرة", en: "Afternoon", defaultTime: "14:00" },
  evening: { ar: "المساء", en: "Evening", defaultTime: "20:00" },
  bedtime: { ar: "قبل النوم", en: "Bedtime", defaultTime: "22:30" },
  night: { ar: "الليل", en: "Night", defaultTime: "02:00" },
};

const ALL_DAY_IDS = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];

export function ScheduleManagementModal({
  isOpen,
  onClose,
  onUpdated,
}: ScheduleManagementModalProps) {
  const { currentCaregiver } = useCaregiver();
  const { locale, t } = useLanguage();

  const [meds, setMeds] = useState<Medication[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Per-medication schedule edits: { [medId]: { days: string[], times: Record<string, string> } }
  const [scheduleDrafts, setScheduleDrafts] = useState<
    Record<string, { days: string[]; times: Record<string, string> }>
  >({});
  const [savingMedId, setSavingMedId] = useState<string | null>(null);
  const [savedSuccessId, setSavedSuccessId] = useState<string | null>(null);

  const canEdit = canManageMedications(currentCaregiver?.role);

  const loadMeds = async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchApi<Medication[]>("/medications");
      setMeds(data || []);

      // Initialize drafts
      const drafts: Record<string, { days: string[]; times: Record<string, string> }> = {};
      (data || []).forEach((m) => {
        const initialDays =
          m.days_of_week && m.days_of_week.length > 0 ? m.days_of_week : ALL_DAY_IDS;
        const initialTimes: Record<string, string> = {};
        m.frequency_slots.forEach((s) => {
          initialTimes[s] =
            m.slot_times?.[s] || SLOT_LABELS[s]?.defaultTime || "08:00";
        });
        drafts[m.id] = { days: initialDays, times: initialTimes };
      });
      setScheduleDrafts(drafts);
    } catch (err: any) {
      setError(err.message || "Failed to load medications schedule");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (isOpen) {
      loadMeds();
    }
  }, [isOpen]);

  if (!isOpen) return null;

  const handleToggleDay = (medId: string, dayId: string) => {
    if (!canEdit) return;
    setScheduleDrafts((prev) => {
      const current = prev[medId] || { days: ALL_DAY_IDS, times: {} };
      const currentDays = current.days;
      let newDays: string[];
      if (currentDays.includes(dayId)) {
        if (currentDays.length > 1) {
          newDays = currentDays.filter((d) => d !== dayId);
        } else {
          newDays = currentDays; // At least one day required
        }
      } else {
        newDays = [...currentDays, dayId];
      }
      return {
        ...prev,
        [medId]: { ...current, days: newDays },
      };
    });
  };

  const handleSetAllDays = (medId: string) => {
    if (!canEdit) return;
    setScheduleDrafts((prev) => {
      const current = prev[medId] || { days: ALL_DAY_IDS, times: {} };
      return {
        ...prev,
        [medId]: { ...current, days: ALL_DAY_IDS },
      };
    });
  };

  const handleTimeChange = (medId: string, slot: string, newTime: string) => {
    if (!canEdit) return;
    setScheduleDrafts((prev) => {
      const current = prev[medId] || { days: ALL_DAY_IDS, times: {} };
      return {
        ...prev,
        [medId]: {
          ...current,
          times: {
            ...current.times,
            [slot]: newTime,
          },
        },
      };
    });
  };

  const handleSaveMedSchedule = async (medId: string) => {
    if (!canEdit) return;
    const draft = scheduleDrafts[medId];
    if (!draft) return;

    setSavingMedId(medId);
    setError(null);
    try {
      await fetchApi(`/medications/${medId}/schedule`, {
        method: "PUT",
        body: JSON.stringify({
          days_of_week: draft.days,
          slot_times: draft.times,
        }),
      });

      setSavedSuccessId(medId);
      setTimeout(() => setSavedSuccessId(null), 3000);

      if (onUpdated) {
        onUpdated();
      }
    } catch (err: any) {
      setError(err.message || "Failed to update schedule");
    } finally {
      setSavingMedId(null);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-3 sm:p-4 overflow-y-auto">
      <div className="relative w-full max-w-3xl rounded-3xl bg-white shadow-2xl dark:bg-slate-900 border border-slate-200 dark:border-slate-800 my-auto overflow-hidden animate-in fade-in zoom-in-95 duration-200">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-slate-100 bg-slate-50/80 px-4 sm:px-6 py-4 dark:border-slate-800 dark:bg-slate-800/50">
          <div className="flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-2xl bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300">
              <Calendar className="h-6 w-6" />
            </div>
            <div>
              <h2 className="text-base sm:text-lg font-bold text-slate-900 dark:text-white flex items-center gap-2">
                <span>{t.manageSchedule}</span>
                <span className="inline-flex items-center gap-1 rounded-md bg-emerald-50 px-2 py-0.5 text-[10px] font-bold text-emerald-700 dark:bg-emerald-950/80 dark:text-emerald-300 border border-emerald-200 dark:border-emerald-800">
                  <ShieldCheck className="h-3 w-3" />
                  {locale === "ar" ? "صلاحيات المشرف والمدير" : "Super Admin & Admin"}
                </span>
              </h2>
              <p className="text-xs text-slate-500 dark:text-slate-400">
                {t.scheduleSubtitle}
              </p>
            </div>
          </div>
          <button
            onClick={onClose}
            className="flex h-10 w-10 items-center justify-center rounded-full text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800 dark:hover:text-slate-200 transition-colors"
            aria-label={t.cancel}
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Content Body */}
        <div className="max-h-[75vh] overflow-y-auto p-4 sm:p-6 space-y-4">
          {error && (
            <div className="flex items-center gap-2 rounded-2xl bg-rose-50 p-3 text-xs text-rose-700 dark:bg-rose-950/50 dark:text-rose-300 border border-rose-200 dark:border-rose-800">
              <AlertCircle className="h-4 w-4 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {/* Intro Notice */}
          <div className="flex items-start gap-2.5 rounded-2xl bg-sky-50 p-3 text-xs text-sky-800 dark:bg-sky-950/40 dark:text-sky-300 border border-sky-200 dark:border-sky-800">
            <Info className="h-4 w-4 shrink-0 mt-0.5" />
            <div>
              <p className="font-semibold">
                {locale === "ar"
                  ? "تحديد أيام ومواعيد الأدوية لضبط التعافي العصبي"
                  : "Prescription Schedule & Timing Rule Engine"}
              </p>
              <p className="text-[11px] text-sky-700 dark:text-sky-400 mt-0.5">
                {locale === "ar"
                  ? "الجرعات المحددة ستظهر تلقائياً في الصفحة الرئيسية فقط في الأيام المقررة وبالمواعيد المحددة بالدقائق لمنع اللبس بين الإخوة."
                  : "Doses will only appear on the family dashboard on their prescribed days and sorted by exact clock times."}
              </p>
            </div>
          </div>

          {loading ? (
            <div className="flex flex-col items-center justify-center py-12 text-slate-400">
              <Loader2 className="h-8 w-8 animate-spin text-emerald-600 mb-2" />
              <p className="text-xs">{locale === "ar" ? "جارٍ تحميل مواعيد الأدوية..." : "Loading medication schedule..."}</p>
            </div>
          ) : meds.length === 0 ? (
            <div className="text-center py-12 text-slate-400">
              <Calendar className="h-10 w-10 mx-auto text-slate-300 dark:text-slate-600 mb-2" />
              <p className="text-sm font-semibold text-slate-600 dark:text-slate-300">
                {locale === "ar" ? "لا توجد أدوية مسجلة في الدليل حالياً" : "No medications registered yet"}
              </p>
            </div>
          ) : (
            <div className="space-y-4">
              {meds.map((med) => {
                const draft = scheduleDrafts[med.id] || {
                  days: ALL_DAY_IDS,
                  times: {},
                };
                const isAllDays = draft.days.length === 7;
                const isSaving = savingMedId === med.id;
                const isSaved = savedSuccessId === med.id;

                return (
                  <div
                    key={med.id}
                    className="rounded-2xl border border-slate-200/90 bg-white p-4 shadow-xs dark:border-slate-800 dark:bg-slate-800/80 transition-all hover:border-emerald-300 dark:hover:border-emerald-700"
                  >
                    {/* Medication Title Bar */}
                    <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 pb-3 dark:border-slate-700/60">
                      <div className="flex items-center gap-3">
                        {med.image_url ? (
                          <img
                            src={med.image_url}
                            alt=""
                            className="h-10 w-10 rounded-xl bg-slate-50 p-1 object-contain dark:bg-slate-900 border border-slate-200 dark:border-slate-700"
                          />
                        ) : (
                          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-emerald-50 text-emerald-700 font-bold dark:bg-emerald-950 dark:text-emerald-300">
                            {med.name_ar.slice(0, 1)}
                          </div>
                        )}
                        <div>
                          <div className="flex items-center gap-2">
                            <h3 className="font-bold text-sm text-slate-900 dark:text-white">
                              {locale === "ar" ? med.name_ar : med.name_en || med.name_ar}
                            </h3>
                            <span className="rounded-md bg-emerald-100 px-2 py-0.5 text-[11px] font-bold text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300">
                              {med.dosage}
                            </span>
                          </div>
                          {med.name_en && locale === "ar" && (
                            <p className="text-[11px] text-slate-400 font-mono">
                              {med.name_en}
                            </p>
                          )}
                        </div>
                      </div>

                      {/* Save Button for this Medication */}
                      <button
                        type="button"
                        disabled={isSaving || !canEdit}
                        onClick={() => handleSaveMedSchedule(med.id)}
                        className={`flex min-h-10 items-center justify-center gap-1.5 rounded-xl px-3.5 py-1.5 text-xs font-bold transition-all active:scale-95 shadow-xs ${
                          isSaved
                            ? "bg-emerald-600 text-white"
                            : "bg-emerald-50 hover:bg-emerald-100 text-emerald-800 dark:bg-emerald-950/80 dark:text-emerald-300 dark:hover:bg-emerald-900"
                        }`}
                      >
                        {isSaving ? (
                          <>
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                            <span>{t.saving}</span>
                          </>
                        ) : isSaved ? (
                          <>
                            <Check className="h-3.5 w-3.5" />
                            <span>{locale === "ar" ? "تم الحفظ!" : "Saved!"}</span>
                          </>
                        ) : (
                          <>
                            <Save className="h-3.5 w-3.5" />
                            <span>{locale === "ar" ? "حفظ المواعيد" : "Save Schedule"}</span>
                          </>
                        )}
                      </button>
                    </div>

                    {/* Schedule Configuration Grid */}
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pt-3.5">
                      {/* Left: Days of the Week */}
                      <div>
                        <div className="flex items-center justify-between mb-2">
                          <label className="text-xs font-bold text-slate-700 dark:text-slate-300 flex items-center gap-1.5">
                            <Calendar className="h-3.5 w-3.5 text-emerald-600 dark:text-emerald-400" />
                            <span>{t.scheduledDays}</span>
                          </label>

                          <button
                            type="button"
                            onClick={() => handleSetAllDays(med.id)}
                            className={`text-[11px] font-semibold px-2 py-0.5 rounded-lg border transition-all ${
                              isAllDays
                                ? "bg-emerald-500 text-white border-emerald-500"
                                : "bg-slate-100 text-slate-600 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-300 border-slate-200 dark:border-slate-700"
                            }`}
                          >
                            {t.allDays}
                          </button>
                        </div>

                        {/* Days Chips */}
                        <div className="flex flex-wrap gap-1.5">
                          {DAYS_LIST.map((day) => {
                            const isSelected = draft.days.includes(day.id);
                            return (
                              <button
                                type="button"
                                key={day.id}
                                onClick={() => handleToggleDay(med.id, day.id)}
                                className={`flex min-h-9 min-w-9 items-center justify-center px-2.5 py-1 rounded-xl text-xs font-bold border transition-all active:scale-95 ${
                                  isSelected
                                    ? "bg-emerald-600 text-white border-emerald-600 shadow-xs"
                                    : "bg-slate-50 text-slate-500 hover:border-slate-300 dark:bg-slate-900 dark:text-slate-400 border-slate-200 dark:border-slate-700"
                                }`}
                              >
                                {locale === "ar" ? day.labelAr : day.labelEn}
                              </button>
                            );
                          })}
                        </div>
                        <p className="text-[10px] text-slate-400 mt-1">
                          {isAllDays
                            ? locale === "ar"
                              ? "مقرر يومياً طوال الأسبوع"
                              : "Scheduled daily every day"
                            : `${locale === "ar" ? "مقرر في" : "Active on"} ${draft.days.length} ${
                                locale === "ar" ? "أيام أسبوعياً" : "days/week"
                              }`}
                        </p>
                      </div>

                      {/* Right: Clock Times per Slot */}
                      <div>
                        <label className="text-xs font-bold text-slate-700 dark:text-slate-300 mb-2 flex items-center gap-1.5">
                          <Clock className="h-3.5 w-3.5 text-emerald-600 dark:text-emerald-400" />
                          <span>{t.doseTimes}</span>
                        </label>

                        <div className="space-y-2">
                          {med.frequency_slots.map((slot) => {
                            const slotInfo = SLOT_LABELS[slot] || {
                              ar: slot,
                              en: slot,
                              defaultTime: "08:00",
                            };
                            const timeVal = draft.times[slot] || slotInfo.defaultTime;

                            return (
                              <div
                                key={slot}
                                className="flex items-center justify-between rounded-xl bg-slate-50 px-3 py-1.5 border border-slate-200 dark:bg-slate-900/60 dark:border-slate-700/80"
                              >
                                <span className="text-xs font-semibold text-slate-700 dark:text-slate-300">
                                  {locale === "ar" ? slotInfo.ar : slotInfo.en}
                                </span>

                                <div className="flex items-center gap-1.5">
                                  <Clock className="h-3.5 w-3.5 text-slate-400" />
                                  <input
                                    type="time"
                                    value={timeVal}
                                    disabled={!canEdit}
                                    onChange={(e) =>
                                      handleTimeChange(med.id, slot, e.target.value)
                                    }
                                    className="rounded-lg border border-slate-300 bg-white px-2 py-1 text-base font-mono font-bold text-slate-900 focus:border-emerald-500 focus:outline-none dark:border-slate-600 dark:bg-slate-800 dark:text-white"
                                  />
                                </div>
                              </div>
                            );
                          })}
                        </div>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {/* Modal Footer */}
        <div className="flex items-center justify-end border-t border-slate-100 bg-slate-50/80 px-4 sm:px-6 py-3 dark:border-slate-800 dark:bg-slate-800/50">
          <button
            type="button"
            onClick={onClose}
            className="rounded-xl border border-slate-200 bg-white px-4 py-2 text-xs font-bold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-300 transition-colors"
          >
            {locale === "ar" ? "إغلاق" : "Close"}
          </button>
        </div>
      </div>
    </div>
  );
}
