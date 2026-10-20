"use client";

import React, { useCallback, useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import { formatTime } from "@/lib/datetime";
import {
  flushOutbox,
  listOutbox,
  removeFromOutbox,
  OUTBOX_EVENT,
  OUTBOX_RESULT_EVENT,
  type OutboxEntry,
  type OutboxOutcome,
} from "@/lib/outbox";
import { CloudOff, RotateCw, X } from "lucide-react";

/** Live view of this phone's outbox (issue #16). */
export function useOutbox(): OutboxEntry[] {
  const [entries, setEntries] = useState<OutboxEntry[]>([]);
  useEffect(() => {
    let alive = true;
    const load = () => listOutbox().then((e) => alive && setEntries(e)).catch(() => {});
    load();
    window.addEventListener(OUTBOX_EVENT, load);
    return () => {
      alive = false;
      window.removeEventListener(OUTBOX_EVENT, load);
    };
  }, []);
  return entries;
}

/**
 * "1 unsent action — Retry". Sends automatically when the phone is back online,
 * becomes visible or the live stream reconnects; stale doses ask first.
 */
export function OutboxBar() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const entries = useOutbox();
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const ar = locale === "ar";
  const me = currentCaregiver?.id;

  const flush = useCallback(
    async (confirmed: string[] = []) => {
      setBusy(true);
      await flushOutbox(me, confirmed).catch(() => {});
      setBusy(false);
    },
    [me]
  );

  useEffect(() => {
    if (!me) return;
    flush();
    const onVisible = () => document.visibilityState === "visible" && flush();
    const onOnline = () => flush();
    window.addEventListener("online", onOnline);
    window.addEventListener(RESYNC_EVENT, onOnline);
    document.addEventListener("visibilitychange", onVisible);
    const timer = setInterval(() => flush(), 20_000);
    return () => {
      window.removeEventListener("online", onOnline);
      window.removeEventListener(RESYNC_EVENT, onOnline);
      document.removeEventListener("visibilitychange", onVisible);
      clearInterval(timer);
    };
  }, [me, flush]);

  // Say plainly when something could not be sent after all.
  useEffect(() => {
    const onResult = (e: Event) => {
      const r = (e as CustomEvent<OutboxOutcome>).detail;
      if (r.outcome === "sent") return;
      const label = ar ? r.entry.label_ar : r.entry.label_en;
      const who = String(r.data?.caregiver_name ?? "");
      setProblem(
        r.outcome === "conflict"
          ? ar ? `لم تُرسل «${label}»: سجّلها ${who} قبل ذلك.` : `“${label}” not sent: ${who} had already logged it.`
          : r.outcome === "too_soon"
          ? ar ? `لم تُرسل «${label}»: أُعطيت جرعة قبل وقت قصير.` : `“${label}” not sent: a dose was given too recently.`
          : ar ? `تعذّر إرسال «${label}». سجّلها مرة أخرى.` : `Could not send “${label}”. Please log it again.`
      );
    };
    window.addEventListener(OUTBOX_RESULT_EVENT, onResult);
    return () => window.removeEventListener(OUTBOX_RESULT_EVENT, onResult);
  }, [ar]);

  const mine = entries.filter((e) => e.caregiver_id === me);
  const others = entries.filter((e) => e.caregiver_id !== me);
  const stale = mine.filter((e) => e.needs_confirm);

  // The bar floats over the page, so pad the page by its height: the last
  // card's buttons must stay reachable above it.
  const barRef = useCallback((el: HTMLDivElement | null) => {
    const root = document.documentElement;
    if (!el) return;
    const ro = new ResizeObserver(() => root.style.setProperty("--outbox-h", `${el.offsetHeight + 8}px`));
    ro.observe(el);
    return () => {
      ro.disconnect();
      root.style.removeProperty("--outbox-h");
    };
  }, []);

  if (!mine.length && !others.length && !problem) return null;

  return (
    <div
      ref={barRef}
      role="status"
      data-testid="outbox-bar"
      className="fixed inset-x-0 bottom-[calc(5.5rem+env(safe-area-inset-bottom))] z-40 mx-auto w-11/12 max-w-md space-y-2 rounded-2xl bg-amber-800 p-3 text-white shadow-xl"
    >
      {problem && (
        <p role="alert" className="flex items-start gap-2 text-sm font-bold">
          <span className="flex-1">{problem}</span>
          <button type="button" onClick={() => setProblem(null)} aria-label={ar ? "إغلاق" : "Dismiss"} className="flex min-h-12 min-w-12 items-center justify-center">
            <X className="h-5 w-5" />
          </button>
        </p>
      )}

      {stale.map((e) => (
        <div key={e.id} data-testid="outbox-confirm" className="space-y-2 rounded-xl bg-amber-950/30 p-2">
          <p className="text-sm font-bold">
            {ar ? "ما زالت تريد إرسال " : "Still send "}«{ar ? e.label_ar : e.label_en}»{" "}
            {ar ? "المحفوظة الساعة " : "saved at "}
            <bdi dir="auto">{formatTime(new Date(e.created_at).toISOString(), locale)}</bdi>?{" "}
            {ar ? "ربما سجّلها أحد الإخوة منذ ذلك الوقت." : "A sibling may have logged it since."}
          </p>
          <div className="flex gap-2">
            <button type="button" onClick={() => removeFromOutbox(e.id)} className="min-h-12 flex-1 rounded-xl border border-white/60 text-sm font-semibold">
              {ar ? "احذفها" : "Discard"}
            </button>
            <button type="button" onClick={() => flush([e.id])} className="min-h-12 flex-1 rounded-xl bg-white text-sm font-bold text-amber-900">
              {ar ? "أرسلها" : "Send it"}
            </button>
          </div>
        </div>
      ))}

      {mine.length > stale.length && (
        <div className="flex items-center gap-2">
          <CloudOff className="h-5 w-5 shrink-0" />
          <p className="flex-1 text-sm font-bold">
            {ar
              ? `${mine.length - stale.length} محفوظة على هذا الهاتف ولم تُرسل بعد`
              : `${mine.length - stale.length} saved on this phone, not sent yet`}
          </p>
          <button
            type="button"
            onClick={() => flush()}
            disabled={busy}
            className="flex min-h-12 items-center gap-1 rounded-xl bg-white px-3 text-sm font-bold text-amber-900 disabled:opacity-60"
          >
            <RotateCw className={`h-4 w-4 ${busy ? "animate-spin" : ""}`} />
            {ar ? "أعد المحاولة" : "Retry"}
          </button>
        </div>
      )}

      {others.length > 0 && (
        <p className="text-xs font-semibold">
          {ar ? `${others.length} محفوظة باسم ${others[0].caregiver_name ?? "شخص آخر"}؛ تُرسل عند دخوله على هذا الهاتف.` : `${others.length} saved by ${others[0].caregiver_name ?? "someone else"}; sent when they sign in on this phone.`}
        </p>
      )}
    </div>
  );
}
