"use client";

import React from "react";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { SiblingBar } from "@/components/SiblingBar";
import { LastHandover } from "@/components/LastHandover";
import { PushOptIn } from "@/components/PushOptIn";
import { useLanguage } from "@/context/LanguageContext";
import { useLiveSync } from "@/hooks/useLiveSync";

// The last handover and lock-screen alerts, kept off the medicines page so it
// stays short. Repositioning, fluids and bowel are on Vitals & Tracker.
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
        <LastHandover />
        <PushOptIn />
      </main>
    </div>
  );
}
