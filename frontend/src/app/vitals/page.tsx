"use client";

import React, { useState, useEffect, useCallback } from "react";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { BloodPressureBadge } from "@/components/BloodPressureBadge";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLiveSync } from "@/hooks/useLiveSync";
import { VitalsLog, AlertnessLevel } from "@/lib/types";
import { ALERTNESS_METAS, classifyBloodPressure } from "@/lib/clinical";
import { fetchApi } from "@/lib/api";
import { Activity, Plus, Heart, Droplet, Brain, Clock, Loader2 } from "lucide-react";

export default function VitalsPage() {
  const { t, locale } = useLanguage();
  const { currentCaregiver, openPinModal } = useCaregiver();

  const [vitalsList, setVitalsList] = useState<VitalsLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [showForm, setShowForm] = useState(false);

  // Form state
  const [systolic, setSystolic] = useState<number>(120);
  const [diastolic, setDiastolic] = useState<number>(80);
  const [pulse, setPulse] = useState<number>(75);
  const [bloodSugar, setBloodSugar] = useState<string>("");
  const [alertness, setAlertness] = useState<AlertnessLevel>("alert");
  const [notes, setNotes] = useState<string>("");

  const loadVitals = useCallback(async () => {
    try {
      const data = await fetchApi<VitalsLog[]>("/vitals?limit=30");
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
      await fetchApi("/vitals", {
        method: "POST",
        body: JSON.stringify({
          systolic: Number(systolic),
          diastolic: Number(diastolic),
          pulse: Number(pulse),
          blood_sugar: bloodSugar ? parseFloat(bloodSugar) : undefined,
          alertness_level: alertness,
          notes: notes.trim() || undefined,
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

  const previewBP = classifyBloodPressure(systolic, diastolic);

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
                ? "متابعة ضغط الدم ومستوى الوعي الحركي والإدراكي"
                : "Tracking stroke recovery hemodynamics and alertness"}
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
            className="mb-8 rounded-3xl border border-slate-200 bg-white p-5 shadow-lg dark:border-slate-800 dark:bg-slate-900 animate-in fade-in slide-in-from-top-4 duration-200"
          >
            <h2 className="text-base font-bold text-slate-900 dark:text-white mb-4 flex items-center gap-2">
              <Heart className="h-4 w-4 text-rose-500" />
              <span>{t.recordVitals}</span>
            </h2>

            {/* Blood Pressure Inputs */}
            <div className="grid grid-cols-2 gap-3 mb-4">
              <div>
                <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-1">
                  {t.systolic}
                </label>
                <input
                  type="number"
                  min="50"
                  max="300"
                  required
                  value={systolic}
                  onChange={(e) => setSystolic(Number(e.target.value))}
                  className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-lg font-bold text-slate-900 focus:border-emerald-500 focus:bg-white focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-1">
                  {t.diastolic}
                </label>
                <input
                  type="number"
                  min="30"
                  max="200"
                  required
                  value={diastolic}
                  onChange={(e) => setDiastolic(Number(e.target.value))}
                  className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-lg font-bold text-slate-900 focus:border-emerald-500 focus:bg-white focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                />
              </div>
            </div>

            {/* Live BP Classification Preview */}
            <div className="mb-4">
              <BloodPressureBadge systolic={systolic} diastolic={diastolic} showDetails={true} />
            </div>

            {/* Pulse & Blood Sugar */}
            <div className="grid grid-cols-2 gap-3 mb-4">
              <div>
                <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-1 flex items-center gap-1">
                  <Activity className="h-3 w-3 text-emerald-500" />
                  <span>{t.pulse}</span>
                </label>
                <input
                  type="number"
                  min="30"
                  max="220"
                  required
                  value={pulse}
                  onChange={(e) => setPulse(Number(e.target.value))}
                  className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm font-bold text-slate-900 focus:border-emerald-500 focus:bg-white focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                />
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-1 flex items-center gap-1">
                  <Droplet className="h-3 w-3 text-blue-500" />
                  <span>{t.bloodSugar}</span>
                </label>
                <input
                  type="number"
                  step="0.1"
                  placeholder="Optional"
                  value={bloodSugar}
                  onChange={(e) => setBloodSugar(e.target.value)}
                  className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm font-bold text-slate-900 focus:border-emerald-500 focus:bg-white focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                />
              </div>
            </div>

            {/* Cognitive Alertness Taxonomy */}
            <div className="mb-4">
              <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-2 flex items-center gap-1">
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

            {/* Clinical Notes */}
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
                    ? "أي ملاحظات سريرية (مثل: بعد العلاج الطبيعي، أو صداع خفيف)..."
                    : "Clinical observations (e.g. after physical therapy, mild headache)..."
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

        {/* Vitals History List */}
        <div className="space-y-3">
          <h2 className="text-sm font-bold text-slate-700 dark:text-slate-300 uppercase tracking-wider">
            {t.vitalsHistory}
          </h2>

          {loading ? (
            <div className="space-y-3">
              {[1, 2, 3].map((i) => (
                <div
                  key={i}
                  className="h-20 w-full animate-pulse rounded-2xl bg-slate-200 dark:bg-slate-800"
                />
              ))}
            </div>
          ) : vitalsList.length === 0 ? (
            <div className="rounded-2xl border border-dashed border-slate-300 p-8 text-center dark:border-slate-800">
              <Activity className="mx-auto h-8 w-8 text-slate-400 mb-2" />
              <p className="text-sm font-semibold text-slate-600 dark:text-slate-300">
                {locale === "ar" ? "لم يتم تسجيل قراءات بعد" : "No vitals recorded yet"}
              </p>
            </div>
          ) : (
            vitalsList.map((vital) => {
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
                  className="rounded-2xl border border-slate-200 bg-white p-4 shadow-xs dark:border-slate-800 dark:bg-slate-900 transition-all"
                >
                  <div className="flex items-center justify-between gap-3 mb-2">
                    <BloodPressureBadge systolic={vital.systolic} diastolic={vital.diastolic} />

                    <div className="flex items-center gap-1 text-xs text-slate-500 dark:text-slate-400">
                      <Clock className="h-3.5 w-3.5" />
                      <span>{dateStr} • {timeStr}</span>
                    </div>
                  </div>

                  <div className="flex flex-wrap items-center gap-2 text-xs pt-2 border-t border-slate-100 dark:border-slate-800">
                    <span className="flex items-center gap-1 font-semibold text-slate-700 dark:text-slate-300">
                      <Heart className="h-3 w-3 text-rose-500" />
                      <span>{vital.pulse} bpm</span>
                    </span>

                    {vital.blood_sugar && (
                      <span className="flex items-center gap-1 font-semibold text-blue-600 dark:text-blue-400">
                        <Droplet className="h-3 w-3" />
                        <span>{vital.blood_sugar} mg/dL</span>
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
