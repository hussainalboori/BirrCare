"use client";

import React from "react";

export type StartingWhenValue = "today" | "tomorrow";

/**
 * Issue #57: a schedule or dose change applies from today or from tomorrow;
 * earlier days keep the schedule they had (the doctor report stays true).
 */
export function StartingWhen({ value, onChange, ar }: { value: StartingWhenValue; onChange: (v: StartingWhenValue) => void; ar: boolean }) {
  const option = (v: StartingWhenValue, label: string) => (
    <button
      type="button"
      onClick={() => onChange(v)}
      aria-pressed={value === v}
      data-testid={`starting-${v}`}
      className={`min-h-12 flex-1 rounded-xl border-2 px-3 text-base font-bold ${
        value === v
          ? "border-emerald-700 bg-emerald-700 text-white"
          : "border-slate-300 bg-white text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-100"
      }`}
    >
      {label}
    </button>
  );
  return (
    <fieldset className="mb-4 rounded-xl border border-slate-200 p-3 dark:border-slate-700">
      <legend className="px-1 text-sm font-bold text-slate-800 dark:text-slate-100">{ar ? "متى يبدأ التغيير؟" : "Starting when?"}</legend>
      <div className="flex gap-2">
        {option("today", ar ? "اليوم" : "Today")}
        {option("tomorrow", ar ? "غداً" : "Tomorrow")}
      </div>
      <p className="mt-2 text-sm text-slate-600 dark:text-slate-300">
        {ar ? "الأيام السابقة تبقى كما كانت في السجل وتقرير الطبيب." : "Earlier days stay as they were, in the history and the doctor report."}
      </p>
    </fieldset>
  );
}
