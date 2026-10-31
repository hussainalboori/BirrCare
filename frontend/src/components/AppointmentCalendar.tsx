"use client";

import React from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import type { Locale } from "@/lib/i18n";
import { monthGrid, monthLabel, shiftMonth, weekdayNames, type Appointment, type AppointmentKind } from "@/lib/appointments";
import { Ltr } from "./Ltr";

// The appointments as a month (Visits page): a dot per visit on its day, the
// colour by kind; tap a day to see its visits underneath. Dates are household
// dates (Appointment.date), so a sibling abroad sees home's calendar.

const DOT: Record<AppointmentKind, string> = {
  doctor: "bg-sky-600 dark:bg-sky-400",
  lab: "bg-rose-600 dark:bg-rose-400",
  physio: "bg-emerald-600 dark:bg-emerald-400",
  other: "bg-amber-500 dark:bg-amber-300",
};
const DONE_DOT = "bg-slate-400 dark:bg-slate-500";

export interface CalendarState {
  month: string; // YYYY-MM
  today: string; // household YYYY-MM-DD
  selected: string | null;
}

export function AppointmentCalendar({
  appointments,
  state,
  onChange,
  locale,
}: {
  appointments: Appointment[];
  state: CalendarState;
  onChange: (next: CalendarState) => void;
  locale: Locale;
}) {
  const ar = locale === "ar";
  const byDay = new Map<string, Appointment[]>();
  for (const a of appointments) {
    if (a.status === "cancelled") continue;
    byDay.set(a.date, [...(byDay.get(a.date) ?? []), a]);
  }
  const names = weekdayNames(locale);
  const go = (step: number) => onChange({ ...state, month: shiftMonth(state.month, step), selected: null });
  // Arrows point the way the page reads.
  const Prev = ar ? ChevronRight : ChevronLeft;
  const Next = ar ? ChevronLeft : ChevronRight;
  const navBtn =
    "flex min-h-12 items-center gap-1 rounded-xl border border-slate-300 bg-white px-3 text-sm font-bold text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100";

  return (
    <div data-testid="appointment-calendar" className="mt-3">
      <div className="flex items-center justify-between gap-2">
        <button type="button" onClick={() => go(-1)} className={navBtn} data-testid="calendar-prev">
          <Prev className="h-5 w-5 shrink-0" />
          {ar ? "السابق" : "Previous"}
        </button>
        <h3 data-testid="calendar-month" className="text-center text-base font-black text-slate-900 dark:text-slate-100">
          {monthLabel(state.month, locale)}
        </h3>
        <button type="button" onClick={() => go(1)} className={navBtn} data-testid="calendar-next">
          {ar ? "التالي" : "Next"}
          <Next className="h-5 w-5 shrink-0" />
        </button>
      </div>
      {state.month !== state.today.slice(0, 7) && (
        <button
          type="button"
          onClick={() => onChange({ ...state, month: state.today.slice(0, 7), selected: state.today })}
          className="mt-2 min-h-12 w-full rounded-xl text-sm font-bold text-sky-800 underline dark:text-sky-300"
        >
          {ar ? "العودة إلى اليوم" : "Back to today"}
        </button>
      )}

      <div role="grid" className="-mx-2 mt-2 grid grid-cols-7 gap-0.5 text-center">
        {names.map((n) => (
          <div key={n} role="columnheader" className="pb-1 text-xs font-bold text-slate-600 dark:text-slate-400">
            {n}
          </div>
        ))}
        {monthGrid(state.month).map((day, i) => {
          if (!day) return <div key={`pad-${i}`} />;
          const list = byDay.get(day) ?? [];
          const isToday = day === state.today;
          const selected = day === state.selected;
          const label = `${day}: ${list.length === 0 ? (ar ? "لا مواعيد" : "no visits") : ar ? `${list.length} موعد` : `${list.length} visit${list.length > 1 ? "s" : ""}`}`;
          return (
            <button
              key={day}
              type="button"
              role="gridcell"
              aria-selected={selected}
              aria-label={label}
              data-testid="calendar-day"
              data-date={day}
              data-count={list.length}
              onClick={() => onChange({ ...state, selected: selected ? null : day })}
              className={`flex min-h-12 flex-col items-center justify-start rounded-lg pt-1 ${
                selected
                  ? "bg-sky-700 text-white dark:bg-sky-600"
                  : isToday
                    ? "border-2 border-sky-700 text-slate-900 dark:border-sky-400 dark:text-slate-100"
                    : "text-slate-800 hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800"
              }`}
            >
              <Ltr className={`text-sm ${isToday || list.length ? "font-black" : "font-medium"}`}>{Number(day.slice(8))}</Ltr>
              <span className="mt-0.5 flex gap-0.5" aria-hidden="true">
                {list.slice(0, 3).map((a) => (
                  <span key={a.id} className={`h-2 w-2 rounded-full ${selected ? "bg-white" : a.status === "scheduled" ? DOT[a.kind] : DONE_DOT}`} />
                ))}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

/** The visits on one day, in time order (cancelled ones left out). */
export function appointmentsOn(appointments: Appointment[], day: string): Appointment[] {
  return appointments.filter((a) => a.date === day && a.status !== "cancelled").sort((x, y) => x.time.localeCompare(y.time));
}
