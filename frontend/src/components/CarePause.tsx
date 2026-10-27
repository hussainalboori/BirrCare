"use client";

import React, { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi, type ApiError } from "@/lib/api";
import { canTakeActions } from "@/lib/permissions";
import { formatShortDate, formatTime } from "@/lib/datetime";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import type { SSEEvent } from "@/lib/types";

// Hospital stay / away mode (issue #59). While care is paused the reminders stay
// quiet and the doctor report counts those doses as "paused", not missed.

export const OPEN_MED_CATALOG_EVENT = "birrcare:open_med_catalog";
const CHANGED_EVENT = "birrcare:care_pause_changed";

type Reason = "hospital" | "travel" | "other";
interface Pause {
  id: string;
  reason: Reason;
  started_at: string;
  started_by_name?: string;
  notes?: string;
}

const REASON: Record<Reason, [string, string]> = {
  hospital: ["In hospital", "في المستشفى"],
  travel: ["Away (travel)", "مسافرة"],
  other: ["Away", "خارج البيت"],
};

function usePause() {
  const { currentCaregiver } = useCaregiver();
  const [pause, setPause] = useState<Pause | null>(null);
  const load = useCallback(async () => {
    if (!currentCaregiver) return;
    const res = await fetchApi<{ paused: boolean; pause: Pause | null }>("/care/pause").catch(() => undefined);
    if (res) setPause(res.paused ? res.pause : null);
  }, [currentCaregiver]);
  useEffect(() => {
    load();
    const onEvent = (e: Event) => {
      const type = (e as CustomEvent<SSEEvent>).detail?.type;
      if (type === "CARE_PAUSED" || type === "CARE_RESUMED") load();
    };
    window.addEventListener("birrcare:sse_event", onEvent);
    window.addEventListener(RESYNC_EVENT, load);
    window.addEventListener(CHANGED_EVENT, load);
    return () => {
      window.removeEventListener("birrcare:sse_event", onEvent);
      window.removeEventListener(RESYNC_EVENT, load);
      window.removeEventListener(CHANGED_EVENT, load);
    };
  }, [load]);
  return pause;
}

/** On every page while care is paused: since when, and "Resume care at home". */
export function CarePauseBanner() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const ar = locale === "ar";
  const pause = usePause();
  const [resumed, setResumed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");

  const resume = async () => {
    setBusy(true);
    setMsg("");
    try {
      await fetchApi("/care/resume", { method: "POST" });
      setResumed(true);
      window.dispatchEvent(new Event(CHANGED_EVENT));
    } catch (e) {
      const err = e as ApiError;
      if (err.data?.error === "NOT_PAUSED") window.dispatchEvent(new Event(CHANGED_EVENT));
      else setMsg(ar ? "لم يُرسل. تحقق من الاتصال وحاول مرة أخرى." : "Not sent. Check the connection and try again.");
    } finally {
      setBusy(false);
    }
  };

  if (resumed) {
    // Back home: did anything change in hospital?
    return (
      <div role="dialog" aria-label={ar ? "العودة إلى البيت" : "Back home"} data-testid="care-resumed" className="border-b border-emerald-200 bg-emerald-50 px-4 py-3 text-emerald-950 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-50">
        <div className="mx-auto max-w-3xl space-y-2">
          <p className="text-base font-black">{ar ? "هل غيّر المستشفى أي دواء؟" : "Did the hospital change any medicines?"}</p>
          <div className="grid grid-cols-2 gap-2">
            <button
              type="button"
              onClick={() => {
                window.dispatchEvent(new Event(OPEN_MED_CATALOG_EVENT));
                setResumed(false);
              }}
              className="min-h-12 rounded-xl bg-emerald-700 px-3 text-base font-bold text-white"
            >
              {ar ? "نعم، راجع قائمة الأدوية" : "Yes, review the list"}
            </button>
            <button type="button" onClick={() => setResumed(false)} className="min-h-12 rounded-xl border-2 border-emerald-700 px-3 text-base font-bold text-emerald-900 dark:text-emerald-100">
              {ar ? "لا" : "No"}
            </button>
          </div>
          <p className="text-sm">{ar ? "راجع كل دواء: الجرعة والأوقات، وأوقف ما أوقفه المستشفى." : "Check each medicine: the dose and times, and stop any the hospital stopped."}</p>
          <div className="flex flex-wrap gap-2">
            <Link href="/handovers" className="inline-flex min-h-12 items-center rounded-xl bg-white px-3 text-base font-bold text-emerald-900 dark:bg-emerald-900 dark:text-emerald-50">
              {ar ? "اكتب ملاحظات الخروج" : "Write the discharge notes"}
            </Link>
            <Link href="/vitals?quick=bp" className="inline-flex min-h-12 items-center rounded-xl bg-white px-3 text-base font-bold text-emerald-900 dark:bg-emerald-900 dark:text-emerald-50">
              {ar ? "سجّل أول ضغط في البيت" : "Log the first BP at home"}
            </Link>
          </div>
        </div>
      </div>
    );
  }
  if (!pause) return null;
  const since = (
    <bdi>
      {formatShortDate(pause.started_at, locale)} {formatTime(pause.started_at, locale)}
    </bdi>
  );
  return (
    <div role="status" data-testid="care-paused-banner" className="border-b border-slate-300 bg-slate-100 px-4 py-2 text-slate-900 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100">
      <div className="mx-auto flex max-w-3xl flex-wrap items-center justify-between gap-2">
        <p className="text-base font-bold">
          {REASON[pause.reason]?.[ar ? 1 : 0] ?? REASON.other[ar ? 1 : 0]} {ar ? "منذ" : "since"} {since} — {ar ? "التذكيرات متوقفة" : "reminders paused"}
          {pause.notes && <span className="block text-sm font-semibold">{pause.notes}</span>}
        </p>
        {canTakeActions(currentCaregiver?.role) && (
          <button type="button" onClick={resume} disabled={busy} data-testid="resume-care" className="min-h-12 rounded-xl bg-emerald-700 px-4 text-base font-bold text-white disabled:opacity-60">
            {ar ? "استئناف الرعاية في البيت" : "Resume care at home"}
          </button>
        )}
        {msg && <p role="alert" className="w-full text-sm font-bold text-rose-700 dark:text-rose-300">{msg}</p>}
      </div>
    </div>
  );
}

