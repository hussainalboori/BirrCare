"use client";

import { MedPhotoGallery, medPhotoList } from "@/components/MedPhotoGallery";
import { haptic } from "@/lib/theme";
import React, { useEffect, useRef, useState } from "react";
import { MedicationLog, ScheduledDose } from "@/lib/types";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { canManageMedications, isViewer } from "@/lib/permissions";
import { formatDuration, formatShortDate, formatTime, lastGivenText, parseServerTime } from "@/lib/datetime";
import { clockDiffersFromHome, householdClock, householdDateString, householdDateTime, householdToday } from "@/lib/householdTime";
import { useHouseholdZone } from "@/context/HouseholdTimeContext";
import { Auto } from "./Ltr";
import { DoctorCallButton } from "./CrisisActions";
import { DOSE_COPY, VOMIT_MINUTES } from "@/lib/doseWarnings";
import { enqueue, isNetworkError, removeFromOutbox } from "@/lib/outbox";
import { useOutbox } from "./OutboxBar";
import { giveAs } from "@/lib/actor";
import { Check, Clock, AlertCircle, Loader2, Eye, ZoomIn, X, Pill, Lock, History, Undo2, Ban, SkipForward } from "lucide-react";

const UNDO_SECONDS = 10;
const OWN_CANCEL_MS = 10 * 60_000; // matches the backend's undoWindow

