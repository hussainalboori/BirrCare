"use client";

import React, { useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { canManageMedications, canTakeActions } from "@/lib/permissions";
import { formatTime, parseServerTime } from "@/lib/datetime";
import type { VitalsLog } from "@/lib/types";
import { Ltr } from "./Ltr";
import { Pencil } from "lucide-react";

// Issue #15: a typo is corrected by a new reading; the original stays visible.

const REASONS = [
  { ar: "خطأ في الكتابة", en: "Typo" },
  { ar: "في الخانة الخطأ", en: "Wrong field" },
  { ar: "خطأ في الجهاز", en: "Device error" },
];
const WINDOW_MS = 24 * 3600_000;

function original(v: VitalsLog): string {
  const c = v.correction!;
  const parts: string[] = [];
  if (c.original_systolic != null) parts.push(`${c.original_systolic}/${c.original_diastolic}`);
  if (c.original_pulse != null) parts.push(`${c.original_pulse} bpm`);
  if (c.original_blood_sugar != null) parts.push(`${c.original_blood_sugar} mmol/L`);
  return parts.join(" · ");
}

/** "Edited by Fatima at 4:15 PM (original: ~~190/100~~) — Typo" */
export function CorrectionNote({ vital }: { vital: VitalsLog }) {
  const { locale } = useLanguage();
  if (!vital.correction) return null;
  const ar = locale === "ar";
  const c = vital.correction;
  const was = original(vital);
  return (
    <p data-testid="correction-note" className="mt-2 break-words text-xs text-amber-800 dark:text-amber-300">
      {ar ? "عدّلها " : "Edited by "}
      <bdi>{vital.caregiver_name}</bdi> <bdi dir="auto">{formatTime(c.corrected_at, locale)}</bdi>
      {was && (
        <>
          {" "}
          ({ar ? "الأصلي: " : "original: "}
          <Ltr className="line-through">{was}</Ltr>)
        </>
      )}
      {c.reason && <> — {c.reason}</>}
    </p>
  );
}

/** Button + inline form: the person who logged it (24 h) or an admin. */
export function CorrectVital({ vital, onDone }: { vital: VitalsLog; onDone: () => void }) {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const ar = locale === "ar";
  const [open, setOpen] = useState(false);
  const [sys, setSys] = useState(vital.systolic != null ? String(vital.systolic) : "");
  const [dia, setDia] = useState(vital.diastolic != null ? String(vital.diastolic) : "");
  const [pulse, setPulse] = useState(vital.pulse != null ? String(vital.pulse) : "");
  const [sugar, setSugar] = useState(vital.blood_sugar != null ? String(vital.blood_sugar) : "");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [openedAt] = useState(() => Date.now()); // read the clock once, not on every render

  const recorded = parseServerTime(vital.recorded_at as unknown as string);
  const own = !!currentCaregiver && vital.caregiver_id === currentCaregiver.id && !!recorded && openedAt - recorded.getTime() <= WINDOW_MS;
  const allowed = vital.vital_type !== "fast" && canTakeActions(currentCaregiver?.role) && (own || canManageMedications(currentCaregiver?.role));
  if (!allowed) return null;

  const hasBP = vital.systolic != null;
  const hasSugar = vital.blood_sugar != null;

  const save = async () => {
    setBusy(true);
    setError("");
    try {
      await fetchApi(`/vitals/${encodeURIComponent(vital.id)}/correct`, {
        method: "POST",
        body: JSON.stringify({
          vital_type: vital.vital_type,
          ...(hasBP ? { systolic: Number(sys), diastolic: Number(dia) } : {}),
          ...(pulse.trim() ? { pulse: Number(pulse) } : {}),
          ...(hasSugar && sugar.trim() ? { blood_sugar: Number(sugar) } : {}),
          ...(vital.alertness_level ? { alertness_level: vital.alertness_level } : {}),
          reason,
        }),
      });
      setOpen(false);
      onDone();
    } catch (err: unknown) {
      const code = (err as { data?: { error?: string } })?.data?.error;
      setError(
        code === "CORRECTION_NOT_ALLOWED"
          ? ar ? "مرّ أكثر من 24 ساعة: اطلب من المسؤول" : "More than 24 hours: ask an admin"
          : code === "ALREADY_CORRECTED"
          ? ar ? "تم تصحيحها من قبل" : "Already corrected"
          : ar ? "تحقق من القيم ثم حاول مجدداً" : "Check the values and try again"
      );
    } finally {
      setBusy(false);
    }
  };

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="flex min-h-12 items-center gap-1 rounded-xl px-2 text-sm font-semibold text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
      >
        <Pencil className="h-4 w-4" />
        {ar ? "تصحيح" : "Correct"}
      </button>
    );
  }

  const field = (label: string, value: string, set: (v: string) => void, decimal = false) => (
    <label className="text-xs font-semibold text-slate-600 dark:text-slate-300">
      {label}
      <input
        type="number"
        inputMode={decimal ? "decimal" : "numeric"}
        dir="ltr"
        value={value}
        onChange={(e) => set(e.target.value)}
        className="mt-1 block w-24 rounded-xl border border-slate-200 px-3 py-2 text-base dark:border-slate-700 dark:bg-slate-800"
      />
    </label>
  );

  return (
    <div className="mt-3 w-full space-y-2 rounded-xl border border-amber-300 bg-amber-50 p-3 dark:border-amber-800 dark:bg-amber-950/30">
      <p className="text-sm font-bold text-amber-900 dark:text-amber-200">
        {ar ? "تصحيح القراءة (تبقى القراءة الأصلية ظاهرة)" : "Correct the reading (the original stays visible)"}
      </p>
      <div className="flex flex-wrap gap-2">
        {hasBP && field(ar ? "الانقباضي" : "Systolic", sys, setSys)}
        {hasBP && field(ar ? "الانبساطي" : "Diastolic", dia, setDia)}
        {(hasBP || vital.pulse != null) && field(ar ? "النبض" : "Pulse", pulse, setPulse)}
        {hasSugar && field(ar ? "السكر" : "Sugar", sugar, setSugar, true)}
      </div>
      <div className="flex flex-wrap gap-2">
        {REASONS.map((r) => {
          const label = ar ? r.ar : r.en;
          return (
            <button
              key={r.en}
              type="button"
              aria-pressed={reason === label}
              onClick={() => setReason(label)}
              className={`min-h-12 rounded-xl border px-3 text-sm font-semibold ${
                reason === label ? "border-amber-700 bg-amber-700 text-white" : "border-amber-300 bg-white text-amber-900 dark:bg-slate-900 dark:text-amber-200"
              }`}
            >
              {label}
            </button>
          );
        })}
      </div>
      {error && <p role="alert" className="text-sm font-semibold text-rose-700 dark:text-rose-300">{error}</p>}
      <div className="flex gap-2">
        <button type="button" onClick={() => setOpen(false)} className="min-h-12 flex-1 rounded-xl border border-slate-300 text-sm font-semibold text-slate-700 dark:border-slate-700 dark:text-slate-300">
          {ar ? "إلغاء" : "Cancel"}
        </button>
        <button type="button" disabled={busy || !reason} onClick={save} className="min-h-12 flex-1 rounded-xl bg-amber-700 text-sm font-bold text-white disabled:opacity-50">
          {ar ? "حفظ التصحيح" : "Save correction"}
        </button>
      </div>
    </div>
  );
}
