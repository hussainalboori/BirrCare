"use client";

import React, { useCallback, useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { isSuperAdmin } from "@/lib/permissions";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import { Ltr } from "./Ltr";
import { Sun } from "lucide-react";

// "Today" (issue #67): the ten-second answer to "how was she today?", for
// everyone including relatives with a view-only account. Each person can ask for
// it as an evening notification; the lock screen shows no values unless they
// turn on "show details on the lock screen".

interface DailySummary {
  date: string;
  headline: string;
  lines: string[];
  paused_since?: string;
}
interface SummaryPrefs {
  enabled: boolean;
  lock_details: boolean;
  time: string;
}

// Events that change what the card says.
const REFRESH = new Set(["MED_LOGGED", "VITAL_LOGGED", "NOTE_ADDED", "CARE_EVENT_LOGGED", "CARE_PAUSED", "CARE_RESUMED", "DUTY_UPDATED", "DUTY_PASSED"]);

export function TodaySummaryCard() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const ar = locale === "ar";
  const [summary, setSummary] = useState<DailySummary | null>(null);
  const [prefs, setPrefs] = useState<SummaryPrefs | null>(null);
  const [msg, setMsg] = useState("");

  const load = useCallback(async () => {
    const [s, p] = await Promise.all([
      fetchApi<DailySummary>(`/summary/today?lang=${locale}`).catch(() => null),
      fetchApi<SummaryPrefs>("/summary/prefs").catch(() => null),
    ]);
    if (s && Array.isArray(s.lines)) setSummary(s);
    if (p && typeof p.enabled === "boolean") setPrefs(p);
  }, [locale]);
  useEffect(() => {
    if (!currentCaregiver) return;
    load();
    const onEvent = (e: Event) => {
      if (REFRESH.has((e as CustomEvent<{ type?: string }>).detail?.type ?? "")) load();
    };
    window.addEventListener("birrcare:sse_event", onEvent);
    window.addEventListener(RESYNC_EVENT, load);
    return () => {
      window.removeEventListener("birrcare:sse_event", onEvent);
      window.removeEventListener(RESYNC_EVENT, load);
    };
  }, [currentCaregiver, load]);

  // Shown at once; put back if the server didn't take it.
  const save = async (change: Partial<SummaryPrefs>) => {
    setMsg("");
    const before = prefs;
    if (before) setPrefs({ ...before, ...change });
    try {
      const p = await fetchApi<SummaryPrefs>("/summary/prefs", { method: "PUT", body: JSON.stringify(change) });
      if (p) setPrefs(p);
    } catch {
      setPrefs(before);
      setMsg(ar ? "لم يُحفظ. تحقق من الاتصال وحاول مرة أخرى." : "Not saved. Check the connection and try again.");
    }
  };

  if (!summary) return null;
  const time = prefs?.time ?? "21:00";

  return (
    <section data-testid="today-summary" className="mb-4 rounded-2xl border border-emerald-200 bg-emerald-50 p-4 text-emerald-950 dark:border-emerald-900 dark:bg-emerald-950/50 dark:text-emerald-50">
      <h2 className="flex items-center gap-2 text-base font-black">
        <Sun className="h-5 w-5 shrink-0" />
        {ar ? "اليوم باختصار" : "Today in short"}
      </h2>
      <p data-testid="today-headline" className="mt-1 text-base font-bold">
        {summary.headline}
      </p>
      {summary.lines.length > 1 && (
        <ul data-testid="today-lines" className="mt-2 list-disc space-y-0.5 ps-5 text-sm">
          {summary.lines.map((l) => (
            <li key={l}>
              <bdi>{l}</bdi>
            </li>
          ))}
        </ul>
      )}

      {prefs && (
        <div className="mt-3 space-y-1 border-t border-emerald-200 pt-3 dark:border-emerald-900">
          <label className="flex min-h-12 items-center gap-3 text-sm font-semibold">
            <input type="checkbox" checked={prefs.enabled} onChange={(e) => save({ enabled: e.target.checked })} data-testid="summary-enabled" className="h-5 w-5" />
            <span>
              {ar ? "أرسل لي هذا الملخص كل مساء الساعة " : "Send me this summary every evening at "}
              <Ltr>{time}</Ltr> {ar ? "(بتوقيت البيت)" : "(home time)"}
            </span>
          </label>
          {prefs.enabled && (
            <label className="flex min-h-12 items-center gap-3 text-sm font-semibold">
              <input type="checkbox" checked={prefs.lock_details} onChange={(e) => save({ lock_details: e.target.checked })} data-testid="summary-lock-details" className="h-5 w-5" />
              {ar ? "اعرض التفاصيل على شاشة القفل (يراها أي أحد يرى هاتفك)" : "Show details on the lock screen (anyone who sees your phone can read them)"}
            </label>
          )}
          {isSuperAdmin(currentCaregiver?.role) && (
            <label className="flex min-h-12 items-center gap-3 text-sm font-semibold">
              {ar ? "وقت إرسال الملخص للجميع" : "Time the summary is sent to everyone"}
              <input
                type="time"
                value={time}
                onChange={(e) => e.target.value && save({ time: e.target.value })}
                data-testid="summary-time"
                className="min-h-12 rounded-xl border border-emerald-300 bg-white px-3 text-base text-slate-900 dark:border-emerald-800 dark:bg-slate-900 dark:text-slate-100"
              />
            </label>
          )}
          {msg && (
            <p role="alert" className="text-sm font-bold text-rose-700 dark:text-rose-300">
              {msg}
            </p>
          )}
        </div>
      )}
    </section>
  );
}
