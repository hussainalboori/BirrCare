"use client";

import React from "react";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLanguage } from "@/context/LanguageContext";
import { ConnectionStatus } from "@/hooks/useLiveSync";
import { User, Users, Globe, ShieldCheck } from "lucide-react";

interface CaregiverHeaderProps {
  connectionStatus?: ConnectionStatus;
}

export function CaregiverHeader({ connectionStatus = "connected" }: CaregiverHeaderProps) {
  const { currentCaregiver, openPinModal } = useCaregiver();
  const { locale, toggleLocale, t } = useLanguage();

  return (
    <header className="sticky top-0 z-30 w-full border-b border-slate-200/80 bg-white/90 backdrop-blur-md dark:border-slate-800 dark:bg-slate-900/90 shadow-xs">
      <div className="mx-auto flex max-w-5xl items-center justify-between px-4 py-2.5 sm:px-6">
        {/* Left: Active Caregiver Avatar & Profile */}
        <div className="flex items-center gap-3">
          <div
            className="relative flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-white font-semibold text-sm shadow-sm ring-2 ring-white dark:ring-slate-800"
            style={{ backgroundColor: currentCaregiver?.avatar_color || "#059669" }}
          >
            {currentCaregiver ? (
              currentCaregiver.name.slice(0, 1)
            ) : (
              <User className="h-5 w-5" />
            )}
            {/* Live SSE pulse indicator */}
            <span
              className={`absolute -bottom-0.5 -end-0.5 h-3 w-3 rounded-full border-2 border-white dark:border-slate-900 ${
                connectionStatus === "connected"
                  ? "bg-emerald-500 animate-pulse"
                  : connectionStatus === "connecting"
                  ? "bg-amber-500 animate-ping"
                  : "bg-rose-500"
              }`}
              title={
                connectionStatus === "connected"
                  ? t.liveConnected
                  : connectionStatus === "connecting"
                  ? t.liveReconnecting
                  : t.liveOffline
              }
            />
          </div>

          <div className="flex flex-col">
            <span className="text-xs font-medium text-slate-500 dark:text-slate-400">
              {t.activeCaregiver}
            </span>
            <div className="flex items-center gap-1.5">
              <span className="text-sm font-bold text-slate-900 dark:text-white">
                {currentCaregiver?.name || "..."}
              </span>
              {currentCaregiver?.role === "admin" && (
                <span title="Admin">
                  <ShieldCheck className="h-3.5 w-3.5 text-amber-500" />
                </span>
              )}
            </div>
          </div>
        </div>

        {/* Right: Quick Profile Switcher & Language Toggle */}
        <div className="flex items-center gap-2">
          {/* Switch Profile Button */}
          <button
            onClick={() => openPinModal()}
            className="flex items-center gap-1.5 rounded-xl bg-slate-100 hover:bg-slate-200 px-3 py-1.5 text-xs font-semibold text-slate-700 transition-colors active:scale-95 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700"
            aria-label={t.switchCaregiver}
          >
            <Users className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
            <span className="hidden sm:inline">{t.switchCaregiver}</span>
          </button>

          {/* Language Switcher */}
          <button
            onClick={toggleLocale}
            className="flex items-center gap-1 rounded-xl border border-slate-200 px-2.5 py-1.5 text-xs font-semibold text-slate-600 hover:bg-slate-50 transition-colors active:scale-95 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
            title="Switch Language"
          >
            <Globe className="h-3.5 w-3.5" />
            <span>{locale === "ar" ? "EN" : "عربي"}</span>
          </button>
        </div>
      </div>
    </header>
  );
}
