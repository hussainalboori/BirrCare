"use client";

import React from "react";
import { classifyBloodPressure } from "@/lib/clinical";
import { useLanguage } from "@/context/LanguageContext";
import { AlertTriangle, CheckCircle, AlertCircle } from "lucide-react";
import { Ltr } from "./Ltr";
import { CrisisActions } from "./CrisisActions";

interface BloodPressureBadgeProps {
  systolic: number;
  diastolic: number;
  showDetails?: boolean;
}

export function BloodPressureBadge({ systolic, diastolic, showDetails = false }: BloodPressureBadgeProps) {
  const { locale } = useLanguage();
  const meta = classifyBloodPressure(systolic, diastolic);

  const isCrisis = meta.status === "CRISIS";
  const isNormal = meta.status === "NORMAL";

  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-wrap items-center gap-2">
        {/* Reading + unit isolated as one LTR, unbreakable run: never "80/120" or "mmHg" on its own line */}
        <Ltr className="inline-flex items-baseline gap-1" data-testid="bp-reading">
          <span className="text-xl font-black text-slate-900 dark:text-white tracking-tight">
            {systolic}/{diastolic}
          </span>
          <span className="text-xs text-slate-500 dark:text-slate-400">mmHg</span>
        </Ltr>

        <span
          role={isCrisis ? "alert" : undefined}
          className={`inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-bold ${meta.badgeClass}`}
        >
          {isCrisis ? (
            <AlertTriangle className="h-3.5 w-3.5 animate-bounce" />
          ) : isNormal ? (
            <CheckCircle className="h-3 w-3" />
          ) : (
            <AlertCircle className="h-3 w-3" />
          )}
          <span>{locale === "ar" ? meta.labelAr : meta.labelEn}</span>
        </span>
      </div>

      {showDetails && (
        <div className={`p-2.5 rounded-xl border text-xs font-medium ${meta.bgLightClass}`}>
          {locale === "ar" ? meta.actionAr : meta.actionEn}
        </div>
      )}

      {showDetails && isCrisis && <CrisisActions />}
    </div>
  );
}
