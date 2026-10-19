"use client";

import React from "react";
import { classifyBloodSugar, BloodSugarTiming } from "@/lib/clinical";
import { useLanguage } from "@/context/LanguageContext";
import { Droplet, AlertTriangle, CheckCircle, AlertCircle } from "lucide-react";

interface BloodSugarBadgeProps {
  sugar: number;
  timing?: BloodSugarTiming;
  showDetails?: boolean;
}

export function BloodSugarBadge({ sugar, timing = "random", showDetails = false }: BloodSugarBadgeProps) {
  const { locale } = useLanguage();
  const meta = classifyBloodSugar(sugar, timing);

  const isAlert = meta.status === "LOW" || meta.status === "VERY_HIGH";
  const isNormal = meta.status === "NORMAL";

  const timingLabels = {
    fasting: locale === "ar" ? "صائم" : "Fasting",
    post_meal: locale === "ar" ? "بعد الأكل" : "Post-meal",
    random: locale === "ar" ? "عشوائي" : "Random",
  };

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-2">
        <div className="flex items-center gap-1">
          <Droplet className={`h-4 w-4 ${isAlert ? "text-rose-500 fill-rose-500" : "text-blue-500 fill-blue-500"}`} />
          <span className="text-xl font-black text-slate-900 dark:text-white tracking-tight">
            {typeof sugar === "number" ? Number(sugar.toFixed(1)) : sugar}
          </span>
          <span className="text-xs text-slate-500 dark:text-slate-400">mmol/L</span>
        </div>

        {timing && (
          <span className="text-[10px] font-semibold text-slate-500 bg-slate-100 dark:bg-slate-800 px-1.5 py-0.5 rounded-sm">
            {timingLabels[timing]}
          </span>
        )}

        <span
          className={`inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-bold ${meta.badgeClass}`}
        >
          {isAlert ? (
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
    </div>
  );
}
