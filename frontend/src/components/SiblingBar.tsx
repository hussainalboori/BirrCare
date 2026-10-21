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
  const { currentCaregiver, allCaregivers, openPinModal } = useCaregiver();
  const { locale } = useLanguage();
  const ar = locale === "ar";
  if (allCaregivers.length < 2) return null;

  return (
    <section data-testid="sibling-bar" aria-label={ar ? "من يستخدم الجهاز الآن" : "Who is using this phone"} className={className}>
      <p className="mb-1.5 text-xs font-bold text-slate-500 dark:text-slate-400">
        {ar ? "من يستخدم الجهاز الآن؟ اضغط على اسمك" : "Who is using this phone? Tap your name"}
      </p>
      <div className="-mx-1 flex gap-1 overflow-x-auto px-1 pb-1">
        {allCaregivers.map((c) => {
          const active = c.id === currentCaregiver?.id;
          return (
            <button
              key={c.id}
              type="button"
              aria-current={active ? "true" : undefined}
              data-testid={`sibling-${c.id}`}
              onClick={() => !active && openPinModal(c.id)}
              className={`flex w-20 shrink-0 flex-col items-center gap-1 rounded-2xl p-1.5 transition-colors ${
                active ? "bg-emerald-50 dark:bg-emerald-950/40" : "hover:bg-slate-100 active:scale-95 dark:hover:bg-slate-800"
              }`}
            >
              <span
                className={`flex h-14 w-14 items-center justify-center rounded-full text-lg font-bold text-white shadow-sm ${
                  active ? "ring-4 ring-emerald-500 ring-offset-2 ring-offset-white dark:ring-offset-slate-900" : "opacity-80"
                }`}
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
