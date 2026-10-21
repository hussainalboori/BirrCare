"use client";

import React from "react";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { SiblingBar } from "@/components/SiblingBar";
import { LastHandover } from "@/components/LastHandover";
import { PushOptIn } from "@/components/PushOptIn";
import { RepositionCard } from "@/components/RepositionCard";
import { FluidBowelCard } from "@/components/FluidBowelCard";
import { useLanguage } from "@/context/LanguageContext";
import { useLiveSync } from "@/hooks/useLiveSync";

// The day's care around the medicines: last handover, repositioning, fluids and
// bowel, and lock-screen alerts. Moved off the medicines page so it stays short.
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
        {/* Who is logging: repositioning and fluids are saved under this name */}
        <SiblingBar className="mb-4" />
        <LastHandover />
        <PushOptIn />
        {/* Issue #27: Repositioning card to prevent pressure sores */}
        <RepositionCard />
        {/* Issue #28: Fluid intake and bowel movement tracking */}
        <FluidBowelCard />
      </main>
    </div>
  );
}
