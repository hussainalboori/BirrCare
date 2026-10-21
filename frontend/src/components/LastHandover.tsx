"use client";

import React, { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { fetchApi } from "@/lib/api";
import { formatTime } from "@/lib/datetime";
import { firstName } from "@/lib/actor";
import { isUrgentTag, tagLabel } from "@/lib/handoverTags";
import { RESYNC_EVENT } from "@/hooks/useLiveSync";
import type { HandoverNote, SSEEvent } from "@/lib/types";
import { AlertTriangle, ArrowRightLeft, ClipboardList } from "lucide-react";

/** Issue #18: the last handover at the top of Home: "From Hussain, 14:02: Ate half · Needs rest". */
export function LastHandover() {
  const { locale } = useLanguage();
  const { currentCaregiver } = useCaregiver();
  const ar = locale === "ar";
  const [note, setNote] = useState<HandoverNote | null>(null);

  const load = useCallback(async () => {
    const list = await fetchApi<HandoverNote[]>("/handovers?limit=1").catch(() => null);
    if (Array.isArray(list)) setNote(list[0] || null);
  }, []);

  useEffect(() => {
    if (!currentCaregiver) return;
    load();
    const onEvent = (e: Event) => {
      const type = (e as CustomEvent<SSEEvent>).detail?.type;
      if (type === "NOTE_ADDED" || type === "DUTY_PASSED" || type === "DUTY_ACCEPTED") load();
    };
    window.addEventListener("birrcare:sse_event", onEvent);
    window.addEventListener(RESYNC_EVENT, load);
    return () => {
      window.removeEventListener("birrcare:sse_event", onEvent);
      window.removeEventListener(RESYNC_EVENT, load);
    };
  }, [currentCaregiver, load]);

  if (!note) return null;
  const tags = (note.tags || []).map((k) => tagLabel(k, ar)).join(" · ");
  const text = [tags, note.summary].filter(Boolean).join(" — ");
  const urgent = note.urgent_flag || (note.tags || []).some(isUrgentTag);

  return (
    <Link
      href="/handovers"
      data-testid="last-handover"
      className={`mb-4 block rounded-2xl border p-3 active:scale-[0.99] ${
        urgent ? "border-rose-300 bg-rose-50 dark:border-rose-900 dark:bg-rose-950/40" : "border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900"
      }`}
    >
      <p className="flex items-center gap-1.5 text-xs font-bold text-slate-500 dark:text-slate-400">
        {urgent ? <AlertTriangle className="h-4 w-4 text-rose-600" /> : <ClipboardList className="h-4 w-4" />}
        {ar ? "آخر تسليم" : "Last handover"}
      </p>
      <p className="mt-1 text-sm font-bold text-slate-900 dark:text-white">
        {ar ? "من" : "From"} <bdi>{firstName(note.caregiver_name)}</bdi>{ar ? "،" : ","} <bdi dir="ltr">{formatTime(note.created_at, locale)}</bdi>
        {text && (
          <>
            {": "}
            <span className="font-semibold">{text.length > 160 ? `${text.slice(0, 160)}…` : text}</span>
          </>
        )}
      </p>
      {note.passed_to_name && (
        <p className="mt-1 flex items-center gap-1.5 text-xs font-bold text-blue-800 dark:text-blue-300">
          <ArrowRightLeft className="h-3.5 w-3.5" />
          {ar ? "المناوبة الآن مع" : "On duty now:"} <bdi>{firstName(note.passed_to_name)}</bdi>
          {!note.accepted_at && <span className="font-semibold">({ar ? "لم يقبلها بعد" : "not accepted yet"})</span>}
        </p>
      )}
    </Link>
  );
}
