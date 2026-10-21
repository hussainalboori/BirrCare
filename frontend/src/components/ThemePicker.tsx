"use client";

import React, { useEffect, useState } from "react";
import { useLanguage } from "@/context/LanguageContext";
import { THEME_COOKIE, applyTheme, parseThemeMode, resolveTheme, type ThemeMode } from "@/lib/theme";
import { Moon, Sun, MonitorSmartphone, Clock, X, Check } from "lucide-react";

// Issue #21: Auto / Light / Dark / Night screen colours.

export const THEME_EVENT = "birrcare:theme";

function readMode(): ThemeMode {
  if (typeof document === "undefined") return "auto";
  return parseThemeMode((document.cookie.match(new RegExp(`(?:^|; )${THEME_COOKIE}=([^;]*)`)) || [])[1]);
}

function systemDark(): boolean {
  return typeof window !== "undefined" && !!window.matchMedia?.("(prefers-color-scheme: dark)").matches;
}

export function setThemeMode(mode: ThemeMode) {
  document.cookie = `${THEME_COOKIE}=${mode}; path=/; max-age=${60 * 60 * 24 * 365}; samesite=lax`;
  applyTheme(resolveTheme(mode, new Date().getHours(), systemDark()));
  window.dispatchEvent(new Event(THEME_EVENT));
}

export function useThemeMode(): ThemeMode {
  const [mode, setMode] = useState<ThemeMode>("auto");
  useEffect(() => {
    const read = () => setMode(readMode());
    read();
    window.addEventListener(THEME_EVENT, read);
    return () => window.removeEventListener(THEME_EVENT, read);
  }, []);
  return mode;
}

/** Keeps Auto right as the evening comes (21:00) and the morning (07:00). */
export function ThemeKeeper() {
  useEffect(() => {
    const update = () => {
      const mode = readMode();
      if (mode === "auto") applyTheme(resolveTheme(mode, new Date().getHours(), systemDark()));
    };
    update();
    const timer = window.setInterval(update, 60_000);
    const mq = window.matchMedia?.("(prefers-color-scheme: dark)");
    mq?.addEventListener?.("change", update);
    document.addEventListener("visibilitychange", update);
    return () => {
      window.clearInterval(timer);
      mq?.removeEventListener?.("change", update);
      document.removeEventListener("visibilitychange", update);
    };
  }, []);
  return null;
}

export function themeLabel(mode: ThemeMode, ar: boolean): string {
  return {
    auto: ar ? "تلقائي" : "Auto",
    light: ar ? "فاتح" : "Light",
    dark: ar ? "داكن" : "Dark",
    night: ar ? "ليلي (أسود)" : "Night (black)",
  }[mode];
}

export function ThemePicker({ isOpen, onClose }: { isOpen: boolean; onClose: () => void }) {
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const mode = useThemeMode();
  if (!isOpen) return null;

  const options: { mode: ThemeMode; icon: React.ElementType; hint: string }[] = [
    { mode: "auto", icon: Clock, hint: ar ? "ليلي من 9 مساءً إلى 7 صباحاً، وإلا حسب إعداد الهاتف" : "Night from 9 PM to 7 AM, otherwise the phone's setting" },
    { mode: "night", icon: Moon, hint: ar ? "أسود تماماً وخافت، لغرفة المريضة في الليل" : "Pure black and dim, for her room at night" },
    { mode: "dark", icon: MonitorSmartphone, hint: ar ? "ألوان داكنة" : "Dark colours" },
    { mode: "light", icon: Sun, hint: ar ? "ألوان فاتحة" : "Light colours" },
  ];

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-slate-900/60 sm:items-center" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={ar ? "ألوان الشاشة" : "Screen colours"}
        className="w-full max-w-md rounded-t-3xl bg-white p-4 pb-[calc(1rem+env(safe-area-inset-bottom))] shadow-2xl dark:bg-slate-900 sm:rounded-3xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-2 flex items-center justify-between">
          <span className="ps-2 text-base font-black text-slate-900 dark:text-white">{ar ? "ألوان الشاشة" : "Screen colours"}</span>
          <button type="button" onClick={onClose} className="flex min-h-12 items-center gap-1 rounded-full px-3 text-sm font-bold text-slate-500 dark:text-slate-400">
            <X className="h-5 w-5" />
            {ar ? "إغلاق" : "Close"}
          </button>
        </div>
        <div className="space-y-2">
          {options.map((o) => {
            const on = o.mode === mode;
            return (
              <button
                key={o.mode}
                type="button"
                aria-pressed={on}
                onClick={() => {
                  setThemeMode(o.mode);
                  onClose();
                }}
                className={`flex min-h-16 w-full items-center gap-3 rounded-2xl border-2 px-4 text-start active:scale-[0.98] ${
                  on ? "border-emerald-600 bg-emerald-50 dark:bg-emerald-950/60" : "border-slate-200 dark:border-slate-700"
                }`}
              >
                <o.icon className="h-6 w-6 shrink-0 text-slate-700 dark:text-slate-200" />
                <span className="flex-1">
                  <span className="block text-base font-bold text-slate-900 dark:text-white">{themeLabel(o.mode, ar)}</span>
                  <span className="block text-sm text-slate-600 dark:text-slate-300">{o.hint}</span>
                </span>
                {on && <Check className="h-5 w-5 shrink-0 text-emerald-700 dark:text-emerald-400" />}
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}
