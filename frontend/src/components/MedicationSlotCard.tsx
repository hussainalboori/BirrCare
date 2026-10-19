"use client";

import React, { useState } from "react";
import { ScheduledDose } from "@/lib/types";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { isViewer } from "@/lib/permissions";
import { Check, Clock, AlertCircle, Loader2, Eye, ZoomIn, X, Pill } from "lucide-react";

interface MedicationSlotCardProps {
  dose: ScheduledDose;
  scheduledDate: string;
  onLoggedSuccess?: () => void;
}

export function MedicationSlotCard({ dose, scheduledDate, onLoggedSuccess }: MedicationSlotCardProps) {
  const { locale, t } = useLanguage();
  const { currentCaregiver, openPinModal } = useCaregiver();

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [conflictError, setConflictError] = useState<string | null>(null);
  const [isZoomOpen, setIsZoomOpen] = useState(false);

  const { medication, slot, log } = dose;
  const isTaken = log && log.status === "taken";

  const handleLogDose = async () => {
    if (isTaken || isSubmitting) return;

    if (!currentCaregiver) {
      openPinModal();
      return;
    }

    if (isViewer(currentCaregiver.role)) {
      alert(locale === "ar" ? "حسابك مخصص للمشاهدة فقط ولا يملك صلاحية تسجيل الجرعات" : "Viewer accounts cannot log doses");
      return;
    }

    setIsSubmitting(true);
    setConflictError(null);

    // Haptic feedback
    if (typeof window !== "undefined" && "vibrate" in navigator) {
      navigator.vibrate?.(40);
    }

    try {
      await fetchApi("/medications/log", {
        method: "POST",
        body: JSON.stringify({
          medication_id: medication.id,
          slot,
          scheduled_date: scheduledDate,
          status: "taken",
        }),
      });

      onLoggedSuccess?.();
    } catch (err: any) {
      // 409 Conflict: Double-Dosing Lock caught by backend!
      if (err.status === 409 || err.data?.error === "DOSE_ALREADY_LOGGED") {
        setConflictError(
          locale === "ar"
            ? `تنبيه: تم تسجيل الجرعة مسبقاً بواسطة ${err.data?.caregiver_name || "أحد الأخوة"}`
            : `Notice: Dose was already recorded by ${err.data?.caregiver_name || "a sibling"}`
        );
        onLoggedSuccess?.();
      } else {
        alert(err.message || "Failed to log dose");
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  const medName = locale === "ar" ? medication.name_ar : medication.name_en;
  const altName = locale === "ar" ? medication.name_en : medication.name_ar;
  const instructions = locale === "ar" ? medication.instructions_ar : medication.instructions_en;
  const pillImage = medication.image_url || "/meds/pill-default.svg";

  // Format timestamp for display
  const formattedTime = log?.taken_at
    ? new Date(log.taken_at).toLocaleTimeString(locale === "ar" ? "ar-SA" : "en-US", {
        hour: "numeric",
        minute: "2-digit",
        hour12: true,
      })
    : "";

  return (
    <>
      <div
        className={`relative overflow-hidden rounded-2xl border p-4 transition-all shadow-xs ${
          isTaken
            ? "border-emerald-200 bg-emerald-50/70 dark:border-emerald-900/60 dark:bg-emerald-950/25"
            : "border-slate-200 bg-white hover:border-slate-300 dark:border-slate-800 dark:bg-slate-900"
        }`}
      >
        <div className="flex items-start justify-between gap-3">
          {/* Pill Thumbnail & Info */}
          <div className="flex items-start gap-3 flex-1 min-w-0">
            {/* Clickable Pill Photo Thumbnail */}
            <button
              type="button"
              onClick={() => setIsZoomOpen(true)}
              title={locale === "ar" ? "اضغط لتكبير صورة الحبة والتحقق منها" : "Click to enlarge & verify pill appearance"}
              className="group relative shrink-0 h-13 w-13 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800/80 p-1 overflow-hidden transition-all hover:scale-105 active:scale-95 hover:border-emerald-400 dark:hover:border-emerald-500 shadow-2xs"
            >
              <img
                src={pillImage}
                alt={medName}
                className="h-full w-full object-contain"
                onError={(e) => {
                  (e.currentTarget as HTMLImageElement).src = "/meds/pill-default.svg";
                }}
              />
              <div className="absolute inset-0 bg-slate-900/20 opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center rounded-xl">
                <ZoomIn className="h-4 w-4 text-white drop-shadow" />
              </div>
            </button>

            <div className="flex-1 min-w-0">
              {/* Slot & Dosage Badges */}
              <div className="flex items-center gap-1.5 flex-wrap mb-1">
                <span className="inline-flex items-center gap-1 rounded-md bg-slate-100 px-2 py-0.5 text-[11px] font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                  <Clock className="h-3 w-3" />
                  {t.slots[slot as keyof typeof t.slots] || slot}
                </span>

                <span className="rounded-md bg-emerald-100/80 px-2 py-0.5 text-[11px] font-bold text-emerald-800 dark:bg-emerald-950/80 dark:text-emerald-300">
                  {medication.dosage}
                </span>
              </div>

              {/* Medication Name */}
              <h4 className="text-sm sm:text-base font-bold text-slate-900 dark:text-white truncate">
                {medName}
              </h4>
              {altName && altName !== medName && (
                <p className="text-[11px] text-slate-400 dark:text-slate-500 truncate -mt-0.5">
                  {altName}
                </p>
              )}

              {/* Instructions */}
              {instructions && (
                <p className="mt-1 text-xs text-slate-500 dark:text-slate-400 leading-relaxed">
                  {instructions}
                </p>
              )}
            </div>
          </div>

          {/* Action Button / Double-Dose Locked Badge */}
          <div className="shrink-0">
            {isTaken ? (
              <div className="flex flex-col items-end gap-1">
                <div className="flex items-center gap-1.5 rounded-full bg-emerald-600 px-3 py-1.5 text-xs font-bold text-white shadow-xs">
                  <Check className="h-3.5 w-3.5 stroke-[3]" />
                  <span>{t.statusTaken}</span>
                </div>
                <div className="flex items-center gap-1 text-[11px] font-medium text-emerald-700 dark:text-emerald-400">
                  <span
                    className="flex h-4 w-4 items-center justify-center rounded-full text-[9px] text-white font-bold"
                    style={{ backgroundColor: log.avatar_color || "#059669" }}
                  >
                    {log.caregiver_name?.slice(0, 1)}
                  </span>
                  <span>
                    {t.givenBy} {log.caregiver_name} {t.atTime} {formattedTime}
                  </span>
                </div>
              </div>
            ) : isViewer(currentCaregiver?.role) ? (
              <div className="flex items-center gap-1.5 rounded-xl border border-slate-200 bg-slate-50 dark:border-slate-800 dark:bg-slate-800/60 px-3 py-2 text-xs font-semibold text-slate-500 dark:text-slate-400">
                <Eye className="h-3.5 w-3.5 text-slate-400" />
                <span>{locale === "ar" ? "قيد الانتظار" : "Pending"}</span>
              </div>
            ) : (
              <button
                onClick={handleLogDose}
                disabled={isSubmitting}
                className={`flex items-center gap-2 rounded-xl px-3.5 sm:px-4 py-2.5 text-xs font-bold transition-all active:scale-95 shadow-sm ${
                  isSubmitting
                    ? "bg-slate-300 text-slate-600 cursor-not-allowed dark:bg-slate-700 dark:text-slate-300"
                    : "bg-emerald-600 hover:bg-emerald-700 text-white shadow-emerald-500/20"
                }`}
              >
                {isSubmitting ? (
                  <>
                    <Loader2 className="h-4 w-4 animate-spin" />
                    <span>{t.saving}</span>
                  </>
                ) : (
                  <>
                    <Check className="h-4 w-4 stroke-[2.5]" />
                    <span>{t.markTaken}</span>
                  </>
                )}
              </button>
            )}
          </div>
        </div>

        {/* Conflict banner if another sibling recorded the dose first */}
        {conflictError && (
          <div className="mt-3 flex items-center gap-2 rounded-xl bg-amber-500/15 p-2.5 text-xs font-semibold text-amber-700 dark:text-amber-400 border border-amber-500/30">
            <AlertCircle className="h-4 w-4 shrink-0 text-amber-600" />
            <span>{conflictError}</span>
          </div>
        )}
      </div>

      {/* Pill Zoom / Verification Lightbox Modal */}
      {isZoomOpen && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/70 backdrop-blur-xs p-4 animate-in fade-in duration-200"
          onClick={() => setIsZoomOpen(false)}
        >
          <div
            className="relative w-full max-w-sm rounded-3xl bg-white p-6 shadow-2xl dark:bg-slate-900 border border-slate-200 dark:border-slate-800 text-center animate-in zoom-in-95 duration-150"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Close Button */}
            <button
              onClick={() => setIsZoomOpen(false)}
              className="absolute top-4 end-4 rounded-full p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800"
            >
              <X className="h-5 w-5" />
            </button>

            {/* Pill Image */}
            <div className="mx-auto mb-4 flex h-44 w-44 items-center justify-center rounded-2xl bg-slate-50 dark:bg-slate-800/80 p-3 border border-slate-100 dark:border-slate-700 shadow-inner">
              <img
                src={pillImage}
                alt={medName}
                className="h-full w-full object-contain drop-shadow-md"
                onError={(e) => {
                  (e.currentTarget as HTMLImageElement).src = "/meds/pill-default.svg";
                }}
              />
            </div>

            {/* Badges */}
            <div className="flex items-center justify-center gap-2 mb-2">
              <span className="rounded-md bg-emerald-100 px-2.5 py-1 text-xs font-bold text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300">
                {medication.dosage}
              </span>
              <span className="rounded-md bg-slate-100 px-2.5 py-1 text-xs font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                {t.slots[slot as keyof typeof t.slots] || slot}
              </span>
            </div>

            {/* Title & Info */}
            <h3 className="text-lg font-black text-slate-900 dark:text-white">
              {medName}
            </h3>
            {altName && altName !== medName && (
              <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
                {altName}
              </p>
            )}

            {instructions && (
              <div className="mt-3 rounded-xl bg-slate-50 dark:bg-slate-800/60 p-2.5 text-xs text-slate-600 dark:text-slate-300 leading-relaxed border border-slate-100 dark:border-slate-700/60">
                <span className="font-bold text-slate-700 dark:text-slate-200 block mb-0.5">
                  {locale === "ar" ? "إرشادات الاستخدام:" : "Usage Instructions:"}
                </span>
                {instructions}
              </div>
            )}

            <button
              onClick={() => setIsZoomOpen(false)}
              className="mt-5 w-full rounded-2xl bg-emerald-600 hover:bg-emerald-700 py-3 text-xs font-bold text-white shadow-sm transition-all active:scale-95"
            >
              {locale === "ar" ? "إغلاق نافذة المعاينة" : "Close Preview"}
            </button>
          </div>
        </div>
      )}
    </>
  );
}
