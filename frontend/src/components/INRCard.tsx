"use client";

import React, { useCallback, useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { newRequestId } from "@/lib/outbox";
import { canTakeActions } from "@/lib/permissions";
import { formatShortDate } from "@/lib/datetime";
import { householdToday } from "@/lib/householdTime";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import type { SSEEvent } from "@/lib/types";
import { Ltr } from "./Ltr";

// INR quick entry (issue #62), for warfarin. The app shows the doctor's range and
// says to tell the doctor when a result is outside it; it never suggests a dose.

interface Lab {
  id: string;
  value: number;
  taken_on: string;
  next_test_on?: string;
  out_of_range: boolean;
}
interface LabList {
  results: Lab[];
  inr_target: { inr_target_low: number; inr_target_high: number };
}

export function INRCard() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const ar = locale === "ar";
  const [data, setData] = useState<LabList | null>(null);
  const [value, setValue] = useState("");
  const [nextTest, setNextTest] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [saved, setSaved] = useState<Lab | null>(null);

  const load = useCallback(async () => {
    const res = await fetchApi<LabList>("/labs?kind=inr&days=365").catch(() => null);
    if (res && Array.isArray(res.results)) setData(res); // offline or unexpected: keep what is shown
  }, []);
  useEffect(() => {
    load();
    const onEvent = (e: Event) => {
      if ((e as CustomEvent<SSEEvent>).detail?.type === "LAB_RESULT_LOGGED") load();
    };
    window.addEventListener("birrcare:sse_event", onEvent);
    window.addEventListener(RESYNC_EVENT, load);
    return () => {
      window.removeEventListener("birrcare:sse_event", onEvent);
      window.removeEventListener(RESYNC_EVENT, load);
    };
  }, [load]);

  const low = data?.inr_target?.inr_target_low ?? 2;
  const high = data?.inr_target?.inr_target_high ?? 3;
  const last = saved ?? data?.results[0] ?? null;
  const nextDate = data?.results.find((r) => r.next_test_on)?.next_test_on;
  const date = (d: string) => formatShortDate(`${d}T12:00:00Z`, locale);

  const save = async () => {
    const v = Number(value.replace(",", "."));
    if (!Number.isFinite(v) || v < 0.5 || v > 10) {
      setMsg(ar ? "اكتب رقم INR بين 0.5 و10." : "Enter an INR between 0.5 and 10.");
      return;
    }
    setBusy(true);
    setMsg("");
    try {
      const res = await fetchApi<Lab>("/labs", {
        method: "POST",
        body: JSON.stringify({ kind: "inr", value: v, taken_on: householdToday(), next_test_on: nextTest || undefined, client_request_id: newRequestId() }),
      });
      setSaved(res);
      setValue("");
      setNextTest("");
      load();
    } catch {
      setMsg(ar ? "لم يُحفظ. تحقق من الاتصال وحاول مرة أخرى." : "Not saved. Check the connection and try again.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <section data-testid="inr-card" className="mb-4 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <h2 className="text-base font-black text-slate-900 dark:text-slate-100">{ar ? "تحليل INR (الوارفارين)" : "INR test (warfarin)"}</h2>
      <p className="mt-1 text-sm text-slate-600 dark:text-slate-300">
        {ar ? "المدى الذي حدده الطبيب: " : "The doctor's range: "}
        <Ltr>
          {low}–{high}
        </Ltr>
      </p>
      {last && (
        <p
          data-testid="inr-last"
          className={`mt-2 rounded-xl p-3 text-base font-bold ${
            last.out_of_range ? "bg-amber-100 text-amber-950 dark:bg-amber-950 dark:text-amber-50" : "bg-emerald-50 text-emerald-900 dark:bg-emerald-950/50 dark:text-emerald-100"
          }`}
        >
          {ar ? "آخر نتيجة: " : "Last result: "}
          <Ltr>{last.value}</Ltr> (<bdi>{date(last.taken_on)}</bdi>)
          {last.out_of_range && (
            <span className="block">{ar ? "خارج المدى: أخبر الطبيب قبل الجرعة القادمة." : "Outside the range: tell the doctor before the next dose."}</span>
          )}
        </p>
      )}
      {nextDate && nextDate >= householdToday() && (
        <p className="mt-2 text-sm font-bold text-slate-800 dark:text-slate-100">
          {ar ? "موعد التحليل القادم: " : "Next test: "}
          <bdi>{date(nextDate)}</bdi>
        </p>
      )}
      {canTakeActions(currentCaregiver?.role) && (
        <div className="mt-3 grid grid-cols-2 gap-2">
          <label className="block text-sm font-semibold text-slate-700 dark:text-slate-300">
            {ar ? "نتيجة اليوم" : "Today's result"}
            <input
              value={value}
              onChange={(e) => setValue(e.target.value)}
              inputMode="decimal"
              dir="ltr"
              data-testid="inr-value"
              className="mt-1 w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 focus:bg-white dark:border-slate-600 dark:bg-slate-800 dark:text-slate-100 dark:focus:bg-slate-800"
            />
          </label>
          <label className="block text-sm font-semibold text-slate-700 dark:text-slate-300">
            {ar ? "التحليل القادم (اختياري)" : "Next test (optional)"}
            <input
              type="date"
              value={nextTest}
              onChange={(e) => setNextTest(e.target.value)}
              className="mt-1 w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 focus:bg-white dark:border-slate-600 dark:bg-slate-800 dark:text-slate-100 dark:focus:bg-slate-800"
            />
          </label>
          <button type="button" onClick={save} disabled={busy || !value.trim()} data-testid="inr-save" className="col-span-2 min-h-12 rounded-xl bg-emerald-700 px-4 text-base font-bold text-white disabled:opacity-60">
            {ar ? "احفظ نتيجة INR" : "Save INR result"}
          </button>
        </div>
      )}
      {msg && <p role="alert" className="mt-2 text-sm font-bold text-rose-700 dark:text-rose-300">{msg}</p>}
    </section>
  );
}
