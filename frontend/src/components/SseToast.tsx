"use client";

import React, { useEffect, useRef, useState } from "react";
import { SSEEvent } from "@/lib/types";
import { useLanguage } from "@/context/LanguageContext";
import { Pill, Activity, ClipboardList, UserCheck, X } from "lucide-react";

export function SseToast() {
  const { t, locale } = useLanguage();
  const [toast, setToast] = useState<{ event: SSEEvent; id: number } | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    const handleEvent = (e: Event) => {
      const event = (e as CustomEvent<SSEEvent>).detail;
      if (!event || !event.actor) return;

      // Previously the dismiss timer compared Date.now() with the event's string id, which
      // never matched, so toasts never auto-dismissed. Use a single resettable timer instead.
      setToast({ event, id: Date.now() });
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => setToast(null), 4500);
    };

    window.addEventListener("birrcare:sse_event", handleEvent);
    return () => {
      window.removeEventListener("birrcare:sse_event", handleEvent);
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  if (!toast) return null;

  const { event } = toast;
  const actor = event.actor;

  let Icon = Pill;
  let actionText = "";

  switch (event.type) {
    case "MED_LOGGED":
      Icon = Pill;
      actionText = t.toastDoseTaken;
      break;
    case "VITAL_LOGGED":
      Icon = Activity;
      actionText = t.toastVitalsRecorded;
      break;
    case "NOTE_ADDED":
      Icon = ClipboardList;
      actionText = t.toastHandoverAdded;
      break;
    case "CAREGIVER_SWITCHED":
      Icon = UserCheck;
      actionText = locale === "ar" ? "بدّل حسابه" : "switched profile";
      break;
  }

  return (
    // inset-x-0 + mx-auto centers in both directions (start-1/2 + -translate-x-1/2 pushed it off-screen in RTL)
    <div
      role="status"
      aria-live="polite"
      className="fixed top-16 inset-x-0 mx-auto z-50 w-11/12 max-w-sm animate-in slide-in-from-top-4 duration-300"
    >
      <div className="flex items-center gap-3 rounded-2xl border border-emerald-500/30 bg-slate-900/95 p-3 text-white shadow-xl backdrop-blur-md">
        <div
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-xs font-bold text-white shadow-xs"
          style={{ backgroundColor: actor.avatar_color || "#059669" }}
        >
          {actor.name?.slice(0, 1)}
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-1.5">
            <span className="text-xs font-bold truncate">{actor.name}</span>
            <Icon className="h-3.5 w-3.5 text-emerald-400 shrink-0" />
          </div>
          <p className="text-[11px] text-slate-300 truncate">{actionText}</p>
        </div>

        <button
          type="button"
          onClick={() => setToast(null)}
          aria-label={locale === "ar" ? "إغلاق التنبيه" : "Dismiss notification"}
          className="flex min-h-12 min-w-12 items-center justify-center rounded-full text-slate-400 hover:text-white"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}
