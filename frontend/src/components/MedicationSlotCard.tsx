"use client";

import React, { useRef, useState } from "react";
import { MedicationLog, ScheduledDose } from "@/lib/types";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { isViewer } from "@/lib/permissions";
import { formatTime } from "@/lib/datetime";
import { Auto, Ltr } from "./Ltr";
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
  // Synchronous guard: state updates are async, so two fast taps could both pass an
  // `isSubmitting` check before re-render. A ref closes that window.
  const inFlightRef = useRef(false);
  const [conflictError, setConflictError] = useState<string | null>(null);
  const [isZoomOpen, setIsZoomOpen] = useState(false);
  // Optimistic/server-confirmed lock applied immediately after a 201 or 409, so the
  // button can't re-enable in the gap before the schedule re-fetch lands (or if it fails).
  const [localLog, setLocalLog] = useState<MedicationLog | null>(null);

  const { medication, slot } = dose;
  const log = dose.log ?? localLog ?? undefined;
  const isLogged = !!log; // taken OR skipped: either way the slot is locked server-side
  const isTaken = log?.status === "taken";

  const buildLocalLog = (data: { caregiver_name?: string; avatar_color?: string; taken_at?: string; status?: string }): MedicationLog => ({
    id: "local",
    medication_id: medication.id,
    caregiver_id: "",
    slot,
    scheduled_date: scheduledDate,
    taken_at: data.taken_at,
    status: data.status === "skipped" ? "skipped" : "taken",
    caregiver_name: data.caregiver_name,
    avatar_color: data.avatar_color,
  });

  const handleLogDose = async () => {
    if (isLogged || inFlightRef.current) return;

    if (!currentCaregiver) {
      openPinModal();
      return;
    }

    if (isViewer(currentCaregiver.role)) {
      alert(locale === "ar" ? "حسابك مخصص للمشاهدة فقط ولا يملك صلاحية تسجيل الجرعات" : "Viewer accounts cannot log doses");
      return;
    }

    inFlightRef.current = true;
    setIsSubmitting(true);
    setConflictError(null);

    // Haptic feedback
    if (typeof window !== "undefined" && "vibrate" in navigator) {
      navigator.vibrate?.(40);
    }

    try {
      const res = await fetchApi<{ taken_at?: string; caregiver_name?: string; avatar_color?: string }>("/medications/log", {
        method: "POST",
        body: JSON.stringify({
          medication_id: medication.id,
          slot,
          scheduled_date: scheduledDate,
          status: "taken",
        }),
      });

      setLocalLog(
        buildLocalLog({
          taken_at: res?.taken_at || new Date().toISOString(),
          caregiver_name: res?.caregiver_name || currentCaregiver.name,
          avatar_color: res?.avatar_color || currentCaregiver.avatar_color,
          status: "taken",
        })
      );
      onLoggedSuccess?.();
    } catch (err: any) {
      // 409 Conflict: Double-Dosing Lock caught by backend!
      if (err.status === 409 || err.data?.error === "DOSE_ALREADY_LOGGED") {
        const who = err.data?.caregiver_name || (locale === "ar" ? "أحد الأخوة" : "a sibling");
        const when = formatTime(err.data?.taken_at, locale);
        // Payload: { caregiver_id, caregiver_name, avatar_color, status, log_id, taken_at (RFC3339;
        // older backends sent "YYYY-MM-DD HH:MM:SS" UTC — parseServerTime accepts both) }
        setLocalLog(
          buildLocalLog({
            caregiver_name: err.data?.caregiver_name,
            avatar_color: err.data?.avatar_color,
            taken_at: err.data?.taken_at,
            status: err.data?.status,
          })
        );
        setConflictError(`${t.doubleDoseAlert} ${t.givenBy} ${who}${when ? ` ${t.atTime} ${when}` : ""}`);
        if (typeof navigator !== "undefined" && "vibrate" in navigator) {
          navigator.vibrate?.([60, 50, 60]);
        }
        onLoggedSuccess?.();
      } else {
        alert(err.message || "Failed to log dose");
      }
    } finally {
      inFlightRef.current = false;
      setIsSubmitting(false);
    }
  };

  const medName = locale === "ar" ? medication.name_ar : medication.name_en;
  const altName = locale === "ar" ? medication.name_en : medication.name_ar;
  const instructions = locale === "ar" ? medication.instructions_ar : medication.instructions_en;
  const pillImage = medication.image_url || "/meds/pill-default.svg";

  // Format timestamp for display (handles RFC3339 and raw SQLite UTC strings)
  const formattedTime = formatTime(log?.taken_at, locale);
  const scheduledTime = dose.time || (medication.slot_times?.[slot] || "");

  const isOverdue = (() => {
    if (isLogged || !scheduledTime) return false;
    const now = new Date();
    const todayStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
    if (scheduledDate !== todayStr) return false;
    const [sh, sm] = scheduledTime.split(":").map(Number);
    if (isNaN(sh) || isNaN(sm)) return false;
    const currentMinutes = now.getHours() * 60 + now.getMinutes();
    const scheduledMinutes = sh * 60 + sm;
    return currentMinutes > scheduledMinutes + 30;
  })();

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
              aria-label={locale === "ar" ? `تكبير صورة ${medName}` : `Enlarge ${medName} photo`}
              className="group relative shrink-0 h-13 w-13 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-800/80 p-1 overflow-hidden transition-all hover:scale-105 active:scale-95 hover:border-emerald-400 dark:hover:border-emerald-500 shadow-2xs"
            >
              <img
                src={pillImage}
                alt=""
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
              {/* Slot & Dosage & Scheduled Time Badges */}
              <div className="flex items-center gap-1.5 flex-wrap mb-1">
                <span className="inline-flex items-center gap-1 rounded-md bg-slate-100 px-2 py-0.5 text-[11px] font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                  <Clock className="h-3 w-3" />
                  {t.slots[slot as keyof typeof t.slots] || slot}
                </span>

                {scheduledTime && (
                  <span className="inline-flex items-center gap-1 rounded-md bg-blue-50 px-2 py-0.5 text-[11px] font-bold text-blue-700 dark:bg-blue-950/80 dark:text-blue-300 border border-blue-200/60 dark:border-blue-800/60">
                    <span className="text-[10px]">⏰</span>
                    <span className="font-mono">{scheduledTime}</span>
                  </span>
                )}

                <Auto className="rounded-md bg-emerald-100/80 px-2 py-0.5 text-[11px] font-bold text-emerald-800 dark:bg-emerald-950/80 dark:text-emerald-300">
                  {medication.dosage}
                </Auto>

                {isOverdue && (
                  <span className="inline-flex items-center gap-1 rounded-md bg-rose-50 px-2 py-0.5 text-[10px] font-bold text-rose-700 dark:bg-rose-950/80 dark:text-rose-300 border border-rose-200 dark:border-rose-800 animate-pulse">
                    <AlertCircle className="h-3 w-3 text-rose-600" />
                    <span>{t.overdue}</span>
                  </span>
                )}
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
            {isLogged && log ? (
              <div className="flex flex-col items-end gap-1" role="status">
                <div
                  className={`flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-bold text-white shadow-xs ${
                    isTaken ? "bg-emerald-600" : "bg-amber-600"
                  }`}
                >
                  {isTaken ? <Check className="h-3.5 w-3.5 stroke-[3]" /> : <AlertCircle className="h-3.5 w-3.5" />}
                  <span>{isTaken ? t.statusTaken : t.statusSkipped}</span>
                </div>
                <div
                  className={`flex items-center gap-1 text-[11px] font-medium ${
                    isTaken ? "text-emerald-700 dark:text-emerald-400" : "text-amber-700 dark:text-amber-400"
                  }`}
                >
                  <span
                    className="flex h-4 w-4 items-center justify-center rounded-full text-[9px] text-white font-bold"
                    style={{ backgroundColor: log.avatar_color || "#059669" }}
                  >
                    {log.caregiver_name?.slice(0, 1)}
                  </span>
                  <span>
                    {t.givenBy} <bdi>{log.caregiver_name}</bdi>
                    {formattedTime && (
                      <>
                        {" "}
                        {t.atTime} <Ltr>{formattedTime}</Ltr>
                      </>
                    )}
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
                type="button"
                onClick={handleLogDose}
                disabled={isSubmitting}
                aria-busy={isSubmitting}
                className={`flex min-h-12 items-center gap-2 rounded-xl px-3.5 sm:px-4 py-2.5 text-xs font-bold transition-all active:scale-95 shadow-sm ${
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
          <div role="alert" className="mt-3 flex items-center gap-2 rounded-xl bg-amber-500/15 p-2.5 text-xs font-semibold text-amber-700 dark:text-amber-400 border border-amber-500/30">
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
            role="dialog"
            aria-modal="true"
            className="relative w-full max-w-sm rounded-3xl bg-white p-6 shadow-2xl dark:bg-slate-900 border border-slate-200 dark:border-slate-800 text-center animate-in zoom-in-95 duration-150"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Close Button */}
            <button
              type="button"
              onClick={() => setIsZoomOpen(false)}
              aria-label={locale === "ar" ? "إغلاق" : "Close"}
              className="absolute top-3 end-3 flex min-h-12 min-w-12 items-center justify-center rounded-full p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800"
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
              <Auto className="rounded-md bg-emerald-100 px-2.5 py-1 text-xs font-bold text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300">
                {medication.dosage}
              </Auto>
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
              className="mt-5 min-h-12 w-full rounded-2xl bg-emerald-600 hover:bg-emerald-700 py-3 text-xs font-bold text-white shadow-sm transition-all active:scale-95"
            >
              {locale === "ar" ? "إغلاق نافذة المعاينة" : "Close Preview"}
            </button>
          </div>
        </div>
      )}
    </>
  );
}
