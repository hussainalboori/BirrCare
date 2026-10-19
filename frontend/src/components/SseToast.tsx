"use client";

import React, { useEffect, useState } from "react";
import { SSEEvent } from "@/lib/types";
import { useLanguage } from "@/context/LanguageContext";
import { Pill, Activity, ClipboardList, UserCheck, X } from "lucide-react";

export function SseToast() {
  const { t, locale } = useLanguage();
  const [toast, setToast] = useState<{ event: SSEEvent; id: number } | null>(null);

  useEffect(() => {
    const handleEvent = (e: any) => {
      const event: SSEEvent = e.detail;
      if (!event) return;

      setToast({ event, id: Date.now() });

      // Auto dismiss after 4.5 seconds
      const timer = setTimeout(() => {
        setToast((curr) => (curr && curr.id === e.detail.id ? null : curr));
      }, 4500);

      return () => clearTimeout(timer);
    };

    window.addEventListener("birrcare:sse_event", handleEvent);
    return () => window.removeEventListener("birrcare:sse_event", handleEvent);
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
    <div className="fixed top-16 start-1/2 -translate-x-1/2 z-50 w-11/12 max-w-sm animate-in slide-in-from-top-4 duration-300">
      <div className="flex items-center gap-3 rounded-2xl border border-emerald-500/30 bg-slate-900/95 p-3 text-white shadow-xl backdrop-blur-md">
        <div
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-xs font-bold text-white shadow-xs"
          style={{ backgroundColor: actor.avatar_color || "#059669" }}
        >
          {actor.name.slice(0, 1)}
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-1.5">
            <span className="text-xs font-bold truncate">{actor.name}</span>
            <Icon className="h-3.5 w-3.5 text-emerald-400 shrink-0" />
          </div>
          <p className="text-[11px] text-slate-300 truncate">{actionText}</p>
        </div>

        <button
          onClick={() => setToast(null)}
          className="text-slate-400 hover:text-white p-1"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}
