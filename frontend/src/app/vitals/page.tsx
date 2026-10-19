"use client";

import React, { useState, useEffect, useCallback } from "react";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { BloodPressureBadge } from "@/components/BloodPressureBadge";
import { BloodSugarBadge } from "@/components/BloodSugarBadge";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLiveSync } from "@/hooks/useLiveSync";
import { VitalsLog, AlertnessLevel } from "@/lib/types";
import { ALERTNESS_METAS, classifyBloodPressure, classifyBloodSugar, BloodSugarTiming } from "@/lib/clinical";
import { fetchApi } from "@/lib/api";
import { Activity, Plus, Heart, Droplet, Brain, Clock, Loader2, Filter, AlertTriangle } from "lucide-react";

export default function VitalsPage() {
  const { t, locale } = useLanguage();
  const { currentCaregiver, openPinModal } = useCaregiver();

  const [vitalsList, setVitalsList] = useState<VitalsLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [filterMode, setFilterMode] = useState<"all" | "sugar" | "alerts">("all");

  // Form state
  const [systolic, setSystolic] = useState<number>(120);
  const [diastolic, setDiastolic] = useState<number>(80);
  const [pulse, setPulse] = useState<number>(75);
  const [bloodSugar, setBloodSugar] = useState<string>("115");
  const [sugarTiming, setSugarTiming] = useState<BloodSugarTiming>("random");
  const [alertness, setAlertness] = useState<AlertnessLevel>("alert");
  const [notes, setNotes] = useState<string>("");

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
  });

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!currentCaregiver) {
      openPinModal();
      return;
    }

    setIsSubmitting(true);
    try {
      const sugarVal = bloodSugar ? parseFloat(bloodSugar) : undefined;
      const combinedNotes = sugarTiming !== "random" && sugarVal
        ? `[${sugarTiming === "fasting" ? (locale === "ar" ? "صائم" : "Fasting") : (locale === "ar" ? "بعد الأكل" : "Post-meal")}] ${notes.trim()}`
        : notes.trim();

      await fetchApi("/vitals", {
        method: "POST",
        body: JSON.stringify({
          systolic: Number(systolic),
          diastolic: Number(diastolic),
          pulse: Number(pulse),
          blood_sugar: sugarVal,
          alertness_level: alertness,
          notes: combinedNotes || undefined,
        }),
      });

      setShowForm(false);
      setNotes("");
      loadVitals();
    } catch (err: any) {
      alert(err.message || "Failed to record vitals");
    } finally {
      setIsSubmitting(false);
    }
  };

  const numericSugar = parseFloat(bloodSugar);
  const hasValidSugar = !isNaN(numericSugar) && numericSugar > 0;

  // Filtered vitals
  const filteredVitals = vitalsList.filter((v) => {
    if (filterMode === "sugar") {
      return v.blood_sugar && v.blood_sugar > 0;
    }
    if (filterMode === "alerts") {
      const bpAlert = v.bp_status === "CRISIS" || v.bp_status === "STAGE_2";
      const sugarAlert = v.blood_sugar && (v.blood_sugar < 70 || v.blood_sugar >= 180);
      return bpAlert || sugarAlert;
    }
    return true;
  });

  return (
    <div className="min-h-screen">
      <CaregiverHeader connectionStatus={connectionStatus} />

      <main className="mx-auto max-w-3xl px-4 py-5 sm:px-6">
        {/* Header */}
        <div className="mb-6 flex items-center justify-between">
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

          <button
            onClick={() => setShowForm(!showForm)}
            className="flex items-center gap-1.5 rounded-xl bg-emerald-600 hover:bg-emerald-700 px-3.5 py-2 text-xs font-bold text-white shadow-emerald-500/20 shadow-sm transition-all active:scale-95"
          >
            <Plus className="h-4 w-4" />
            <span>{t.recordVitals}</span>
          </button>
        </div>

        {/* New Vitals Recording Form Drawer / Card */}
        {showForm && (
          <form
            onSubmit={handleSubmit}
            className="mb-8 rounded-3xl border border-slate-200 bg-white p-5 sm:p-6 shadow-xl dark:border-slate-800 dark:bg-slate-900 animate-in fade-in slide-in-from-top-4 duration-200"
          >
            <h2 className="text-base font-bold text-slate-900 dark:text-white mb-4 flex items-center gap-2">
              <Heart className="h-4 w-4 text-rose-500" />
              <span>{t.recordVitals}</span>
            </h2>

            {/* SECTION 1: Blood Pressure */}
            <div className="rounded-2xl border border-slate-100 bg-slate-50/70 p-4 mb-4 dark:border-slate-800 dark:bg-slate-800/40">
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
                    min="50"
                    max="300"
                    required
                    value={systolic}
                    onChange={(e) => setSystolic(Number(e.target.value))}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-lg font-bold text-slate-900 focus:border-emerald-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white shadow-xs"
                  />
                </div>

                <div>
                  <span className="block text-[11px] font-semibold text-slate-500 mb-1">
                    {t.diastolic}
                  </span>
                  <input
                    type="number"
                    min="30"
                    max="200"
                    required
                    value={diastolic}
                    onChange={(e) => setDiastolic(Number(e.target.value))}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-lg font-bold text-slate-900 focus:border-emerald-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white shadow-xs"
                  />
                </div>
              </div>

              {/* Live BP Classification Preview */}
              <BloodPressureBadge systolic={systolic} diastolic={diastolic} showDetails={true} />
            </div>

            {/* SECTION 2: Blood Sugar (First-Class Feature) */}
            <div className="rounded-2xl border border-blue-100 bg-blue-50/40 p-4 mb-4 dark:border-blue-950/60 dark:bg-blue-950/20">
              <div className="flex items-center justify-between mb-2">
                <label className="text-xs font-bold text-blue-900 dark:text-blue-200 flex items-center gap-1.5">
                  <Droplet className="h-3.5 w-3.5 text-blue-600 fill-blue-600" />
                  <span>{locale === "ar" ? "فحص سكر الدم (Glucose)" : "Blood Glucose (Sugar)"}</span>
                </label>

                {/* Timing selector */}
                <div className="flex items-center gap-1 text-[10px]">
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
                        className={`px-2 py-0.5 rounded-md font-bold transition-all ${
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

              <div className="flex items-center gap-3 mb-3">
                <div className="relative flex-1">
                  <input
                    type="number"
                    step="1"
                    min="20"
                    max="600"
                    placeholder="e.g. 115"
                    value={bloodSugar}
                    onChange={(e) => setBloodSugar(e.target.value)}
                    className="w-full rounded-xl border border-blue-200 bg-white px-3 py-2 text-lg font-bold text-slate-900 focus:border-blue-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white shadow-xs"
                  />
                  <span className="absolute end-3 top-1/2 -translate-y-1/2 text-xs font-semibold text-slate-400">
                    mg/dL
                  </span>
                </div>

                {/* Quick Presets */}
                <div className="flex items-center gap-1">
                  {["90", "110", "130", "160"].map((preset) => (
                    <button
                      type="button"
                      key={preset}
                      onClick={() => setBloodSugar(preset)}
                      className="px-2 py-2 rounded-lg bg-white border border-slate-200 text-xs font-bold text-slate-700 hover:bg-blue-50 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-300"
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

            {/* SECTION 3: Pulse & Heart Rate */}
            <div className="rounded-2xl border border-slate-100 bg-slate-50/70 p-4 mb-4 dark:border-slate-800 dark:bg-slate-800/40">
              <label className="block text-xs font-bold text-slate-800 dark:text-slate-200 mb-2 flex items-center gap-1.5">
                <Activity className="h-3.5 w-3.5 text-emerald-500" />
                <span>{locale === "ar" ? "معدل النبض (Pulse)" : "Heart Rate (Pulse)"}</span>
              </label>
              <div className="flex items-center gap-3">
                <div className="relative flex-1">
                  <input
                    type="number"
                    min="30"
                    max="220"
                    required
                    value={pulse}
                    onChange={(e) => setPulse(Number(e.target.value))}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm font-bold text-slate-900 focus:border-emerald-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white shadow-xs"
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
                      onClick={() => setPulse(pVal)}
                      className="px-2 py-2 rounded-lg bg-white border border-slate-200 text-xs font-bold text-slate-700 hover:bg-emerald-50 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-300"
                    >
                      {pVal}
                    </button>
                  ))}
                </div>
              </div>
            </div>

            {/* SECTION 4: Cognitive Alertness Taxonomy */}
            <div className="mb-4">
              <label className="block text-xs font-bold text-slate-800 dark:text-slate-200 mb-2 flex items-center gap-1.5">
                <Brain className="h-3.5 w-3.5 text-purple-500" />
                <span>{t.alertnessLevel}</span>
              </label>

              <div className="grid grid-cols-2 gap-2">
                {(["alert", "confused", "restless", "drowsy"] as AlertnessLevel[]).map((level) => {
                  const meta = ALERTNESS_METAS[level];
                  const isSelected = alertness === level;
                  return (
                    <button
                      type="button"
                      key={level}
                      onClick={() => setAlertness(level)}
                      className={`flex flex-col text-start p-2.5 rounded-xl border transition-all ${
                        isSelected
                          ? "border-emerald-600 bg-emerald-50 dark:bg-emerald-950/40 ring-2 ring-emerald-500/20"
                          : "border-slate-200 hover:border-slate-300 dark:border-slate-800"
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

            {/* SECTION 5: Clinical Notes */}
            <div className="mb-5">
              <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-1">
                {t.notesOptional}
              </label>
              <textarea
                rows={2}
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                placeholder={
                  locale === "ar"
                    ? "أي ملاحظات إضافية (أعراض صداع، جرعة الإنسولين، أو وقت الوجبة)..."
                    : "Clinical notes (headache symptoms, insulin dose, or meal time)..."
                }
                className="w-full rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-xs text-slate-900 focus:border-emerald-500 focus:bg-white focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white resize-none"
              />
            </div>

            {/* Buttons */}
            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setShowForm(false)}
                className="rounded-xl px-4 py-2 text-xs font-semibold text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800"
              >
                {t.cancel}
              </button>
              <button
                type="submit"
                disabled={isSubmitting}
                className="flex items-center gap-1.5 rounded-xl bg-emerald-600 hover:bg-emerald-700 px-5 py-2 text-xs font-bold text-white shadow-emerald-500/20 shadow-sm transition-all active:scale-95 disabled:opacity-50"
              >
                {isSubmitting ? (
                  <>
                    <Loader2 className="h-4 w-4 animate-spin" />
                    <span>{t.saving}</span>
                  </>
                ) : (
                  <span>{t.saveVitals}</span>
                )}
              </button>
            </div>
          </form>
        )}

        {/* Filter Pills for History */}
        <div className="mb-4 flex items-center justify-between gap-2 overflow-x-auto pb-1">
          <div className="flex items-center gap-1.5">
            <Filter className="h-3.5 w-3.5 text-slate-400" />
            <button
              onClick={() => setFilterMode("all")}
              className={`px-3 py-1 rounded-xl text-xs font-bold transition-all ${
                filterMode === "all"
                  ? "bg-slate-900 text-white dark:bg-emerald-600"
                  : "bg-white text-slate-600 border border-slate-200 hover:bg-slate-100 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-300"
              }`}
            >
              {locale === "ar" ? "جميع القراءات" : "All Readings"}
            </button>
            <button
              onClick={() => setFilterMode("sugar")}
              className={`flex items-center gap-1 px-3 py-1 rounded-xl text-xs font-bold transition-all ${
                filterMode === "sugar"
                  ? "bg-blue-600 text-white"
                  : "bg-white text-blue-700 border border-blue-200 hover:bg-blue-50 dark:bg-slate-800 dark:border-blue-900 dark:text-blue-300"
              }`}
            >
              <Droplet className="h-3 w-3 fill-current" />
              {locale === "ar" ? "فحوصات السكر فقط" : "Sugar Only"}
            </button>
            <button
              onClick={() => setFilterMode("alerts")}
              className={`flex items-center gap-1 px-3 py-1 rounded-xl text-xs font-bold transition-all ${
                filterMode === "alerts"
                  ? "bg-rose-600 text-white"
                  : "bg-white text-rose-700 border border-rose-200 hover:bg-rose-50 dark:bg-slate-800 dark:border-rose-900 dark:text-rose-300"
              }`}
            >
              <AlertTriangle className="h-3 w-3" />
              {locale === "ar" ? "التنبيهات والارتفاعات" : "Alerts Only"}
            </button>
          </div>

          <span className="text-[11px] font-semibold text-slate-400 shrink-0">
            {filteredVitals.length} {locale === "ar" ? "سجل" : "records"}
          </span>
        </div>

        {/* Vitals History List */}
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
              const alertnessMeta = ALERTNESS_METAS[vital.alertness_level];
              const dateStr = new Date(vital.recorded_at).toLocaleDateString(
                locale === "ar" ? "ar-SA" : "en-US",
                { month: "short", day: "numeric" }
              );
              const timeStr = new Date(vital.recorded_at).toLocaleTimeString(
                locale === "ar" ? "ar-SA" : "en-US",
                { hour: "numeric", minute: "2-digit", hour12: true }
              );

              return (
                <div
                  key={vital.id}
                  className="rounded-2xl border border-slate-200 bg-white p-4 shadow-xs dark:border-slate-800 dark:bg-slate-900 transition-all hover:border-slate-300"
                >
                  {/* Top Bar: BP and Date */}
                  <div className="flex items-start justify-between gap-3 mb-2">
                    <BloodPressureBadge systolic={vital.systolic} diastolic={vital.diastolic} />

                    <div className="flex items-center gap-1 text-[11px] text-slate-400">
                      <Clock className="h-3 w-3" />
                      <span>{dateStr} • {timeStr}</span>
                    </div>
                  </div>

                  {/* Middle: Prominent Blood Sugar Highlight if present */}
                  {vital.blood_sugar && vital.blood_sugar > 0 && (
                    <div className="my-2.5 p-2.5 rounded-xl bg-blue-50/60 dark:bg-blue-950/20 border border-blue-100 dark:border-blue-950/50">
                      <BloodSugarBadge sugar={vital.blood_sugar} showDetails={false} />
                    </div>
                  )}

                  {/* Bottom: Pulse, Alertness, and Caregiver Attribution */}
                  <div className="flex flex-wrap items-center gap-2 text-xs pt-2 border-t border-slate-100 dark:border-slate-800">
                    <span className="flex items-center gap-1 font-semibold text-slate-700 dark:text-slate-300">
                      <Heart className="h-3.5 w-3.5 text-rose-500" />
                      <span>{vital.pulse} bpm</span>
                    </span>

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
                    <p className="mt-2 text-xs text-slate-600 dark:text-slate-400 bg-slate-50 dark:bg-slate-800/50 p-2 rounded-xl">
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
