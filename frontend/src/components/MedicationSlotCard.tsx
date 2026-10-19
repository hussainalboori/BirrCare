"use client";

import React, { useState } from "react";
import { ScheduledDose } from "@/lib/types";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { isViewer } from "@/lib/permissions";
import { Check, Clock, AlertCircle, Loader2, Eye } from "lucide-react";

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
  const instructions = locale === "ar" ? medication.instructions_ar : medication.instructions_en;

  // Format timestamp for display
  const formattedTime = log?.taken_at
    ? new Date(log.taken_at).toLocaleTimeString(locale === "ar" ? "ar-SA" : "en-US", {
        hour: "numeric",
        minute: "2-digit",
        hour12: true,
      })
    : "";

  return (
    <div
      className={`relative overflow-hidden rounded-2xl border p-4 transition-all shadow-xs ${
        isTaken
          ? "border-emerald-200 bg-emerald-50/70 dark:border-emerald-900/60 dark:bg-emerald-950/25"
          : "border-slate-200 bg-white hover:border-slate-300 dark:border-slate-800 dark:bg-slate-900"
      }`}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex-1">
          {/* Slot Badge */}
          <div className="flex items-center gap-2 mb-1.5">
            <span className="inline-flex items-center gap-1 rounded-md bg-slate-100 px-2 py-0.5 text-[11px] font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-300">
              <Clock className="h-3 w-3" />
              {t.slots[slot as keyof typeof t.slots] || slot}
            </span>

            <span className="rounded-md bg-emerald-100/80 px-2 py-0.5 text-[11px] font-bold text-emerald-800 dark:bg-emerald-950/80 dark:text-emerald-300">
              {medication.dosage}
            </span>
          </div>

          {/* Medication Name */}
          <h4 className="text-base font-bold text-slate-900 dark:text-white">
            {medName}
          </h4>

          {/* Instructions */}
          {instructions && (
            <p className="mt-1 text-xs text-slate-500 dark:text-slate-400 leading-relaxed">
              {instructions}
            </p>
          )}
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
              className={`flex items-center gap-2 rounded-xl px-4 py-2.5 text-xs font-bold transition-all active:scale-95 shadow-sm ${
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
  );
}
