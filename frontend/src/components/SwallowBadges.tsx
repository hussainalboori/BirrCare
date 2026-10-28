"use client";

import React from "react";
import type { Medication } from "@/lib/types";
import { Ban, Check, HelpCircle } from "lucide-react";

// Crush / split / food guidance (issue #64), next to the dose on the card. "Do not
// crush" is red and always shown; "can be crushed" green. A medicine nobody has
// checked yet says "ask the pharmacist" while a swallowing difficulty is recent
// (that's when the family starts crushing pills into yogurt).

export function crushLabel(crush: Medication["crush"], ar: boolean): string {
  switch (crush) {
    case "do_not_crush":
      return ar ? "لا تُطحن" : "Do not crush";
    case "ok":
      return ar ? "يمكن طحنها" : "Can be crushed";
    default:
      return ar ? "اسأل الصيدلي قبل الطحن" : "Ask the pharmacist before crushing";
  }
}

export function CrushBadge({ crush, ar, className = "" }: { crush: Medication["crush"]; ar: boolean; className?: string }) {
  const tone =
    crush === "do_not_crush"
      ? "border-rose-800 bg-rose-700 text-white dark:border-rose-500 dark:bg-rose-800 dark:text-white"
      : crush === "ok"
      ? "border-emerald-300 bg-emerald-50 text-emerald-900 dark:border-emerald-800 dark:bg-emerald-950/70 dark:text-emerald-100"
      : "border-slate-300 bg-slate-100 text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-100";
  const Icon = crush === "do_not_crush" ? Ban : crush === "ok" ? Check : HelpCircle;
  return (
    <span data-testid={`crush-${crush || "ask_pharmacist"}`} className={`inline-flex items-center gap-1.5 rounded-lg border px-2.5 py-1 text-base font-black ${tone} ${className}`}>
      <Icon className="h-5 w-5 shrink-0" aria-hidden="true" />
      {crushLabel(crush, ar)}
    </span>
  );
}

/** The split and food notes, as plain small badges ("" when nothing is set). */
export function SwallowNotes({ medication, ar }: { medication: Medication; ar: boolean }) {
  const notes = [
    medication.split_ok === true && (ar ? "يمكن قسمها" : "Can be split"),
    medication.split_ok === false && (ar ? "لا تُقسم" : "Do not split"),
    medication.with_food === "with_food" && (ar ? "مع الطعام" : "With food"),
    medication.with_food === "empty_stomach" && (ar ? "على معدة فارغة" : "On an empty stomach"),
  ].filter(Boolean) as string[];
  return (
    <>
      {notes.map((n) => (
        <span
          key={n}
          className="inline-flex items-center rounded-md border border-slate-200 bg-white px-2.5 py-1 text-sm font-bold text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
        >
          {n}
        </span>
      ))}
    </>
  );
}
