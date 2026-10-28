"use client";

import React, { useCallback, useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { canTakeActions } from "@/lib/permissions";
import { householdToday } from "@/lib/householdTime";
import { formatShortDate } from "@/lib/datetime";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import { nightsText, type ShiftSummary, type Unavailability } from "@/lib/roster";
import type { SSEEvent } from "@/lib/types";
import { Moon } from "lucide-react";

// "This month" (issue #66): a thank-you for the nights each sibling covered, in
// name order so it never reads as a ranking; a gentle note when someone has had
// several nights in a row; and the dates people can't take a shift.

const inputClass =
  "mt-1 block min-h-12 w-full rounded-xl border border-slate-300 bg-white px-3 text-base text-slate-900 focus:bg-white dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100 dark:focus:bg-slate-950";

export function NightThanksCard() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const ar = locale === "ar";
  const [summary, setSummary] = useState<ShiftSummary | null>(null);
  const [away, setAway] = useState<Unavailability[]>([]);
  const [adding, setAdding] = useState(false);
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [reason, setReason] = useState("");
  const [msg, setMsg] = useState("");

  const load = useCallback(async () => {
    const [s, u] = await Promise.all([
      fetchApi<ShiftSummary>("/shifts/summary?days=30").catch(() => null),
      fetchApi<Unavailability[]>("/shifts/unavailable").catch(() => null),
    ]);
    if (s && Array.isArray(s.caregivers)) setSummary(s);
    if (Array.isArray(u)) setAway(u);
  }, []);
  useEffect(() => {
    load();
    const onEvent = (e: Event) => {
      const type = (e as CustomEvent<SSEEvent>).detail?.type;
      if (type === "DUTY_UPDATED" || type === "DUTY_PASSED") load();
    };
    window.addEventListener("birrcare:sse_event", onEvent);
    window.addEventListener(RESYNC_EVENT, load);
    return () => {
      window.removeEventListener("birrcare:sse_event", onEvent);
      window.removeEventListener(RESYNC_EVENT, load);
    };
  }, [load]);

  if (!summary) return null;
  const withNights = summary.caregivers.filter((c) => c.nights > 0);
  const tired = summary.caregivers.filter((c) => c.consecutive_nights >= 3);
  const canEdit = canTakeActions(currentCaregiver?.role);
  const day = (d: string) => formatShortDate(`${d}T12:00:00Z`, locale);

  const save = async () => {
    if (!from) return;
    setMsg("");
    try {
      await fetchApi("/shifts/unavailable", { method: "POST", body: JSON.stringify({ start_date: from, end_date: to || from, reason: reason.trim() }) });
      setAdding(false);
      setFrom("");
      setTo("");
      setReason("");
      load();
    } catch (e) {
      const status = (e as { status?: number }).status;
      setMsg(status ? (e as Error).message : ar ? "لم يُحفظ. تحقق من الاتصال وحاول مرة أخرى." : "Not saved. Check the connection and try again.");
    }
  };
  const remove = async (id: string) => {
    try {
      await fetchApi(`/shifts/unavailable/${encodeURIComponent(id)}`, { method: "DELETE" });
      load();
    } catch {
      setMsg(ar ? "لم يُحذف. حاول مرة أخرى." : "Not removed. Try again.");
    }
  };

  return (
    <section data-testid="night-thanks" className="mb-4 rounded-2xl border border-indigo-200 bg-indigo-50 p-4 text-indigo-950 dark:border-indigo-900 dark:bg-indigo-950/50 dark:text-indigo-50">
      <h2 className="flex items-center gap-2 text-base font-black">
        <Moon className="h-5 w-5 shrink-0" />
        {ar ? "شكراً على سهر هذا الشهر" : "Thank you for this month's nights"}
      </h2>
      {withNights.length > 0 ? (
        <p data-testid="night-thanks-list" className="mt-1 text-base font-bold">
          {withNights.map((c, i) => (
            <React.Fragment key={c.caregiver_id}>
              {i > 0 && " · "}
              <bdi>{c.name}</bdi>: {nightsText(c.nights, ar)}
            </React.Fragment>
          ))}
        </p>
      ) : (
        <p className="mt-1 text-sm font-semibold">{ar ? "لا ليالٍ في الجدول خلال آخر 30 يوماً." : "No nights on the roster in the last 30 days."}</p>
      )}
      <p className="mt-1 text-sm">{ar ? "آخر 30 يوماً، الليل من 10 مساءً إلى 7 صباحاً بتوقيت البيت." : "Last 30 days; nights are 10 pm to 7 am home time."}</p>
      {tired.map((c) => (
        <p key={c.caregiver_id} data-testid="night-rest-note" className="mt-2 rounded-xl bg-amber-100 p-2.5 text-sm font-bold text-amber-950 dark:bg-amber-950 dark:text-amber-50">
          {ar ? (
            <>
              سهرات <bdi>{c.name}</bdi>: {nightsText(c.consecutive_nights, true)} متتالية. شكراً — ليلة راحة ستفيد.
            </>
          ) : (
            <>
              <bdi>{c.name}</bdi> has done {nightsText(c.consecutive_nights, false)} in a row. Thank you — a night off would help.
            </>
          )}
        </p>
      ))}

      {/* Dates someone can't take a shift */}
      <div className="mt-3 border-t border-indigo-200 pt-3 dark:border-indigo-900">
        <p className="text-sm font-black">{ar ? "أيام لا يستطيع فيها أحد المناوبة" : "Days someone can't take a shift"}</p>
        {away.length === 0 && <p className="text-sm">{ar ? "لا شيء مسجّل." : "Nothing marked."}</p>}
        <ul className="mt-1 space-y-1">
          {away.map((u) => (
            <li key={u.id} data-testid="unavailable-row" className="flex flex-wrap items-center gap-2 text-sm">
              <span className="flex-1">
                <bdi className="font-bold">{u.name}</bdi>: <bdi>{day(u.start_date)}</bdi>
                {u.end_date !== u.start_date && (
                  <>
                    {" – "}
                    <bdi>{day(u.end_date)}</bdi>
                  </>
                )}
                {u.reason && (
                  <>
                    {" "}
                    (<bdi>{u.reason}</bdi>)
                  </>
                )}
              </span>
              {canEdit && u.caregiver_id === currentCaregiver?.id && (
                <button type="button" onClick={() => remove(u.id)} className="min-h-12 rounded-xl border border-indigo-300 bg-white px-3 text-sm font-bold text-indigo-900 dark:border-indigo-700 dark:bg-slate-900 dark:text-indigo-100">
                  {ar ? "حذف" : "Remove"}
                </button>
              )}
            </li>
          ))}
        </ul>
        {canEdit &&
          (adding ? (
            <div className="mt-2 space-y-2" data-testid="unavailable-form">
              <div className="grid grid-cols-2 gap-2">
                <label className="block text-sm font-semibold">
                  {ar ? "من" : "From"}
                  <input type="date" value={from} min={householdToday()} onChange={(e) => setFrom(e.target.value)} data-testid="unavailable-from" className={`${inputClass}`} />
                </label>
                <label className="block text-sm font-semibold">
                  {ar ? "إلى" : "To"}
                  <input type="date" value={to} min={from || householdToday()} onChange={(e) => setTo(e.target.value)} className={`${inputClass}`} />
                </label>
              </div>
              <label className="block text-sm font-semibold">
                {ar ? "السبب (اختياري)" : "Reason (optional)"}
                <input value={reason} onChange={(e) => setReason(e.target.value)} maxLength={120} placeholder={ar ? "سفر، امتحانات…" : "Travel, exams…"} className={`${inputClass}`} />
              </label>
              {msg && (
                <p role="alert" className="text-sm font-bold text-rose-700 dark:text-rose-300">
                  {msg}
                </p>
              )}
              <div className="grid grid-cols-2 gap-2">
                <button type="button" onClick={save} disabled={!from} data-testid="unavailable-save" className="min-h-12 rounded-xl bg-indigo-700 px-3 text-base font-bold text-white disabled:opacity-60">
                  {ar ? "حفظ" : "Save"}
                </button>
                <button type="button" onClick={() => setAdding(false)} className="min-h-12 rounded-xl border border-indigo-300 bg-white px-3 text-base font-bold text-indigo-900 dark:border-indigo-700 dark:bg-slate-900 dark:text-indigo-100">
                  {ar ? "إلغاء" : "Cancel"}
                </button>
              </div>
            </div>
          ) : (
            <button
              type="button"
              onClick={() => setAdding(true)}
              data-testid="unavailable-add"
              className="mt-2 min-h-12 w-full rounded-xl border border-indigo-300 bg-white px-3 text-base font-bold text-indigo-900 dark:border-indigo-700 dark:bg-slate-900 dark:text-indigo-100"
            >
              {ar ? "لا أستطيع المناوبة في أيام…" : "I can't take shifts on…"}
            </button>
          ))}
      </div>
    </section>
  );
}
