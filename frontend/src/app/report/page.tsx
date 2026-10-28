"use client";

import React from "react";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { SiblingBar } from "@/components/SiblingBar";
import { DayReports } from "@/components/DayReports";
import { PushOptIn } from "@/components/PushOptIn";
import { NightThanksCard } from "@/components/NightThanksCard";
import { TodaySummaryCard } from "@/components/TodaySummaryCard";
import { useLanguage } from "@/context/LanguageContext";
import { useLiveSync } from "@/hooks/useLiveSync";

// Reports as they happen (several a day, every one listed) and lock-screen
// alerts. Repositioning, fluids and bowel are on Vitals & Tracker.
export default function ReportPage() {
  const { locale } = useLanguage();
  const ar = locale === "ar";
  const { status: connectionStatus } = useLiveSync();

  return (
    <div className="min-h-screen">
      <CaregiverHeader connectionStatus={connectionStatus} />

      <main className="mx-auto max-w-3xl px-4 py-5 sm:px-6 lg:max-w-none lg:px-8">
        <h1 className="mb-4 text-xl font-black tracking-tight text-slate-900 sm:text-2xl dark:text-white">
          {ar ? "التقرير" : "Report"}
        </h1>
        <SiblingBar className="mb-4" />
        {/* Issue #67: "how was she today?" in ten seconds, viewers included */}
        <TodaySummaryCard />
        <DayReports />
        {/* Issue #66: thank-you for the nights, rest notes, days away */}
        <NightThanksCard />
        <PushOptIn />
      </main>
    </div>
  );
}
