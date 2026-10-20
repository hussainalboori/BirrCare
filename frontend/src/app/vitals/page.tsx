"use client";

import React, { useState, useEffect, useCallback, useRef } from "react";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { BloodPressureBadge } from "@/components/BloodPressureBadge";
import { BloodSugarBadge } from "@/components/BloodSugarBadge";
import { CrisisActions } from "@/components/CrisisActions";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLiveSync } from "@/hooks/useLiveSync";
import { VitalsLog, AlertnessLevel, VitalKind } from "@/lib/types";
import { ALERTNESS_METAS, classifyBloodPressure, classifyBloodSugar, BloodSugarTiming } from "@/lib/clinical";
import { canTakeActions, isViewer } from "@/lib/permissions";
import { fetchApi } from "@/lib/api";
import { formatShortDate, formatTime } from "@/lib/datetime";
import { Ltr } from "@/components/Ltr";
import {
  Activity,
  Plus,
  Heart,
  Droplet,
  Brain,
  Clock,
  Loader2,
  Filter,
  AlertTriangle,
  X,
  ArrowLeft,
  CheckCircle2,
  ChevronRight,
  Sparkles,
} from "lucide-react";

export default function VitalsPage() {
  const { t, locale } = useLanguage();
  const { currentCaregiver, openPinModal } = useCaregiver();

  const [vitalsList, setVitalsList] = useState<VitalsLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);
  // Synchronous double-submit guard (vitals have no server-side uniqueness constraint).
  const inFlightRef = useRef(false);

  // Modal & Selection State
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [selectedKind, setSelectedKind] = useState<VitalKind | null>(null);

  // Filters
  const [filterMode, setFilterMode] = useState<"all" | "bp" | "sugar" | "alerts">("all");

  // Form state — kept as strings so a field can be cleared while typing on mobile
  // (Number("") === 0 used to snap the field to "0"). Start empty: a pre-filled
  // "120/80" could be saved as a reading that was never actually measured.
  const [systolic, setSystolic] = useState<string>("");
  const [diastolic, setDiastolic] = useState<string>("");
  const [pulse, setPulse] = useState<string>("");
  // Sugar and alertness start empty too: a pre-filled 6.4 / "alert" was saved as if it
  // had been measured or assessed (issue #10). Only what the caregiver enters is sent.
  const [bloodSugar, setBloodSugar] = useState<string>("");
  const [sugarTiming, setSugarTiming] = useState<BloodSugarTiming>("random");
  const [alertness, setAlertness] = useState<AlertnessLevel | null>(null);
  const [notes, setNotes] = useState<string>("");

  const resetForm = () => {
    setSystolic("");
    setDiastolic("");
    setPulse("");
    setBloodSugar("");
    setSugarTiming("random");
    setAlertness(null);
    setNotes("");
  };
  // Last saved crisis reading: keeps the "what to do now" panel on screen after the modal closes.
  const [savedCrisis, setSavedCrisis] = useState<{ systolic: number; diastolic: number } | null>(null);

  const loadVitals = useCallback(async () => {
    try {
      const data = await fetchApi<VitalsLog[]>("/vitals?limit=40");
      setVitalsList(data || []);
    } catch (err) {
      console.warn("Failed fetching vitals:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadVitals();
  }, [loadVitals]);

  // Live SSE listener
  const { status: connectionStatus } = useLiveSync({
    onVitalLogged: () => {
      loadVitals();
    },
    // Insulin logging also records the sugar reading taken with it.
    onEvent: (e) => {
      if (e.type === "INSULIN_LOGGED") loadVitals();
    },
    onResync: () => {
      loadVitals();
    },
  });

  const handleOpenModal = () => {
    if (!currentCaregiver) {
      openPinModal();
      return;
    }
    if (isViewer(currentCaregiver.role)) {
      alert(locale === "ar" ? "حسابك مخصص للمشاهدة فقط ولا يملك صلاحية تسجيل المؤشرات الحيوية" : "Viewer accounts cannot record vitals");
      return;
    }
    setSelectedKind(null);
    setIsModalOpen(true);
  };

  const handleCloseModal = () => {
    setIsModalOpen(false);
    setSelectedKind(null);
    resetForm();
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!currentCaregiver) {
      openPinModal();
      return;
    }

    if (!canTakeActions(currentCaregiver.role)) {
      alert(locale === "ar" ? "حسابك مخصص للمشاهدة فقط ولا يملك صلاحية تسجيل المؤشرات الحيوية" : "Viewer accounts cannot record vitals");
      return;
    }

    if (inFlightRef.current) return;
    const kind = selectedKind || "full";

    if (kind === "bp" || kind === "full") {
      const sys = Number(systolic), dia = Number(diastolic), pul = Number(pulse);
      const inRange = (v: number, lo: number, hi: number) => Number.isInteger(v) && v >= lo && v <= hi;
      if (!inRange(sys, 50, 300) || !inRange(dia, 30, 200) || !inRange(pul, 30, 220) || dia >= sys) {
        alert(
          locale === "ar"
            ? "يرجى التحقق من قيم الضغط والنبض (الانقباضي 50–300، الانبساطي 30–200 وأقل من الانقباضي، النبض 30–220)"
            : "Please check BP/pulse values (systolic 50–300, diastolic 30–200 and below systolic, pulse 30–220)"
        );
        return;
      }
    }

    inFlightRef.current = true;
    setIsSubmitting(true);

    try {
      const sugarVal = bloodSugar.trim() ? parseFloat(bloodSugar) : undefined;
      const combinedNotes =
        (kind === "sugar" || kind === "full") && sugarTiming !== "random" && sugarVal
          ? `[${sugarTiming === "fasting" ? (locale === "ar" ? "صائم" : "Fasting") : (locale === "ar" ? "بعد الأكل" : "Post-meal")}] ${notes.trim()}`
          : notes.trim();

      const payload: Record<string, any> = {
        vital_type: kind,
        notes: combinedNotes || undefined,
      };

      if (kind === "bp") {
        payload.systolic = Number(systolic);
        payload.diastolic = Number(diastolic);
        payload.pulse = Number(pulse);
      } else if (kind === "sugar") {
        if (!sugarVal || isNaN(sugarVal)) {
          alert(locale === "ar" ? "يرجى إدخال قيمة صحيحة لسكر الدم" : "Please enter a valid blood sugar reading");
          return; // finally{} releases the submit guard
        }
        payload.blood_sugar = sugarVal;
      } else if (kind === "alertness") {
        if (!alertness) {
          alert(locale === "ar" ? "اختر حالة الوعي التي لاحظتها" : "Choose the alertness you observed");
          return; // finally{} releases the submit guard
        }
        payload.alertness_level = alertness;
      } else if (kind === "full") {
        payload.systolic = Number(systolic);
        payload.diastolic = Number(diastolic);
        payload.pulse = Number(pulse);
        // Only what was actually measured / tapped: no sugar or alertness is invented.
        if (sugarVal !== undefined && !isNaN(sugarVal)) payload.blood_sugar = sugarVal;
        if (alertness) payload.alertness_level = alertness;
      }

      await fetchApi("/vitals", {
        method: "POST",
        body: JSON.stringify(payload),
      });

      if (payload.systolic && classifyBloodPressure(payload.systolic, payload.diastolic).status === "CRISIS") {
        setSavedCrisis({ systolic: payload.systolic, diastolic: payload.diastolic });
        window.scrollTo({ top: 0, behavior: "smooth" });
      }
      handleCloseModal(); // also clears every field, so the next check starts empty
      loadVitals();
    } catch (err: any) {
      alert(err.message || "Failed to record vitals");
    } finally {
      inFlightRef.current = false;
      setIsSubmitting(false);
    }
  };

  const numericSystolic = systolic === "" ? NaN : Number(systolic);
  const numericDiastolic = diastolic === "" ? NaN : Number(diastolic);
  const hasValidBP = numericSystolic > 0 && numericDiastolic > 0;
  const numericSugar = parseFloat(bloodSugar);
  const hasValidSugar = !isNaN(numericSugar) && numericSugar > 0;

  // Filtered vitals
  const filteredVitals = vitalsList.filter((v) => {
    if (filterMode === "bp") {
      return (v.vital_type === "bp" || v.vital_type === "full" || (v.systolic && v.diastolic));
    }
    if (filterMode === "sugar") {
      return (v.vital_type === "sugar" || (v.blood_sugar && v.blood_sugar > 0));
    }
    if (filterMode === "alerts") {
      const bpAlert = v.bp_status === "CRISIS" || v.bp_status === "STAGE_2";
      const sugarAlert = v.blood_sugar && (v.blood_sugar < 3.9 || v.blood_sugar >= 10.0);
      return bpAlert || sugarAlert;
    }
    return true;
  });


  return (
    <div className="min-h-screen">
      <CaregiverHeader connectionStatus={connectionStatus} />

      <main className="mx-auto max-w-3xl px-4 py-5 sm:px-6 lg:max-w-none lg:px-8">
        {/* Header */}
        <div className="mb-6 flex items-center justify-between gap-3">
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-xl sm:text-2xl font-black text-slate-900 dark:text-white tracking-tight">
                {t.vitalsTitle}
              </h1>
              <Activity className="h-5 w-5 text-rose-500" />
            </div>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-1">
              {locale === "ar"
                ? "متابعة ضغط الدم، سكر الدم الشرياني، ومستوى الوعي العصبي"
                : "Tracking stroke recovery hemodynamics, blood glucose, and alertness"}
            </p>
          </div>

          {!isViewer(currentCaregiver?.role) && (
            <button
              type="button"
              onClick={handleOpenModal}
              className="flex min-h-12 shrink-0 items-center gap-1.5 rounded-xl bg-emerald-600 hover:bg-emerald-700 px-3.5 py-2.5 text-xs font-bold text-white shadow-emerald-500/20 shadow-sm transition-all active:scale-95"
            >
              <Plus className="h-4 w-4 stroke-[2.5]" />
              <span>{t.recordVitals}</span>
            </button>
          )}
        </div>

        {savedCrisis && (
          <section
            role="alert"
            className="mb-6 rounded-3xl border-2 border-rose-500 bg-rose-50 p-4 shadow-lg dark:bg-rose-950/40"
          >
            <div className="mb-3 flex items-center justify-between gap-2">
              <BloodPressureBadge systolic={savedCrisis.systolic} diastolic={savedCrisis.diastolic} />
              <button
                type="button"
                onClick={() => setSavedCrisis(null)}
                aria-label={locale === "ar" ? "إخفاء" : "Dismiss"}
                className="flex min-h-12 min-w-12 items-center justify-center rounded-full text-rose-700 hover:bg-rose-100 dark:text-rose-300 dark:hover:bg-rose-900/40"
              >
                <X className="h-5 w-5" />
              </button>
            </div>
            <CrisisActions />
          </section>
        )}

        {/* ========================================================================= */}
        {/* POP-UP MODAL: CHOOSE VITAL TYPE OR FILL FOCUSED FORM */}
        {/* ========================================================================= */}
        {isModalOpen && (
          <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 backdrop-blur-xs p-4 animate-in fade-in duration-200"
            onClick={handleCloseModal}
          >
            <div
              role="dialog"
              aria-modal="true"
              className="relative w-full max-w-lg max-h-[90dvh] flex flex-col rounded-3xl bg-white shadow-2xl dark:bg-slate-900 border border-slate-200 dark:border-slate-800 overflow-hidden animate-in zoom-in-95 duration-150"
              onClick={(e) => e.stopPropagation()}
            >
              {/* Modal Header */}
              <div className="flex items-center justify-between p-5 border-b border-slate-100 dark:border-slate-800">
                <div className="flex items-center gap-2.5">
                  {selectedKind ? (
                    <button
                      type="button"
                      onClick={() => setSelectedKind(null)}
                      aria-label={locale === "ar" ? "العودة لاختيار نوع الفحص" : "Back to selection"}
                      className="flex min-h-12 min-w-12 items-center justify-center rounded-xl border border-slate-200 dark:border-slate-700 p-1.5 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
                      title={locale === "ar" ? "العودة لاختيار نوع الفحص" : "Back to selection"}
                    >
                      {/* "Back" points toward the start edge: left in LTR, right in RTL */}
                      <ArrowLeft className="h-4 w-4 rtl:rotate-180" />
                    </button>
                  ) : (
                    <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-rose-100 text-rose-600 dark:bg-rose-950/60 dark:text-rose-400">
                      <Activity className="h-5 w-5" />
                    </div>
                  )}

                  <div>
                    <h3 className="text-base font-bold text-slate-900 dark:text-white">
                      {selectedKind === null
                        ? t.chooseVitalType
                        : selectedKind === "bp"
                        ? t.vitalBPTitle
                        : selectedKind === "sugar"
                        ? t.vitalSugarTitle
                        : selectedKind === "alertness"
                        ? t.vitalAlertnessTitle
                        : t.vitalFullTitle}
                    </h3>
                    <p className="text-[11px] text-slate-500 dark:text-slate-400">
                      {selectedKind === null
                        ? t.chooseVitalTypeSubtitle
                        : locale === "ar"
                        ? "أدخل القراءات المقاسة بدقة"
                        : "Enter measured values accurately"}
                    </p>
                  </div>
                </div>

                <button
                  type="button"
                  onClick={handleCloseModal}
                  aria-label={t.cancel}
                  className="flex min-h-12 min-w-12 shrink-0 items-center justify-center rounded-full p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800 transition-colors"
                >
                  <X className="h-5 w-5" />
                </button>
              </div>

              {/* Modal Body */}
              <div className="flex-1 overflow-y-auto p-5">
                {/* STEP 1: PICK KIND */}
                {selectedKind === null ? (
                  <div className="space-y-3">
                    {/* Option 1: Blood Pressure & Pulse */}
                    <button
                      type="button"
                      onClick={() => setSelectedKind("bp")}
                      className="group w-full flex items-center justify-between p-4 rounded-2xl border-2 border-slate-100 hover:border-rose-400 bg-white hover:bg-rose-50/30 dark:bg-slate-800/60 dark:border-slate-700/80 dark:hover:border-rose-500/60 shadow-2xs hover:shadow-md transition-all text-start"
                    >
                      <div className="flex items-center gap-3.5">
                        <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl bg-rose-100 text-rose-600 dark:bg-rose-950/80 dark:text-rose-400 group-hover:scale-105 transition-transform">
                          <Heart className="h-6 w-6" />
                        </div>
                        <div>
                          <div className="flex items-center gap-2">
                            <span className="text-sm font-bold text-slate-900 dark:text-white">
                              {t.vitalBPTitle}
                            </span>
                            <span className="rounded-md bg-rose-100 px-1.5 py-0.5 text-[10px] font-bold text-rose-800 dark:bg-rose-950 dark:text-rose-300">
                              {locale === "ar" ? "مقياس الذراع" : "BP Cuff"}
                            </span>
                          </div>
                          <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5 leading-relaxed">
                            {t.vitalBPDesc}
                          </p>
                        </div>
                      </div>
                      <ChevronRight className="h-5 w-5 shrink-0 text-slate-400 group-hover:text-rose-600 transition-all rtl:rotate-180 group-hover:translate-x-0.5 rtl:group-hover:-translate-x-0.5" />
                    </button>

                    {/* Option 2: Blood Sugar */}
                    <button
                      type="button"
                      onClick={() => setSelectedKind("sugar")}
                      className="group w-full flex items-center justify-between p-4 rounded-2xl border-2 border-slate-100 hover:border-blue-400 bg-white hover:bg-blue-50/30 dark:bg-slate-800/60 dark:border-slate-700/80 dark:hover:border-blue-500/60 shadow-2xs hover:shadow-md transition-all text-start"
                    >
                      <div className="flex items-center gap-3.5">
                        <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl bg-blue-100 text-blue-600 dark:bg-blue-950/80 dark:text-blue-400 group-hover:scale-105 transition-transform">
                          <Droplet className="h-6 w-6 fill-current" />
                        </div>
                        <div>
                          <div className="flex items-center gap-2">
                            <span className="text-sm font-bold text-slate-900 dark:text-white">
                              {t.vitalSugarTitle}
                            </span>
                            <span className="rounded-md bg-blue-100 px-1.5 py-0.5 text-[10px] font-bold text-blue-800 dark:bg-blue-950 dark:text-blue-300">
                              mmol/L
                            </span>
                          </div>
                          <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5 leading-relaxed">
                            {t.vitalSugarDesc}
                          </p>
                        </div>
                      </div>
                      <ChevronRight className="h-5 w-5 shrink-0 text-slate-400 group-hover:text-blue-600 transition-all rtl:rotate-180 group-hover:translate-x-0.5 rtl:group-hover:-translate-x-0.5" />
                    </button>

                    {/* Option 3: Cognitive Alertness */}
                    <button
                      type="button"
                      onClick={() => setSelectedKind("alertness")}
                      className="group w-full flex items-center justify-between p-4 rounded-2xl border-2 border-slate-100 hover:border-purple-400 bg-white hover:bg-purple-50/30 dark:bg-slate-800/60 dark:border-slate-700/80 dark:hover:border-purple-500/60 shadow-2xs hover:shadow-md transition-all text-start"
                    >
                      <div className="flex items-center gap-3.5">
                        <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl bg-purple-100 text-purple-600 dark:bg-purple-950/80 dark:text-purple-400 group-hover:scale-105 transition-transform">
                          <Brain className="h-6 w-6" />
                        </div>
                        <div>
                          <div className="flex items-center gap-2">
                            <span className="text-sm font-bold text-slate-900 dark:text-white">
                              {t.vitalAlertnessTitle}
                            </span>
                            <span className="rounded-md bg-purple-100 px-1.5 py-0.5 text-[10px] font-bold text-purple-800 dark:bg-purple-950 dark:text-purple-300">
                              {locale === "ar" ? "تقييم عصبي" : "Neuro"}
                            </span>
                          </div>
                          <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5 leading-relaxed">
                            {t.vitalAlertnessDesc}
                          </p>
                        </div>
                      </div>
                      <ChevronRight className="h-5 w-5 shrink-0 text-slate-400 group-hover:text-purple-600 transition-all rtl:rotate-180 group-hover:translate-x-0.5 rtl:group-hover:-translate-x-0.5" />
                    </button>

                    {/* Option 4: Full Checkup */}
                    <button
                      type="button"
                      onClick={() => setSelectedKind("full")}
                      className="group w-full flex items-center justify-between p-4 rounded-2xl border-2 border-emerald-100 hover:border-emerald-400 bg-emerald-50/20 hover:bg-emerald-50/60 dark:bg-slate-800/60 dark:border-slate-700/80 dark:hover:border-emerald-500/60 shadow-2xs hover:shadow-md transition-all text-start"
                    >
                      <div className="flex items-center gap-3.5">
                        <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl bg-emerald-100 text-emerald-700 dark:bg-emerald-950/80 dark:text-emerald-400 group-hover:scale-105 transition-transform">
                          <Sparkles className="h-6 w-6" />
                        </div>
                        <div>
                          <div className="flex items-center gap-2">
                            <span className="text-sm font-bold text-slate-900 dark:text-white">
                              {t.vitalFullTitle}
                            </span>
                            <span className="rounded-md bg-emerald-100 px-1.5 py-0.5 text-[10px] font-bold text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300">
                              {locale === "ar" ? "شامل الكل" : "All-in-one"}
                            </span>
                          </div>
                          <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5 leading-relaxed">
                            {t.vitalFullDesc}
                          </p>
                        </div>
                      </div>
                      <ChevronRight className="h-5 w-5 shrink-0 text-slate-400 group-hover:text-emerald-600 transition-all rtl:rotate-180 group-hover:translate-x-0.5 rtl:group-hover:-translate-x-0.5" />
                    </button>
                  </div>
                ) : (
                  /* STEP 2: FOCUSED FORM FOR THE SELECTED VITAL */
                  <form onSubmit={handleSubmit} className="space-y-4">
                    {/* Switcher button */}
                    <div className="flex items-center justify-between pb-2 border-b border-slate-100 dark:border-slate-800">
                      <span className="text-xs font-semibold text-slate-500">
                        {locale === "ar" ? "النوع المختار:" : "Selected Type:"}
                      </span>
                      <button
                        type="button"
                        onClick={() => setSelectedKind(null)}
                        className="min-h-12 text-xs font-bold text-emerald-600 hover:text-emerald-700 dark:text-emerald-400 flex items-center gap-1"
                      >
                        <span>{t.changeVitalType}</span>
                        <ArrowLeft className="h-3 w-3 rtl:rotate-180" />
                      </button>
                    </div>

                    {/* BLOOD PRESSURE INPUTS */}
                    {(selectedKind === "bp" || selectedKind === "full") && (
                      <div className="rounded-2xl border border-rose-100 bg-rose-50/30 p-4 dark:border-rose-950/60 dark:bg-rose-950/15">
                        <label className="block text-xs font-bold text-slate-800 dark:text-slate-200 mb-2 flex items-center gap-1.5">
                          <Heart className="h-3.5 w-3.5 text-rose-500" />
                          <span>{locale === "ar" ? "قياس ضغط الدم (mmHg)" : "Blood Pressure (mmHg)"}</span>
                        </label>

                        <div className="grid grid-cols-2 gap-3 mb-3">
                          <div>
                            <span className="block text-[11px] font-semibold text-slate-500 mb-1">
                              {t.systolic}
                            </span>
                            <input
                              type="number"
                              inputMode="numeric"
                              pattern="[0-9]*"
                              enterKeyHint="next"
                              dir="ltr"
                              min="50"
                              max="300"
                              required
                              placeholder="120"
                              aria-label={t.systolic}
                              value={systolic}
                              onChange={(e) => setSystolic(e.target.value)}
                              className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-lg font-bold text-slate-900 focus:border-rose-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white shadow-xs"
                            />
                          </div>

                          <div>
                            <span className="block text-[11px] font-semibold text-slate-500 mb-1">
                              {t.diastolic}
                            </span>
                            <input
                              type="number"
                              inputMode="numeric"
                              pattern="[0-9]*"
                              enterKeyHint="next"
                              dir="ltr"
                              min="30"
                              max="200"
                              required
                              placeholder="80"
                              aria-label={t.diastolic}
                              value={diastolic}
                              onChange={(e) => setDiastolic(e.target.value)}
                              className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-lg font-bold text-slate-900 focus:border-rose-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white shadow-xs"
                            />
                          </div>
                        </div>

                        {/* Live BP Classification */}
                        {hasValidBP && (
                          <BloodPressureBadge systolic={numericSystolic} diastolic={numericDiastolic} showDetails={true} />
                        )}
                      </div>
                    )}

                    {/* PULSE INPUTS */}
                    {(selectedKind === "bp" || selectedKind === "full") && (
                      <div className="rounded-2xl border border-slate-100 bg-slate-50/70 p-4 dark:border-slate-800 dark:bg-slate-800/40">
                        <label className="block text-xs font-bold text-slate-800 dark:text-slate-200 mb-2 flex items-center gap-1.5">
                          <Activity className="h-3.5 w-3.5 text-emerald-500" />
                          <span>{locale === "ar" ? "معدل النبض (Pulse)" : "Heart Rate (Pulse)"}</span>
                        </label>
                        <div className="flex flex-wrap items-center gap-3">
                          <div className="relative min-w-32 flex-1">
                            <input
                              type="number"
                              inputMode="numeric"
                              pattern="[0-9]*"
                              enterKeyHint="done"
                              min="30"
                              max="220"
                              required
                              placeholder="75"
                              aria-label={t.pulse}
                              value={pulse}
                              onChange={(e) => setPulse(e.target.value)}
                              className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 pe-12 text-base font-bold text-slate-900 focus:border-emerald-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white shadow-xs"
                            />
                            <span className="absolute end-3 top-1/2 -translate-y-1/2 text-xs font-semibold text-slate-400">
                              bpm
                            </span>
                          </div>
                          <div className="flex items-center gap-1">
                            {[65, 72, 80, 88].map((pVal) => (
                              <button
                                type="button"
                                key={pVal}
                                onClick={() => setPulse(String(pVal))}
                                className="min-h-12 min-w-12 px-2 py-2 rounded-lg bg-white border border-slate-200 text-xs font-bold text-slate-700 hover:bg-emerald-50 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-300"
                              >
                                {pVal}
                              </button>
                            ))}
                          </div>
                        </div>
                      </div>
                    )}

                    {/* BLOOD SUGAR INPUTS */}
                    {(selectedKind === "sugar" || selectedKind === "full") && (
                      <div className="rounded-2xl border border-blue-100 bg-blue-50/40 p-4 dark:border-blue-950/60 dark:bg-blue-950/20">
                        <div className="flex flex-wrap items-center justify-between gap-2 mb-2">
                          <label className="text-xs font-bold text-blue-900 dark:text-blue-200 flex items-center gap-1.5">
                            <Droplet className="h-3.5 w-3.5 text-blue-600 fill-blue-600" />
                            <span>{locale === "ar" ? "فحص سكر الدم (Glucose)" : "Blood Glucose (Sugar)"}</span>
                          </label>

                          {/* Timing selector */}
                          <div className="flex items-center gap-1 text-xs">
                            {(["random", "fasting", "post_meal"] as BloodSugarTiming[]).map((tSlot) => {
                              const isSelected = sugarTiming === tSlot;
                              const labels = {
                                random: locale === "ar" ? "عشوائي" : "Random",
                                fasting: locale === "ar" ? "صائم" : "Fasting",
                                post_meal: locale === "ar" ? "بعد الأكل" : "Post-meal",
                              };
                              return (
                                <button
                                  type="button"
                                  key={tSlot}
                                  onClick={() => setSugarTiming(tSlot)}
                                  aria-pressed={isSelected}
                                  className={`min-h-12 px-2.5 py-1 rounded-md font-bold transition-all ${
                                    isSelected
                                      ? "bg-blue-600 text-white shadow-2xs"
                                      : "bg-white text-slate-600 hover:bg-slate-100 dark:bg-slate-800 dark:text-slate-300"
                                  }`}
                                >
                                  {labels[tSlot]}
                                </button>
                              );
                            })}
                          </div>
                        </div>

                        <div className="flex flex-wrap items-center gap-3 mb-3">
                          <div className="relative min-w-32 flex-1">
                            <input
                              type="number"
                              inputMode="decimal"
                              enterKeyHint="done"
                              aria-label={t.bloodSugar}
                              step="0.1"
                              min="1.0"
                              max="35.0"
                              required={selectedKind === "sugar"}
                              placeholder="e.g. 6.4"
                              value={bloodSugar}
                              onChange={(e) => setBloodSugar(e.target.value)}
                              className="w-full rounded-xl border border-blue-200 bg-white px-3 py-2 pe-16 text-lg font-bold text-slate-900 focus:border-blue-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white shadow-xs"
                            />
                            <span className="absolute end-3 top-1/2 -translate-y-1/2 text-xs font-semibold text-slate-400">
                              mmol/L
                            </span>
                          </div>

                          {/* Quick Presets */}
                          <div className="flex flex-wrap items-center gap-1">
                            {["5.5", "6.8", "7.5", "9.0", "11.0"].map((preset) => (
                              <button
                                type="button"
                                key={preset}
                                onClick={() => setBloodSugar(preset)}
                                className="min-h-12 min-w-12 px-2 py-2 rounded-lg bg-white border border-slate-200 text-xs font-bold text-slate-700 hover:bg-blue-50 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-300"
                              >
                                {preset}
                              </button>
                            ))}
                          </div>
                        </div>

                        {/* Live Sugar Classification Preview */}
                        {hasValidSugar && (
                          <BloodSugarBadge sugar={numericSugar} timing={sugarTiming} showDetails={true} />
                        )}
                      </div>
                    )}

                    {/* COGNITIVE ALERTNESS INPUTS */}
                    {(selectedKind === "alertness" || selectedKind === "full") && (
                      <div className="rounded-2xl border border-purple-100 bg-purple-50/30 p-4 dark:border-purple-950/60 dark:bg-purple-950/15">
                        <label className="block text-xs font-bold text-slate-800 dark:text-slate-200 mb-2 flex items-center gap-1.5">
                          <Brain className="h-3.5 w-3.5 text-purple-500" />
                          <span>{t.alertnessLevel}</span>
                        </label>
                        <p className="-mt-1 mb-2 text-xs text-slate-500 dark:text-slate-400">
                          {locale === "ar" ? "اختر فقط إذا لاحظت حالتها الآن. اتركه فارغاً إن لم تقيّمه." : "Choose only if you observed her now. Leave empty if not assessed."}
                        </p>

                        <div className="grid grid-cols-2 gap-2">
                          {(["alert", "confused", "restless", "drowsy"] as AlertnessLevel[]).map((level) => {
                            const meta = ALERTNESS_METAS[level];
                            const isSelected = alertness === level;
                            return (
                              <button
                                type="button"
                                key={level}
                                onClick={() => setAlertness(isSelected ? null : level)}
                                aria-pressed={isSelected}
                                className={`flex min-h-12 flex-col text-start p-2.5 rounded-xl border transition-all ${
                                  isSelected
                                    ? "border-purple-600 bg-purple-100/70 text-purple-900 dark:bg-purple-950/70 dark:text-purple-200 ring-2 ring-purple-500/20"
                                    : "border-slate-200 hover:border-slate-300 bg-white dark:bg-slate-800 dark:border-slate-700"
                                }`}
                              >
                                <span className="text-xs font-bold text-slate-900 dark:text-white">
                                  {locale === "ar" ? meta.labelAr : meta.labelEn}
                                </span>
                                <span className="text-[10px] text-slate-500 dark:text-slate-400 line-clamp-1 mt-0.5">
                                  {locale === "ar" ? meta.descAr : meta.descEn}
                                </span>
                              </button>
                            );
                          })}
                        </div>
                      </div>
                    )}

                    {/* CLINICAL NOTES */}
                    <div>
                      <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-1">
                        {t.notesOptional}
                      </label>
                      <textarea
                        rows={2}
                        value={notes}
                        onChange={(e) => setNotes(e.target.value)}
                        placeholder={
                          selectedKind === "sugar"
                            ? locale === "ar"
                              ? "ملاحظات السكر (جرعة الإنسولين، محتوى الوجبة)..."
                              : "Sugar notes (insulin dose, meal content)..."
                            : selectedKind === "bp"
                            ? locale === "ar"
                              ? "ملاحظات الضغط (صداع، قياس على الذراع الأيمن)..."
                              : "BP notes (headache, arm position)..."
                            : selectedKind === "alertness"
                            ? locale === "ar"
                              ? "ملاحظات استجابة الوالد ونطقه للكلمات وحركته..."
                              : "Alertness notes (speech clarity, movement, response)..."
                            : locale === "ar"
                            ? "أي ملاحظات إضافية..."
                            : "Any clinical observations..."
                        }
                        className="w-full rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-base text-slate-900 focus:border-emerald-500 focus:bg-white focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white resize-none"
                      />
                    </div>

                    {/* Actions */}
                    <div className="flex items-center justify-end gap-2 pt-2">
                      <button
                        type="button"
                        onClick={handleCloseModal}
                        className="min-h-12 rounded-xl px-4 py-2.5 text-xs font-semibold text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800"
                      >
                        {t.cancel}
                      </button>
                      <button
                        type="submit"
                        disabled={isSubmitting}
                        aria-busy={isSubmitting}
                        className="flex min-h-12 items-center gap-1.5 rounded-xl bg-emerald-600 hover:bg-emerald-700 px-5 py-2.5 text-xs font-bold text-white shadow-emerald-500/20 shadow-sm transition-all active:scale-95 disabled:opacity-50"
                      >
                        {isSubmitting ? (
                          <>
                            <Loader2 className="h-4 w-4 animate-spin" />
                            <span>{t.saving}</span>
                          </>
                        ) : (
                          <>
                            <CheckCircle2 className="h-4 w-4" />
                            <span>{t.saveVitals}</span>
                          </>
                        )}
                      </button>
                    </div>
                  </form>
                )}
              </div>
            </div>
          </div>
        )}

        {/* ========================================================================= */}
        {/* FILTER PILLS FOR HISTORY */}
        {/* ========================================================================= */}
        <div className="mb-4 flex items-center justify-between gap-2 overflow-x-auto pb-1">
          <div className="flex items-center gap-1.5 flex-wrap">
            <Filter className="h-3.5 w-3.5 text-slate-400" />
            <button
              type="button"
              onClick={() => setFilterMode("all")}
              aria-pressed={filterMode === "all"}
              className={`min-h-12 px-3 py-1 rounded-xl text-xs font-bold transition-all ${
                filterMode === "all"
                  ? "bg-slate-900 text-white dark:bg-emerald-600"
                  : "bg-white text-slate-600 border border-slate-200 hover:bg-slate-100 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-300"
              }`}
            >
              {locale === "ar" ? "جميع القراءات" : "All Readings"}
            </button>
            <button
              type="button"
              onClick={() => setFilterMode("bp")}
              aria-pressed={filterMode === "bp"}
              className={`flex items-center gap-1 min-h-12 px-3 py-1 rounded-xl text-xs font-bold transition-all ${
                filterMode === "bp"
                  ? "bg-rose-600 text-white"
                  : "bg-white text-rose-700 border border-rose-200 hover:bg-rose-50 dark:bg-slate-800 dark:border-rose-900 dark:text-rose-300"
              }`}
            >
              <Heart className="h-3 w-3 fill-current" />
              {locale === "ar" ? "ضغط ونبض" : "BP & Pulse"}
            </button>
            <button
              type="button"
              onClick={() => setFilterMode("sugar")}
              aria-pressed={filterMode === "sugar"}
              className={`flex items-center gap-1 min-h-12 px-3 py-1 rounded-xl text-xs font-bold transition-all ${
                filterMode === "sugar"
                  ? "bg-blue-600 text-white"
                  : "bg-white text-blue-700 border border-blue-200 hover:bg-blue-50 dark:bg-slate-800 dark:border-blue-900 dark:text-blue-300"
              }`}
            >
              <Droplet className="h-3 w-3 fill-current" />
              {locale === "ar" ? "سكر الدم" : "Sugar Only"}
            </button>
            <button
              type="button"
              onClick={() => setFilterMode("alerts")}
              aria-pressed={filterMode === "alerts"}
              className={`flex items-center gap-1 min-h-12 px-3 py-1 rounded-xl text-xs font-bold transition-all ${
                filterMode === "alerts"
                  ? "bg-amber-600 text-white"
                  : "bg-white text-amber-700 border border-amber-200 hover:bg-amber-50 dark:bg-slate-800 dark:border-amber-900 dark:text-amber-300"
              }`}
            >
              <AlertTriangle className="h-3 w-3" />
              {locale === "ar" ? "التنبيهات" : "Alerts"}
            </button>
          </div>

          <span className="text-[11px] font-semibold text-slate-400 shrink-0">
            {filteredVitals.length} {locale === "ar" ? "سجل" : "records"}
          </span>
        </div>

        {/* ========================================================================= */}
        {/* VITALS HISTORY LIST */}
        {/* ========================================================================= */}
        <div className="space-y-3">
          {loading ? (
            <div className="space-y-3">
              {[1, 2, 3].map((i) => (
                <div
                  key={i}
                  className="h-24 w-full animate-pulse rounded-2xl bg-slate-200 dark:bg-slate-800"
                />
              ))}
            </div>
          ) : filteredVitals.length === 0 ? (
            <div className="rounded-2xl border border-dashed border-slate-300 p-8 text-center dark:border-slate-800">
              <Activity className="mx-auto h-8 w-8 text-slate-400 mb-2" />
              <p className="text-sm font-semibold text-slate-600 dark:text-slate-300">
                {locale === "ar" ? "لا توجد قراءات مطابقة لهذا الفلتر" : "No readings match this filter"}
              </p>
            </div>
          ) : (
            filteredVitals.map((vital) => {
              const alertnessMeta = vital.alertness_level ? ALERTNESS_METAS[vital.alertness_level] : undefined;
              const dateStr = formatShortDate(vital.recorded_at, locale);
              const timeStr = formatTime(vital.recorded_at, locale);

              // `!= null` also rejects JSON null (not just a missing key)
              const hasBP = vital.systolic != null && vital.diastolic != null;
              const hasSugar = vital.blood_sugar != null && vital.blood_sugar > 0;
              const hasAlertness = vital.vital_type === "alertness" || (!hasBP && !hasSugar);

              // Badge indicator based on vital type
              const typeBadge =
                vital.vital_type === "sugar" || (!hasBP && hasSugar) ? (
                  <span className="rounded-md bg-blue-100 px-2 py-0.5 text-[10px] font-bold text-blue-800 dark:bg-blue-950 dark:text-blue-300 flex items-center gap-1">
                    <Droplet className="h-3 w-3 fill-current" />
                    <span>{locale === "ar" ? "فحص سكر" : "Glucose"}</span>
                  </span>
                ) : vital.vital_type === "bp" || (hasBP && !hasSugar) ? (
                  <span className="rounded-md bg-rose-100 px-2 py-0.5 text-[10px] font-bold text-rose-800 dark:bg-rose-950 dark:text-rose-300 flex items-center gap-1">
                    <Heart className="h-3 w-3" />
                    <span>{locale === "ar" ? "ضغط ونبض" : "BP & Pulse"}</span>
                  </span>
                ) : vital.vital_type === "alertness" || hasAlertness ? (
                  <span className="rounded-md bg-purple-100 px-2 py-0.5 text-[10px] font-bold text-purple-800 dark:bg-purple-950 dark:text-purple-300 flex items-center gap-1">
                    <Brain className="h-3 w-3" />
                    <span>{locale === "ar" ? "تقييم وعي" : "Alertness"}</span>
                  </span>
                ) : (
                  <span className="rounded-md bg-emerald-100 px-2 py-0.5 text-[10px] font-bold text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300 flex items-center gap-1">
                    <Sparkles className="h-3 w-3" />
                    <span>{locale === "ar" ? "فحص شامل" : "Full Check"}</span>
                  </span>
                );

              return (
                <div
                  key={vital.id}
                  className="rounded-2xl border border-slate-200 bg-white p-4 shadow-xs dark:border-slate-800 dark:bg-slate-900 transition-all hover:border-slate-300"
                >
                  {/* Top Bar: Type Badge & Date */}
                  <div className="flex items-center justify-between gap-3 mb-2.5">
                    <div className="flex items-center gap-2">
                      {typeBadge}
                    </div>

                    <div className="flex items-center gap-1 text-[11px] text-slate-400">
                      <Clock className="h-3 w-3" />
                      <Ltr>{dateStr} • {timeStr}</Ltr>
                    </div>
                  </div>

                  {/* Body Content according to recorded metrics */}
                  {/* 1. Blood Pressure if recorded */}
                  {hasBP && (
                    <div className="mb-2">
                      <BloodPressureBadge systolic={vital.systolic!} diastolic={vital.diastolic!} />
                    </div>
                  )}

                  {/* 2. Blood Sugar if recorded */}
                  {hasSugar && (
                    <div className="my-2 p-2.5 rounded-xl bg-blue-50/60 dark:bg-blue-950/20 border border-blue-100 dark:border-blue-950/50">
                      <BloodSugarBadge sugar={vital.blood_sugar!} showDetails={false} />
                      {vital.suspect_default_sugar && (
                        <p className="mt-1 text-xs font-semibold text-amber-700 dark:text-amber-400">
                          {locale === "ar" ? "غير مؤكد: ربما القيمة المعبأة مسبقاً في النموذج القديم" : "Unverified: may be the old form's pre-filled value"}
                        </p>
                      )}
                    </div>
                  )}

                  {/* 3. Bottom Row: Pulse, Alertness, and Caregiver Attribution */}
                  <div className="flex flex-wrap items-center gap-2 text-xs pt-2.5 border-t border-slate-100 dark:border-slate-800">
                    {vital.pulse != null && (
                      <span className="flex items-center gap-1 font-semibold text-slate-700 dark:text-slate-300">
                        <Heart className="h-3.5 w-3.5 text-rose-500" />
                        <Ltr>{vital.pulse} bpm</Ltr>
                      </span>
                    )}

                    {alertnessMeta && (
                      <span className={`px-2 py-0.5 rounded-md text-[11px] font-semibold ${alertnessMeta.badgeClass}`}>
                        {locale === "ar" ? alertnessMeta.labelAr : alertnessMeta.labelEn}
                      </span>
                    )}

                    <div className="ms-auto flex items-center gap-1 text-[11px] text-slate-500 dark:text-slate-400">
                      <span
                        className="flex h-4 w-4 items-center justify-center rounded-full text-[9px] text-white font-bold"
                        style={{ backgroundColor: vital.avatar_color || "#059669" }}
                      >
                        {vital.caregiver_name?.slice(0, 1)}
                      </span>
                      <span>{vital.caregiver_name}</span>
                    </div>
                  </div>

                  {vital.notes && (
                    <p className="mt-2 text-xs text-slate-600 dark:text-slate-400 bg-slate-50 dark:bg-slate-800/50 p-2.5 rounded-xl leading-relaxed">
                      {vital.notes}
                    </p>
                  )}
                </div>
              );
            })
          )}
        </div>
      </main>
    </div>
  );
}
