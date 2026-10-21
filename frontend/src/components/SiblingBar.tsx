"use client";

import React from "react";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLanguage } from "@/context/LanguageContext";
import { firstName } from "@/lib/actor";

/**
 * Issue #17: who is using this phone, in big avatars with names. Tapping another
 * sibling opens the PIN keypad already set to them: one tap plus 4 digits.
 */
export function SiblingBar({ className = "" }: { className?: string }) {
  const { currentCaregiver, allCaregivers, switchCaregiverDirect, trustStatus } = useCaregiver();
  const { locale } = useLanguage();
  const [switchingId, setSwitchingId] = React.useState<string | null>(null);
  const ar = locale === "ar";
  if (allCaregivers.length < 2) return null;

  const handleSelectSibling = async (siblingId: string) => {
    if (switchingId || siblingId === currentCaregiver?.id) return;
    setSwitchingId(siblingId);
    try {
      await switchCaregiverDirect(siblingId);
    } finally {
      setSwitchingId(null);
    }
  };

  return (
    <section data-testid="sibling-bar" aria-label={ar ? "من يستخدم الجهاز الآن" : "Who is using this phone"} className={className}>
      <div className="mb-1.5 flex items-center justify-between">
        <p className="text-xs font-bold text-slate-500 dark:text-slate-400">
          {ar ? "من يستخدم الجهاز الآن؟ اضغط على اسمك" : "Who is using this phone? Tap your name"}
        </p>
        {trustStatus?.trusted && (
          <span className="text-xs font-semibold text-emerald-600 dark:text-emerald-400">
            {ar ? "تبديل سريع بلمسة واحدة" : "1-Tap Switch"}
          </span>
        )}
      </div>
      <div className="-mx-1 flex gap-1 overflow-x-auto px-1 pb-1">
        {allCaregivers.map((c) => {
          const active = c.id === currentCaregiver?.id;
          const isSwitching = switchingId === c.id;
          return (
            <button
              key={c.id}
              type="button"
              aria-current={active ? "true" : undefined}
              data-testid={`sibling-${c.id}`}
              disabled={isSwitching}
              onClick={() => !active && handleSelectSibling(c.id)}
              className={`flex w-20 shrink-0 flex-col items-center gap-1 rounded-2xl p-1.5 transition-colors ${
                active ? "bg-emerald-50 dark:bg-emerald-950/40" : "hover:bg-slate-100 active:scale-95 dark:hover:bg-slate-800"
              } ${isSwitching ? "opacity-60 animate-pulse" : ""}`}
            >
              <span
                className={`flex h-14 w-14 items-center justify-center rounded-full text-lg font-bold text-white shadow-sm transition-transform ${
                  active ? "ring-4 ring-emerald-500 ring-offset-2 ring-offset-white dark:ring-offset-slate-900" : "opacity-80"
                } ${isSwitching ? "scale-95" : ""}`}
                style={{ backgroundColor: c.avatar_color }}
              >
                {c.name.slice(0, 1)}
              </span>
              <span className={`w-full truncate text-center text-xs ${active ? "font-black text-emerald-800 dark:text-emerald-300" : "font-semibold text-slate-700 dark:text-slate-300"}`}>
                {firstName(c.name)}
              </span>
              {active && <span className="text-xs font-bold text-emerald-700 dark:text-emerald-400">{ar ? "أنت الآن" : "You now"}</span>}
            </button>
          );
        })}
      </div>
    </section>
  );
}