const CANCEL_REASONS: { ar: string; en: string }[] = [
  { ar: "ضغطت على البطاقة الخطأ", en: "Tapped the wrong card" },
  { ar: "لم تُعطَ فعلاً", en: "Not actually given" },
  { ar: "سُجّلت مرتين", en: "Recorded twice" },
];

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
  // Interval lock (issue #7): set from a 409 DOSE_TOO_SOON before the schedule re-fetch lands.
  const [tooSoon, setTooSoon] = useState<{ takenAt?: string; by?: string; nextAllowedAt?: string } | null>(null);
  const [askWhen, setAskWhen] = useState(false);
  const [overrideOpen, setOverrideOpen] = useState(false);
  const [overrideReason, setOverrideReason] = useState("");
  // Mis-tap recovery (issue #8): 10-second undo bar, later cancel with a reason, and skip.
  const [undo, setUndo] = useState<{ logId: string; until: number } | null>(null);
  const [undoLeft, setUndoLeft] = useState(0);
  const [voidedIds, setVoidedIds] = useState<string[]>([]);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [cancelReason, setCancelReason] = useState("");
  const [skipOpen, setSkipOpen] = useState(false);
  const [vomitOpen, setVomitOpen] = useState(false);
  // Re-render every 30 s so "4 h 15 min ago" stays true and the lock lifts on time.
  const [now, setNow] = useState(() => new Date());
  const zone = useHouseholdZone();
  const abroad = clockDiffersFromHome(now, zone);
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 30_000);
    return () => clearInterval(id);
  }, []);
  useEffect(() => {
    if (!undo) return;
    const tick = () => {
      const left = Math.ceil((undo.until - Date.now()) / 1000);
      if (left <= 0) setUndo(null);
      else setUndoLeft(left);
    };
    tick();
    const id = setInterval(tick, 250);
    return () => clearInterval(id);
  }, [undo]);

  const { medication, slot } = dose;
  // A just-cancelled entry stays hidden until the schedule re-fetch drops it.
  const serverLog = dose.log && !voidedIds.includes(dose.log.id) ? dose.log : undefined;
  const log = serverLog ?? (localLog && !voidedIds.includes(localLog.id) ? localLog : undefined);
  const isLogged = !!log; // taken OR skipped: either way the slot is locked server-side
  const isTaken = log?.status === "taken";
  // Saved on this phone but not sent yet (issue #16): amber, never green.
  const outbox = useOutbox();
  const queued = !log
    ? outbox.find((e) => e.kind === "dose" && e.dose?.medication_id === medication.id && e.dose.slot === slot && e.dose.scheduled_date === scheduledDate)
    : undefined;
  // Given = some medicine went in (taken or partial): holds the slot and the interval.
  const isGiven = isTaken || log?.status === "partial";
  const vomited = log?.events?.find((e) => e.type === "vomited");
  // Partial or vomited: the family must ask the doctor before any further dose.
  const needsDoctor = !!log && (log.status === "partial" || !!vomited);
  const ar = locale === "ar";
  const c = DOSE_COPY[locale];

  // The person who tapped may cancel within 10 minutes; an admin any time (with a reason).
  const tappedAt = parseServerTime(log?.recorded_at);
  const ownRecent = !!log && !!currentCaregiver && log.caregiver_id === currentCaregiver.id && !!tappedAt && now.getTime() - tappedAt.getTime() <= OWN_CANCEL_MS;
  const isAdmin = canManageMedications(currentCaregiver?.role);
  const canCancel = !!log && log.id !== "local" && (ownRecent || isAdmin);

  const lastTaken = tooSoon ? { taken_at: tooSoon.takenAt, caregiver_name: tooSoon.by } : dose.last_taken;
  const nextAllowed = parseServerTime(tooSoon?.nextAllowedAt ?? dose.next_allowed_at);
  const isIntervalLocked = !isLogged && !!nextAllowed && nextAllowed > now;
  const nextAllowedText = nextAllowed
    ? `${formatTime(nextAllowed.toISOString(), locale)}${
        householdDateString(nextAllowed, zone) !== householdDateString(now, zone) ? ` (${formatShortDate(nextAllowed.toISOString(), locale)})` : ""
      }`
    : "";

  const buildLocalLog = (data: {
    id?: string;
    caregiver_id?: string;
    caregiver_name?: string;
    avatar_color?: string;
    taken_at?: string;
    recorded_at?: string;
    status?: string;
    notes?: string;
  }): MedicationLog => ({
    id: data.id || "local",
    medication_id: medication.id,
    caregiver_id: data.caregiver_id || "",
    slot,
    scheduled_date: scheduledDate,
    taken_at: data.taken_at,
    recorded_at: data.recorded_at,
    status: data.status === "skipped" ? "skipped" : "taken",
    notes: data.notes,
    caregiver_name: data.caregiver_name,
    avatar_color: data.avatar_color,
  });

  const voidEntry = async (logId: string, reason: string) => {
    if (inFlightRef.current) return;
    inFlightRef.current = true;
    setIsSubmitting(true);
    try {
      await fetchApi(`/medications/log/${encodeURIComponent(logId)}/void`, {
        method: "POST",
        body: JSON.stringify({ reason }),
      });
      setVoidedIds((ids) => [...ids, logId]);
      setUndo(null);
      setCancelOpen(false);
      setCancelReason("");
      setConflictError(null);
      setTooSoon(null);
      onLoggedSuccess?.();
    } catch (err: any) {
      if (err.data?.error === "VOID_NOT_ALLOWED") {
        alert(ar ? "مرّت 10 دقائق: اطلب من المسؤول إلغاء التسجيل" : "More than 10 minutes have passed: ask an admin to cancel it");
      } else if (err.data?.error === "REASON_REQUIRED") {
        alert(ar ? "اكتب سبب الإلغاء" : "Write why it is being cancelled");
      } else if (err.data?.error === "ALREADY_VOIDED") {
        setVoidedIds((ids) => [...ids, logId]);
        setUndo(null);
        onLoggedSuccess?.();
      } else {
        alert(err.message || "Failed to cancel");
      }
    } finally {
      inFlightRef.current = false;
      setIsSubmitting(false);
    }
  };

  const recordVomit = async (logId: string, minutes: number | null) => {
    if (inFlightRef.current) return;
    inFlightRef.current = true;
    setIsSubmitting(true);
    try {
      const ev = await fetchApi<NonNullable<MedicationLog["events"]>[number]>(`/medications/log/${encodeURIComponent(logId)}/event`, {
        method: "POST",
        body: JSON.stringify({ type: "vomited", ...(minutes == null ? {} : { minutes_after: minutes }) }),
      });
      setLocalLog((l) => (l && l.id === logId ? { ...l, events: [...(l.events ?? []), ev] } : l));
      setVomitOpen(false);
      onLoggedSuccess?.();
    } catch (err: any) {
      if (err.data?.error === "EVENT_ALREADY_LOGGED") {
        setVomitOpen(false);
        onLoggedSuccess?.();
      } else {
        alert(err.message || "Failed to save");
      }
    } finally {
      inFlightRef.current = false;
      setIsSubmitting(false);
    }
  };

  const handleLogDose = async (opts: { givenAt?: string; override?: boolean; status?: MedicationLog["status"]; notes?: string } = {}) => {
    if (isLogged || inFlightRef.current) return;
    const status = opts.status ?? "taken";
    // Ticked well after its time: ask when it was really given, so a late tick of the
    // morning dose doesn't lock the evening dose until the next morning.
    if (status !== "skipped" && status !== "refused" && !opts.givenAt && !opts.override && !askWhen && scheduledAt && lateEnoughToAsk) {
      setAskWhen(true);
      return;
    }
    setAskWhen(false);
    setSkipOpen(false);

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
    haptic(40);

    const doseBody = {
      medication_id: medication.id,
      slot,
      scheduled_date: scheduledDate,
      status,
      ...(opts.notes ? { notes: opts.notes } : {}),
      ...(opts.givenAt ? { given_at: opts.givenAt } : {}),
      ...(opts.override ? { override_interval: true, override_reason: overrideReason.trim() } : {}),
    };
    try {
      const res = await fetchApi<{ log_id?: string; taken_at?: string; caregiver_name?: string; avatar_color?: string }>("/medications/log", {
        method: "POST",
        body: JSON.stringify(doseBody),
      });
      setOverrideOpen(false);
      setOverrideReason("");

      setLocalLog(
        buildLocalLog({
          id: res?.log_id,
          caregiver_id: currentCaregiver.id,
          taken_at: res?.taken_at || new Date().toISOString(),
          recorded_at: new Date().toISOString(),
          caregiver_name: res?.caregiver_name || currentCaregiver.name,
          avatar_color: res?.avatar_color || currentCaregiver.avatar_color,
          status,
          notes: opts.notes,
        })
      );
      if (res?.log_id) setUndo({ logId: res.log_id, until: Date.now() + UNDO_SECONDS * 1000 });
      onLoggedSuccess?.();
    } catch (err: any) {
      if (isNetworkError(err)) {
        // Offline (issue #16): keep it on this phone, honestly marked as not sent.
        // given_at = the moment of the tap, so a late send records the real time.
        await enqueue({
          kind: "dose",
          endpoint: "/medications/log",
          body: { ...doseBody, given_at: doseBody.given_at ?? new Date().toISOString() },
          caregiver_id: currentCaregiver.id,
          caregiver_name: currentCaregiver.name,
          label_ar: medication.name_ar,
          label_en: medication.name_en,
          dose: { medication_id: medication.id, slot, scheduled_date: scheduledDate },
        }).catch(() => {});
        setAskWhen(false);
        setSkipOpen(false);
      } else if (err.data?.error === "DOSE_TOO_SOON") {
        // Same medication given recently on another card (e.g. last night's bedtime dose).
        setTooSoon({ takenAt: err.data.last_taken_at, by: err.data.caregiver_name, nextAllowedAt: err.data.next_allowed_at });
        haptic([60, 50, 60]);
        onLoggedSuccess?.();
      } else if (err.status === 409 || err.data?.error === "DOSE_ALREADY_LOGGED") {
        // 409 Conflict: Double-Dosing Lock caught by backend!
        const who = err.data?.caregiver_name || (locale === "ar" ? "أحد الأخوة" : "a sibling");
        const when = formatTime(err.data?.taken_at, locale);
        // Payload: { caregiver_id, caregiver_name, avatar_color, status, log_id, taken_at (RFC3339;
        // older backends sent "YYYY-MM-DD HH:MM:SS" UTC — parseServerTime accepts both) }
        setLocalLog(
          buildLocalLog({
            id: err.data?.log_id,
            caregiver_id: err.data?.caregiver_id,
            caregiver_name: err.data?.caregiver_name,
            avatar_color: err.data?.avatar_color,
            taken_at: err.data?.taken_at,
            status: err.data?.status,
          })
        );
        setConflictError(`${t.doubleDoseAlert} ${t.givenBy} ${who}${when ? ` ${t.atTime} ${when}` : ""}`);
        haptic([60, 50, 60]);
        onLoggedSuccess?.();
      } else if (opts.override && (err.status === 403 || err.data?.error === "REASON_REQUIRED")) {
        alert(ar ? "اكتب سبب إعطاء الجرعة (مثلاً تعليمات الطبيب)" : "Write the reason for this dose (e.g. the doctor's instruction)");
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
  const photos = medPhotoList(medication.image_url, medication.photo_urls);

  // Format timestamp for display (handles RFC3339 and raw SQLite UTC strings)
  const formattedTime = formatTime(log?.taken_at, locale);
  const scheduledTime = dose.time || (medication.slot_times?.[slot] || "");

  const takenDate = parseServerTime(log?.taken_at);
  const elapsed = takenDate ? formatDuration(now.getTime() - takenDate.getTime(), locale) : "";
  const timeAgoText = takenDate && elapsed
    ? (locale === "ar"
        ? (elapsed === "أقل من دقيقة" ? "قبل قليل" : `منذ ${elapsed}`)
        : (elapsed === "less than a minute" ? "just now" : `${elapsed} ago`))
    : "";

  // This card's scheduled moment: the date and time at home (issue #58).
  const scheduledAt = scheduledTime ? householdDateTime(scheduledDate, scheduledTime, zone) : null;
  const sinceScheduled = scheduledAt ? now.getTime() - scheduledAt.getTime() : 0;
  // More than an hour late, and recent enough for the server to accept that time.
  const lateEnoughToAsk = sinceScheduled > 60 * 60_000 && sinceScheduled < 36 * 3600_000;

  // Overdue by the clock at home, not this phone's (a sibling abroad, issue #58).
  const isOverdue = (() => {
    if (isLogged || !scheduledTime) return false;
    if (scheduledDate !== householdToday(now, zone)) return false;
    const [sh, sm] = scheduledTime.split(":").map(Number);
    if (isNaN(sh) || isNaN(sm)) return false;
    const home = householdClock(now, zone);
    const currentMinutes = home.hours * 60 + home.minutes;
    const scheduledMinutes = sh * 60 + sm;
    return currentMinutes > scheduledMinutes + 30;
  })();

  return (
    <>
      <div
        className={`relative overflow-hidden rounded-2xl border p-4 transition-all shadow-xs ${
          needsDoctor
            ? "border-orange-300 bg-orange-50/70 dark:border-orange-900/60 dark:bg-orange-950/25"
            : isTaken
            ? "border-emerald-200 bg-emerald-50/70 dark:border-emerald-900/60 dark:bg-emerald-950/25"
            : "border-slate-200 bg-white hover:border-slate-300 dark:border-slate-800 dark:bg-slate-900"
        }`}
      >
        {/* On a phone the action goes under the name (full width), so a long
            name like "أبيكسابان (إليكويس)" is not squeezed into a narrow column. */}
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          {/* Pill Thumbnail & Info */}
          <div className="flex items-start gap-3 flex-1 min-w-0">
            {/* Clickable photo, big enough to see, with the word "Enlarge" under it */}
            <button
              type="button"
              onClick={() => setIsZoomOpen(true)}
              aria-label={locale === "ar" ? `تكبير صورة ${medName}` : `Enlarge ${medName} photo`}
              data-testid="med-photo-thumb"
              className="group relative flex w-18 shrink-0 flex-col items-center gap-1 rounded-xl border border-slate-200 bg-slate-50 p-1 shadow-2xs transition-all hover:border-emerald-400 active:scale-95 dark:border-slate-700 dark:bg-slate-800/80 dark:hover:border-emerald-500"
            >
              <span className="relative block h-16 w-full overflow-hidden rounded-lg">
                <img
                  src={pillImage}
                  alt=""
                  className="h-full w-full object-contain"
                  onError={(e) => {
                    (e.currentTarget as HTMLImageElement).src = "/meds/pill-default.svg";
                  }}
                />
                {photos.length > 1 && (
                  <span className="absolute bottom-0.5 end-0.5 rounded-md bg-slate-900/80 px-1 text-xs font-bold text-white" data-testid="med-photo-count">
                    {photos.length}
                  </span>
                )}
              </span>
              <span className="flex items-center gap-0.5 text-xs font-bold text-emerald-700 dark:text-emerald-400">
                <ZoomIn className="h-3.5 w-3.5" />
                {locale === "ar" ? "كبّر" : "Enlarge"}
              </span>
            </button>

            <div className="flex-1 min-w-0">
              {/* Slot & Dosage & Scheduled Time Badges */}
              <div className="flex items-center gap-2 flex-wrap mb-1.5">
                <span className="inline-flex items-center gap-1 rounded-md bg-slate-100 px-2.5 py-1 text-xs font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                  <Clock className="h-3.5 w-3.5" />
                  {t.slots[slot as keyof typeof t.slots] || slot}
                </span>

                {scheduledTime && (
                  <span className="inline-flex items-center gap-1 rounded-md bg-blue-50 px-2.5 py-1 text-xs font-bold text-blue-700 dark:bg-blue-950/80 dark:text-blue-300 border border-blue-200/60 dark:border-blue-800/60">
                    <span>⏰</span>
                    <span className="font-mono">{scheduledTime}</span>
                    {abroad && <span data-testid="home-time-label">{ar ? "بتوقيت البيت" : "home time"}</span>}
                  </span>
                )}

                <Auto className="inline-flex items-center rounded-md bg-emerald-100/80 px-2.5 py-1 text-base font-bold text-emerald-800 dark:bg-emerald-950/80 dark:text-emerald-300">
                  {medication.dosage}
                </Auto>

                {isOverdue && (
                  <span className="inline-flex items-center gap-1 rounded-md bg-rose-50 px-2.5 py-1 text-xs font-bold text-rose-700 dark:bg-rose-950/80 dark:text-rose-300 border border-rose-200 dark:border-rose-800 animate-pulse">
                    <AlertCircle className="h-3.5 w-3.5 text-rose-600" />
                    <span>{t.overdue}</span>
                  </span>
                )}
              </div>

              {/* Medication Name */}
              <h4 className="text-base sm:text-lg font-bold text-slate-900 dark:text-white break-words">
                {medName}
              </h4>
              {altName && altName !== medName && (
                <p className="text-xs text-slate-500 dark:text-slate-400 break-words -mt-0.5">
                  {altName}
                </p>
              )}

              {/* Instructions */}
              {instructions && (
                <p className="mt-1 text-xs sm:text-sm text-slate-500 dark:text-slate-400 leading-relaxed">
                  {instructions}
                </p>
              )}
            </div>
          </div>

          {/* Action Button / Double-Dose Locked Badge */}
          <div className="w-full sm:w-auto sm:shrink-0">
            {queued ? (
              <div role="status" data-testid="dose-queued" className="flex flex-col items-stretch gap-1 sm:max-w-44 sm:items-end">
                <span className="rounded-xl border border-amber-400 bg-amber-50 px-3 py-2 text-sm font-bold text-amber-900 dark:border-amber-700 dark:bg-amber-950/40 dark:text-amber-200">
                  {ar ? "محفوظة على هذا الهاتف — لم تُرسل بعد" : "Saved on this phone — not sent yet"}
                </span>
                <button
                  type="button"
                  onClick={() => removeFromOutbox(queued.id)}
                  className="min-h-12 rounded-xl px-2 text-sm font-semibold text-slate-600 dark:text-slate-300"
                >
                  {ar ? "إلغاء" : "Cancel"}
                </button>
              </div>
            ) : isLogged && log ? null : isIntervalLocked ? (
              <div
                role="status"
                className="flex min-h-12 items-center gap-1.5 rounded-xl border border-amber-300 bg-amber-50 px-3 py-2 text-sm font-bold text-amber-800 dark:border-amber-800 dark:bg-amber-950/50 dark:text-amber-300"
              >
                <Lock className="h-4 w-4 shrink-0" />
                <span>
                  {ar ? "بعد" : "After"} <Auto>{formatTime(nextAllowed!.toISOString(), locale)}</Auto>
                </span>
              </div>
            ) : isViewer(currentCaregiver?.role) ? (
              <div className="flex items-center gap-1.5 rounded-xl border border-slate-200 bg-slate-50 dark:border-slate-800 dark:bg-slate-800/60 px-3 py-2 text-xs font-semibold text-slate-500 dark:text-slate-400">
                <Eye className="h-3.5 w-3.5 text-slate-400" />
                <span>{locale === "ar" ? "قيد الانتظار" : "Pending"}</span>
              </div>
            ) : (
              <button
                type="button"
                onClick={() => handleLogDose()}
                disabled={isSubmitting}
                aria-busy={isSubmitting}
                className={`flex min-h-12 w-full items-center justify-center gap-2 rounded-xl px-4 py-2.5 text-sm font-bold transition-all active:scale-95 shadow-sm sm:w-auto ${
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
                    <span>{giveAs(currentCaregiver?.name, locale === "ar", t.markTaken)}</span>
                  </>
                )}
              </button>
            )}
            {!isLogged && !queued && !isViewer(currentCaregiver?.role) && !skipOpen && (
              <button
                type="button"
                onClick={() => {
                  setSkipOpen(true);
                  setAskWhen(false);
                }}
                className="mt-1 flex min-h-12 w-full items-center justify-center gap-1 rounded-xl px-2 text-sm font-semibold text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
              >
                <SkipForward className="h-4 w-4" />
                {c.notFullyGiven}
              </button>
            )}
          </div>
        </div>

        {/* Full-width status row when logged (Double-Dose Locked) */}
        {isLogged && log && (
          <div
            role="status"
            className={`mt-3 pt-3 border-t flex flex-wrap items-center justify-between gap-2.5 ${
              isTaken && !vomited
                ? "border-emerald-200/80 dark:border-emerald-900/50"
                : needsDoctor
                ? "border-orange-200/80 dark:border-orange-900/50"
                : "border-amber-200/80 dark:border-amber-900/50"
            }`}
          >
            <div className="flex items-center gap-2.5 flex-wrap">
              <div
                className={`inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-sm font-bold text-white shadow-xs ${
                  isTaken && !vomited ? "bg-emerald-600" : needsDoctor ? "bg-orange-600" : "bg-amber-600"
                }`}
              >
                {isTaken ? <Check className="h-4 w-4 stroke-[3]" /> : <AlertCircle className="h-4 w-4" />}
                <span>
                  {log.status === "partial"
                    ? c.badgePartial
                    : log.status === "refused"
                    ? c.badgeRefused
                    : isTaken
                    ? t.statusTaken
                    : t.statusSkipped}
                </span>
              </div>

              <div
                className={`inline-flex items-center gap-2 text-base font-semibold ${
                  isTaken ? "text-emerald-800 dark:text-emerald-300" : "text-amber-800 dark:text-amber-300"
                }`}
              >
                <span
                  className="flex h-6 w-6 items-center justify-center rounded-full text-xs text-white font-bold shrink-0"
                  style={{ backgroundColor: log.avatar_color || "#059669" }}
                >
                  {log.caregiver_name?.slice(0, 1)}
                </span>
                <span className="flex items-center gap-1.5 flex-wrap">
                  <span>{isGiven ? t.givenBy : ar ? "بواسطة" : "by"} <bdi>{log.caregiver_name}</bdi></span>
                  {formattedTime && (
                    <>
                      <span aria-hidden="true" className="opacity-60">·</span>
                      <Auto>{formattedTime}</Auto>
                    </>
                  )}
                  {timeAgoText && (
                    <>
                      <span aria-hidden="true" className="opacity-60">·</span>
                      <span className="text-sm font-normal text-slate-600 dark:text-slate-400">
                        {timeAgoText}
                      </span>
                    </>
                  )}
                </span>
              </div>
            </div>

            {/* Actions: vomited or cancel */}
            <div className="flex items-center gap-2 flex-wrap ms-auto">
              {isGiven && !vomited && !vomitOpen && log.id !== "local" && !isViewer(currentCaregiver?.role) && (
                <button
                  type="button"
                  onClick={() => setVomitOpen(true)}
                  className="flex min-h-12 items-center gap-1 rounded-xl px-2.5 py-1 text-sm font-semibold text-orange-700 dark:text-orange-400 hover:bg-orange-100/50 dark:hover:bg-orange-950/40"
                >
                  <AlertCircle className="h-4 w-4" />
                  {c.vomitedQuestion}
                </button>
              )}
              {canCancel && !cancelOpen && (
                <button
                  type="button"
                  onClick={() => setCancelOpen(true)}
                  className="flex min-h-12 items-center gap-1 rounded-xl px-2.5 py-1 text-sm font-semibold text-rose-700 underline-offset-2 hover:underline dark:text-rose-400 hover:bg-rose-100/50 dark:hover:bg-rose-950/40"
                >
                  <Ban className="h-4 w-4" />
                  {ar ? "إلغاء التسجيل" : "Cancel entry"}
                </button>
              )}
            </div>

            {!isTaken && log.notes && (
              <p className="w-full text-sm font-semibold text-amber-800 dark:text-amber-300 mt-1">
                {log.notes}
              </p>
            )}
          </div>
        )}

        {/* Partial or vomited: persistent warning, routed to the doctor (never a re-dose amount) */}
        {needsDoctor && log && (
          <div role="alert" className="mt-3 space-y-2 rounded-xl border-2 border-orange-400 bg-orange-50 p-3 dark:border-orange-800 dark:bg-orange-950/40">
            <p className="text-base font-bold leading-relaxed text-orange-900 dark:text-orange-200">
              {vomited ? c.warnVomited(vomited.minutes_after ?? null) : c.warnPartial}
            </p>
            <DoctorCallButton />
          </div>
        )}

        {/* Vomited after a given dose: a follow-up on that dose, not a new one */}
        {vomitOpen && log && !vomited && (
          <div className="mt-3 rounded-xl border border-orange-300 bg-orange-50 p-3 dark:border-orange-800 dark:bg-orange-950/40">
            <p className="mb-2 text-base font-semibold text-orange-900 dark:text-orange-200">{c.vomitedWhen}</p>
            <div className="grid grid-cols-3 gap-2">
              {[...VOMIT_MINUTES, null].map((m) => (
                <button
                  key={m ?? "unknown"}
                  type="button"
                  disabled={isSubmitting}
                  onClick={() => recordVomit(log.id, m)}
                  className="min-h-12 rounded-xl border border-orange-300 bg-white px-2 text-sm font-semibold text-orange-800 disabled:opacity-50 dark:border-orange-800 dark:bg-slate-900 dark:text-orange-300"
                >
                  {m == null ? c.dontKnow : c.minutes(m)}
                </button>
              ))}
            </div>
            <button
              type="button"
              onClick={() => setVomitOpen(false)}
              className="mt-2 min-h-12 w-full rounded-xl text-sm font-semibold text-slate-600 dark:text-slate-300"
            >
              {ar ? "رجوع" : "Back"}
            </button>
          </div>
        )}

        {/* Skip with a reason */}
        {skipOpen && !isLogged && (
          <div className="mt-3 rounded-xl border border-slate-200 bg-slate-50 p-3 dark:border-slate-700 dark:bg-slate-800/60">
            <p className="mb-2 text-base font-semibold text-slate-800 dark:text-slate-100">{c.whatHappened}</p>
            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                disabled={isSubmitting}
                onClick={() => handleLogDose({ status: "partial" })}
                className="min-h-12 rounded-xl border-2 border-orange-400 bg-white px-2 text-sm font-bold text-orange-800 disabled:opacity-50 dark:border-orange-800 dark:bg-slate-900 dark:text-orange-300"
              >
                {c.partial}
              </button>
              <button
                type="button"
                disabled={isSubmitting}
                onClick={() => handleLogDose({ status: "refused" })}
                className="min-h-12 rounded-xl border border-amber-300 bg-white px-2 text-sm font-semibold text-amber-800 disabled:opacity-50 dark:border-amber-800 dark:bg-slate-900 dark:text-amber-300"
              >
                {c.refused}
              </button>
              {c.skipReasons.map((r) => (
                <button
                  key={r}
                  type="button"
                  disabled={isSubmitting}
                  onClick={() => handleLogDose({ status: "skipped", notes: r })}
                  className="min-h-12 rounded-xl border border-amber-300 bg-white px-2 text-sm font-semibold text-amber-800 disabled:opacity-50 dark:border-amber-800 dark:bg-slate-900 dark:text-amber-300"
                >
                  {r}
                </button>
              ))}
            </div>
            <button
              type="button"
              onClick={() => setSkipOpen(false)}
              className="mt-2 min-h-12 w-full rounded-xl text-sm font-semibold text-slate-600 dark:text-slate-300"
            >
              {ar ? "رجوع" : "Back"}
            </button>
          </div>
        )}

        {/* Cancel a recorded entry */}
        {cancelOpen && log && (
          <div className="mt-3 rounded-xl border border-rose-200 bg-rose-50 p-3 dark:border-rose-900 dark:bg-rose-950/40">
            <p className="mb-2 text-base font-semibold text-rose-900 dark:text-rose-200">
              {ar ? "لماذا تُلغي هذا التسجيل؟ (يبقى في السجل)" : "Why cancel this entry? (it stays in the history)"}
            </p>
            <div className="flex flex-wrap gap-2">
              {CANCEL_REASONS.map((r) => (
                <button
                  key={r.en}
                  type="button"
                  onClick={() => setCancelReason(ar ? r.ar : r.en)}
                  className={`min-h-12 rounded-xl border px-3 text-sm font-semibold ${
                    cancelReason === (ar ? r.ar : r.en)
                      ? "border-rose-600 bg-rose-600 text-white"
                      : "border-rose-300 bg-white text-rose-800 dark:border-rose-800 dark:bg-slate-900 dark:text-rose-300"
                  }`}
                >
                  {ar ? r.ar : r.en}
                </button>
              ))}
            </div>
            <input
              type="text"
              value={cancelReason}
              onChange={(e) => setCancelReason(e.target.value)}
              maxLength={500}
              placeholder={ar ? "أو اكتب السبب" : "Or write the reason"}
              className="mt-2 w-full rounded-xl border border-rose-300 bg-white px-3 py-2 text-base text-slate-900 dark:border-rose-800 dark:bg-slate-900 dark:text-white"
            />
            <div className="mt-2 flex gap-2">
              <button
                type="button"
                onClick={() => {
                  setCancelOpen(false);
                  setCancelReason("");
                }}
                className="min-h-12 flex-1 rounded-xl border border-slate-300 text-sm font-semibold text-slate-600 dark:border-slate-700 dark:text-slate-300"
              >
                {ar ? "رجوع" : "Back"}
              </button>
              <button
                type="button"
                disabled={isSubmitting || (!ownRecent && !cancelReason.trim())}
                onClick={() => voidEntry(log.id, cancelReason.trim())}
                className="min-h-12 flex-1 rounded-xl bg-rose-600 text-sm font-bold text-white disabled:opacity-50"
              >
                {ar ? "إلغاء التسجيل" : "Cancel entry"}
              </button>
            </div>
          </div>
        )}

        {/* Cancelled entries stay visible as history */}
        {(dose.voided_logs ?? []).map((v) => (
          <p key={v.id} className="mt-2 text-sm text-slate-500 dark:text-slate-400">
            <span className="line-through">
              {v.status === "skipped" ? (ar ? "تخطي" : "Skipped") : ar ? "أُعطي" : "Given"} {ar ? "بواسطة" : "by"} <bdi>{v.caregiver_name}</bdi>{" "}
              <Auto>{formatTime(v.taken_at, locale)}</Auto>
            </span>
            {" — "}
            {ar ? "أُلغي بواسطة" : "cancelled by"} <bdi>{v.voided_by_name}</bdi> <Auto>{formatTime(v.voided_at, locale)}</Auto>
            {v.void_reason && <>: {v.void_reason}</>}
          </p>
        ))}

        {/* Last given (any date) and the interval lock */}
        {!isLogged && lastTaken?.taken_at && (
          <div
            className={`mt-3 rounded-xl px-3 py-2 text-base font-semibold ${
              isIntervalLocked
                ? "border border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200"
                : "bg-slate-50 text-slate-600 dark:bg-slate-800/60 dark:text-slate-300"
            }`}
          >
            <p className="flex items-start gap-2">
              <History className="mt-1 h-4 w-4 shrink-0" />
              <span>
                {lastGivenText(lastTaken.taken_at, lastTaken.caregiver_name, now, locale)}
                {isIntervalLocked && (
                  <>
                    {" — "}
                    {ar ? "لا تُعطِ جرعة أخرى قبل الساعة " : "do not give another before "}
                    <Auto>{nextAllowedText}</Auto>
                  </>
                )}
              </span>
            </p>

            {isIntervalLocked && canManageMedications(currentCaregiver?.role) && (
              <div className="mt-2">
                {overrideOpen ? (
                  <div className="space-y-2">
                    <label className="block text-sm font-semibold">
                      {ar ? "لماذا تُعطى الآن؟ (يُحفظ في السجل)" : "Why give it now? (saved in the audit log)"}
                      <textarea
                        value={overrideReason}
                        onChange={(e) => setOverrideReason(e.target.value)}
                        rows={4}
                        maxLength={500}
                        placeholder={ar ? "مثلاً: الطبيب طلب جرعة إضافية" : "e.g. the doctor ordered an extra dose"}
                        className="mt-1 w-full rounded-xl border border-amber-300 bg-white p-2 text-base font-normal text-slate-900 dark:border-amber-800 dark:bg-slate-900 dark:text-white"
                      />
                    </label>
                    <div className="flex gap-2">
                      <button
                        type="button"
                        onClick={() => setOverrideOpen(false)}
                        className="min-h-12 flex-1 rounded-xl border border-slate-300 text-sm font-semibold text-slate-600 dark:border-slate-700 dark:text-slate-300"
                      >
                        {ar ? "إلغاء" : "Cancel"}
                      </button>
                      <button
                        type="button"
                        disabled={!overrideReason.trim() || isSubmitting}
                        onClick={() => handleLogDose({ override: true })}
                        className="min-h-12 flex-1 rounded-xl bg-amber-600 text-sm font-bold text-white disabled:opacity-50"
                      >
                        {ar ? "أُعطيت بأمر الطبيب" : "Given on doctor's order"}
                      </button>
                    </div>
                  </div>
                ) : (
                  <button
                    type="button"
                    onClick={() => setOverrideOpen(true)}
                    className="min-h-12 rounded-xl border border-amber-400 px-3 text-sm font-semibold text-amber-800 dark:border-amber-700 dark:text-amber-300"
                  >
                    {ar ? "الطبيب طلب جرعة الآن (للمسؤول)" : "Doctor ordered a dose now (admin)"}
                  </button>
                )}
              </div>
            )}
          </div>
        )}

        {/* Late tick: when was it really given? */}
        {askWhen && !isLogged && scheduledAt && (
          <div className="mt-3 rounded-xl border border-blue-200 bg-blue-50 p-3 dark:border-blue-900 dark:bg-blue-950/40">
            <p className="mb-2 text-base font-semibold text-blue-900 dark:text-blue-200">
              {ar ? "متى أُعطيت هذه الجرعة؟" : "When was this dose given?"}
            </p>
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => handleLogDose({ givenAt: new Date().toISOString() })}
                className="min-h-12 flex-1 rounded-xl bg-emerald-600 px-3 text-sm font-bold text-white"
              >
                {ar ? "الآن" : "Just now"}
              </button>
              <button
                type="button"
                onClick={() => handleLogDose({ givenAt: scheduledAt.toISOString() })}
                className="min-h-12 flex-1 rounded-xl border border-emerald-600 px-3 text-sm font-bold text-emerald-700 dark:text-emerald-400"
              >
                {ar ? "في موعدها " : "On time, "}
                <Auto>{formatTime(scheduledAt.toISOString(), locale)}</Auto>
              </button>
              <button
                type="button"
                onClick={() => setAskWhen(false)}
                className="min-h-12 rounded-xl px-3 text-sm font-semibold text-slate-600 dark:text-slate-300"
              >
                {ar ? "إلغاء" : "Cancel"}
              </button>
            </div>
          </div>
        )}

        {/* Conflict banner if another sibling recorded the dose first */}
        {conflictError && (
          <div role="alert" className="mt-3 flex items-center gap-2 rounded-xl bg-amber-500/15 p-2.5 text-xs font-semibold text-amber-700 dark:text-amber-400 border border-amber-500/30">
            <AlertCircle className="h-4 w-4 shrink-0 text-amber-600" />
            <span>{conflictError}</span>
          </div>
        )}
      </div>

      {/* 10-second undo after a tap (sleepy mis-tap) */}
      {undo && (
        <div
          role="status"
          className="fixed inset-x-0 bottom-[calc(5.5rem+env(safe-area-inset-bottom))] z-40 mx-auto flex w-11/12 max-w-md items-center justify-between gap-3 rounded-2xl bg-slate-900 px-4 py-2 text-white shadow-xl"
        >
          <span className="text-base font-semibold">
            {ar ? "تم التسجيل" : "Recorded"} · <bdi>{medName}</bdi>
          </span>
          <button
            type="button"
            disabled={isSubmitting}
            onClick={() => voidEntry(undo.logId, "Undo")}
            className="flex min-h-12 shrink-0 items-center gap-1.5 rounded-xl bg-amber-400 px-4 text-base font-bold text-slate-900 disabled:opacity-50"
          >
            <Undo2 className="h-5 w-5" />
            {ar ? "تراجع" : "Undo"} ({undoLeft})
          </button>
        </div>
      )}

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

            {/* Every photo: the main one first, then the box, the back, the pill… */}
            <div className="pt-8">
              <MedPhotoGallery photos={photos} alt={medName} ar={locale === "ar"} />
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
