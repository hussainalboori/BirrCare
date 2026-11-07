"use client";

import React from "react";
import { ChevronLeft, ChevronRight, Repeat } from "lucide-react";
import type { Locale } from "@/lib/i18n";
import { monthGrid, monthLabel, shiftMonth, weekdayNames } from "@/lib/appointments";
import type { ShiftMonthEntry } from "@/lib/types";
import { Ltr } from "./Ltr";

// The roster as a month (Shift Management): a dot in each person's colour on the
// days they have a shift; tap a day to change it underneath.

export function ShiftCalendar({
  month,
  today,
  selected,
  entries,
  onMonth,
  onSelect,
  locale,
}: {
  month: string; // YYYY-MM
  today: string; // household YYYY-MM-DD
  selected: string;
  entries: ShiftMonthEntry[];
  onMonth: (month: string) => void;
  onSelect: (day: string) => void;
  locale: Locale;
}) {
  const ar = locale === "ar";
  const byDay = new Map<string, ShiftMonthEntry[]>();
  const people = new Map<string, ShiftMonthEntry>();
  for (const e of entries) {
    byDay.set(e.date, [...(byDay.get(e.date) ?? []), e]);
    people.set(e.caregiver_id, e);
  }
  // Arrows point the way the page reads.
  const Prev = ar ? ChevronRight : ChevronLeft;
  const Next = ar ? ChevronLeft : ChevronRight;
  const navBtn =
    "flex min-h-12 items-center gap-1 rounded-xl border border-slate-300 bg-white px-3 text-sm font-bold text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100";

  return (
    <div data-testid="shift-calendar">
      <div className="flex items-center justify-between gap-2">
        <button type="button" onClick={() => onMonth(shiftMonth(month, -1))} className={navBtn} data-testid="shift-calendar-prev">
          <Prev className="h-5 w-5 shrink-0" />
          {ar ? "السابق" : "Previous"}
        </button>
        <h3 data-testid="shift-calendar-month" className="text-center text-base font-black text-slate-900 dark:text-slate-100">
          {monthLabel(month, locale)}
        </h3>
        <button type="button" onClick={() => onMonth(shiftMonth(month, 1))} className={navBtn} data-testid="shift-calendar-next">
          {ar ? "التالي" : "Next"}
          <Next className="h-5 w-5 shrink-0" />
        </button>
      </div>
      {(month !== today.slice(0, 7) || selected !== today) && (
        <button
          type="button"
          onClick={() => {
            onMonth(today.slice(0, 7));
            onSelect(today);
          }}
          className="mt-2 min-h-12 w-full rounded-xl text-sm font-bold text-purple-800 underline dark:text-purple-300"
        >
          {ar ? "العودة إلى اليوم" : "Back to today"}
        </button>
      )}

      <div role="grid" className="mt-2 grid grid-cols-7 gap-0.5 text-center">
        {weekdayNames(locale).map((n) => (
          <div key={n} role="columnheader" className="pb-1 text-xs font-bold text-slate-600 dark:text-slate-400">
            {n}
          </div>
        ))}
        {monthGrid(month).map((day, i) => {
          if (!day) return <div key={`pad-${i}`} />;
          const list = byDay.get(day) ?? [];
          const isToday = day === today;
          const isSelected = day === selected;
          const names = list.map((e) => e.caregiver_name).join(", ");
          return (
            <button
              key={day}
              type="button"
              role="gridcell"
              aria-selected={isSelected}
              aria-label={`${day}: ${names || (ar ? "لا أحد" : "nobody")}`}
              data-testid="shift-calendar-day"
              data-date={day}
              data-count={list.length}
              onClick={() => onSelect(day)}
              className={`flex min-h-12 flex-col items-center justify-start rounded-lg pt-1 ${
                isSelected
                  ? "bg-purple-700 text-white dark:bg-purple-600"
                  : isToday
                    ? "border-2 border-purple-700 text-slate-900 dark:border-purple-400 dark:text-slate-100"
                    : "text-slate-800 hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800"
              }`}
            >
              <Ltr className={`text-sm ${isToday || list.length ? "font-black" : "font-medium"}`}>{Number(day.slice(8))}</Ltr>
              <span className="mt-0.5 flex items-center gap-0.5" aria-hidden="true">
                {list.slice(0, 3).map((e) => (
                  <span
                    key={e.shift_id}
                    className={`h-2.5 w-2.5 rounded-full ${isSelected ? "ring-1 ring-white" : ""}`}
                    style={{ backgroundColor: e.avatar_color || "#64748b" }}
                  />
                ))}
              </span>
            </button>
          );
        })}
      </div>

      {people.size > 0 && (
        <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1" data-testid="shift-calendar-people">
          {[...people.values()].map((e) => (
            <li key={e.caregiver_id} className="flex items-center gap-1.5 text-xs font-semibold text-slate-700 dark:text-slate-300">
              <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: e.avatar_color || "#64748b" }} />
              <bdi>{e.caregiver_name}</bdi>
              {entries.some((x) => x.caregiver_id === e.caregiver_id && x.repeat_id) && <Repeat className="h-3.5 w-3.5 text-slate-500 dark:text-slate-400" aria-hidden="true" />}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