/** On Home: start a hospital stay or time away. */
export function CarePauseStart() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const ar = locale === "ar";
  const pause = usePause();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState<Reason>("hospital");
  const [notes, setNotes] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  if (pause || !canTakeActions(currentCaregiver?.role)) return null;

  const start = async () => {
    setBusy(true);
    setMsg("");
    try {
      await fetchApi("/care/pause", { method: "POST", body: JSON.stringify({ reason, notes: notes.trim() }) });
      setOpen(false);
      setNotes("");
      window.dispatchEvent(new Event(CHANGED_EVENT));
    } catch (e) {
      const err = e as ApiError;
      if (err.data?.error === "PAUSE_OPEN") window.dispatchEvent(new Event(CHANGED_EVENT));
      else setMsg(ar ? "لم يُرسل. تحقق من الاتصال وحاول مرة أخرى." : "Not sent. Check the connection and try again.");
    } finally {
      setBusy(false);
    }
  };

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        data-testid="pause-care"
        className="mt-6 min-h-12 w-full rounded-xl border border-slate-300 bg-white px-4 text-base font-bold text-slate-700 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200"
      >
        {ar ? "ذهبت إلى المستشفى أو سافرت؟ أوقف التذكيرات" : "Going to hospital or away? Pause reminders"}
      </button>
    );
  }
  return (
    <div data-testid="pause-form" className="mt-6 space-y-3 rounded-2xl border border-slate-300 bg-white p-4 dark:border-slate-700 dark:bg-slate-900">
      <p className="text-base font-bold text-slate-900 dark:text-slate-100">{ar ? "أين هي؟" : "Where is she?"}</p>
      <div className="grid grid-cols-3 gap-2">
        {(Object.keys(REASON) as Reason[]).map((r) => (
          <button
            key={r}
            type="button"
            onClick={() => setReason(r)}
            aria-pressed={reason === r}
            className={`min-h-12 rounded-xl border-2 px-2 text-base font-bold ${
              reason === r ? "border-slate-800 bg-slate-800 text-white dark:border-slate-200 dark:bg-slate-200 dark:text-slate-900" : "border-slate-300 text-slate-800 dark:border-slate-600 dark:text-slate-100"
            }`}
          >
            {REASON[r][ar ? 1 : 0]}
          </button>
        ))}
      </div>
      <label className="block text-sm font-semibold text-slate-700 dark:text-slate-300">
        {ar ? "ملاحظة (اختياري): المستشفى، الجناح…" : "Note (optional): hospital, ward…"}
        <input
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          className="mt-1 w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 focus:bg-white dark:border-slate-600 dark:bg-slate-800 dark:text-slate-100 dark:focus:bg-slate-800"
        />
      </label>
      <p className="text-sm text-slate-600 dark:text-slate-300">
        {ar ? "تتوقف تذكيرات الأدوية والتقليب حتى تعود. يمكنك تسجيل الجرعات كالمعتاد." : "Medicine and repositioning reminders stop until she is back. You can still record doses."}
      </p>
      <div className="grid grid-cols-2 gap-2">
        <button type="button" onClick={start} disabled={busy} data-testid="confirm-pause" className="min-h-12 rounded-xl bg-slate-800 px-3 text-base font-bold text-white disabled:opacity-60 dark:bg-slate-200 dark:text-slate-900">
          {ar ? "أوقف التذكيرات" : "Pause reminders"}
        </button>
        <button type="button" onClick={() => setOpen(false)} className="min-h-12 rounded-xl border-2 border-slate-300 px-3 text-base font-bold text-slate-800 dark:border-slate-600 dark:text-slate-100">
          {ar ? "إلغاء" : "Cancel"}
        </button>
      </div>
      {msg && <p role="alert" className="text-sm font-bold text-rose-700 dark:text-rose-300">{msg}</p>}
    </div>
  );
}
