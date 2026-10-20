"use client";

import React, { useState, useEffect, useCallback, useRef, useMemo } from "react";
import { PushOptIn } from "@/components/PushOptIn";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { MedicationSlotCard } from "@/components/MedicationSlotCard";
import { useLanguage } from "@/context/LanguageContext";
import { useLiveSync } from "@/hooks/useLiveSync";
import { DailyScheduleResponse, ScheduledDose } from "@/lib/types";
import { fetchApi } from "@/lib/api";
import { localDateString } from "@/lib/datetime";
import { Ltr } from "@/components/Ltr";
import { Calendar, CheckCircle2, Clock, Sparkles, Brain } from "lucide-react";
import Link from "next/link";

export default function MedicationsPage() {
  const { t, locale } = useLanguage();
  // Local calendar date (NOT toISOString(), which is the UTC date and is "yesterday"
  // between 00:00–03:00 in UTC+3).
  const [selectedDate, setSelectedDate] = useState<string>(() => localDateString());
  // Whether the user is looking at "today" (so we can roll over at midnight on wake).
  const followsTodayRef = useRef(true);
  const [scheduleData, setScheduleData] = useState<ScheduledDose[]>([]);
  const [loading, setLoading] = useState(true);

  const selectedDayName = useMemo(() => {
    if (!selectedDate) return "";
    const parts = selectedDate.split("-").map(Number);
    if (parts.length !== 3 || isNaN(parts[0]) || isNaN(parts[1]) || isNaN(parts[2])) return "";
    const dt = new Date(parts[0], parts[1] - 1, parts[2]);
    if (isNaN(dt.getTime())) return "";
    return dt.toLocaleDateString(locale === "ar" ? "ar-SA" : "en-US", { weekday: "long" });
  }, [selectedDate, locale]);
  // Monotonic request id: responses for an older date/request are discarded, so a slow
  // response can never render one day's doses under another day's date picker.
  const requestSeqRef = useRef(0);

  const loadSchedule = useCallback(async (date: string) => {
    const seq = ++requestSeqRef.current;
    try {
      const res = await fetchApi<DailyScheduleResponse>(`/medications/schedule?date=${encodeURIComponent(date)}`);
      if (seq !== requestSeqRef.current) return;
      setScheduleData(res?.schedule || []);
    } catch (err) {
      console.warn("Failed fetching schedule:", err);
    } finally {
      if (seq === requestSeqRef.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadSchedule(selectedDate);
  }, [selectedDate, loadSchedule]);

  const handleDateChange = (value: string) => {
    if (!value) return;
    followsTodayRef.current = value === localDateString();
    setLoading(true);
    setSelectedDate(value);
  };

  // Real-time SSE listener: instantly refresh schedule when any sibling logs a dose
  const onDoseChanged = (event: { payload?: unknown }) => {
    const date = (event.payload as { scheduled_date?: string } | undefined)?.scheduled_date;
    if (!date || date === selectedDate) loadSchedule(selectedDate);
  };
  const { status: connectionStatus } = useLiveSync({
    onMedLogged: onDoseChanged,
    onMedVoided: onDoseChanged,
    onMedEvent: onDoseChanged,
    // After reconnect / returning from background: re-hydrate (and roll over to the new day).
    onResync: () => {
      const today = localDateString();
      if (followsTodayRef.current && today !== selectedDate) {
        setSelectedDate(today);
      } else {
        loadSchedule(selectedDate);
      }
    },
  });

  // Calculate statistics
  const totalDoses = scheduleData.length;
  const takenDoses = scheduleData.filter((d) => d.log?.status === "taken").length;
  const progressPercent = totalDoses > 0 ? Math.round((takenDoses / totalDoses) * 100) : 0;

  // Group by slots in clock order. "night" (02:00 by default) must be listed, or the
  // dose given at 3 AM never renders on the dashboard.
  const slotOrder = ["night", "morning", "afternoon", "evening", "bedtime"];
  const groupedBySlot = slotOrder.map((slot) => ({
    slot,
    doses: scheduleData.filter((d) => d.slot === slot),
  }));

  return (
    <div className="min-h-screen">
      <CaregiverHeader
        connectionStatus={connectionStatus}
        onMedicationsUpdated={() => loadSchedule(selectedDate)}
      />

      <main className="mx-auto max-w-3xl px-4 py-5 sm:px-6 lg:max-w-none lg:px-8">
        <PushOptIn />
        {/* Issue #13: one tap to the F.A.S.T. stroke check */}
        <Link
          href="/fast"
          data-testid="seems-off"
          className="mb-4 flex min-h-14 items-center justify-center gap-2 rounded-2xl border-2 border-rose-700 bg-white px-4 text-lg font-black text-rose-800 active:scale-95 dark:bg-slate-900 dark:text-rose-300"
        >
          <Brain className="h-6 w-6" />
          {locale === "ar" ? "تبدو غير طبيعية؟ فحص الجلطة السريع" : "She seems off? Quick stroke check"}
        </Link>
        {/* Header & Date Selector */}
        <div className="mb-6 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-xl sm:text-2xl font-black text-slate-900 dark:text-white tracking-tight">
                {t.todayMedications}
              </h1>
              <Sparkles className="h-5 w-5 text-emerald-500" />
            </div>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-1">
              {t.birrDescription}
            </p>
          </div>

          {/* Date Picker Input & Day Name */}
          <div className="flex items-center gap-2 flex-wrap">
            {selectedDayName && (
              <span className="rounded-xl bg-emerald-50 dark:bg-emerald-950/80 px-3 py-2 text-xs font-bold text-emerald-800 dark:text-emerald-300 border border-emerald-200 dark:border-emerald-800">
                {selectedDayName}
              </span>
            )}

            <div className="flex items-center gap-2 rounded-2xl border border-slate-200 bg-white px-3 py-1.5 shadow-2xs dark:border-slate-800 dark:bg-slate-900">
              <Calendar className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
              <input
                type="date"
                value={selectedDate}
                onChange={(e) => handleDateChange(e.target.value)}
                aria-label={locale === "ar" ? "تاريخ الجدول" : "Schedule date"}
                className="min-h-11 bg-transparent text-base font-bold text-slate-800 dark:text-slate-200 focus:outline-none"
              />
            </div>
          </div>
        </div>

        {/* Adherence Progress Bar */}
        <div className="mb-6 rounded-2xl border border-slate-200/80 bg-white p-4 shadow-xs dark:border-slate-800 dark:bg-slate-900">
          <div className="flex items-center justify-between text-xs font-bold mb-2">
            <span className="text-slate-700 dark:text-slate-300">
              {locale === "ar" ? "نسبة إنجاز جرعات اليوم" : "Today's Adherence Rate"}
            </span>
            <span className="text-emerald-600 dark:text-emerald-400">
              <Ltr>{takenDoses} / {totalDoses} ({progressPercent}%)</Ltr>
            </span>
          </div>
          <div className="h-2.5 w-full overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
            <div
              className="h-full rounded-full bg-emerald-500 transition-all duration-500 ease-out"
              style={{ width: `${progressPercent}%` }}
            />
          </div>
        </div>

        {/* Schedule List Grouped by Slots */}
        {loading ? (
          <div className="space-y-4">
            {[1, 2, 3].map((i) => (
              <div
                key={i}
                className="h-24 w-full animate-pulse rounded-2xl bg-slate-200 dark:bg-slate-800"
              />
            ))}
          </div>
        ) : scheduleData.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-slate-300 p-8 text-center dark:border-slate-800">
            <Clock className="mx-auto h-8 w-8 text-slate-400 mb-2" />
            <p className="text-sm font-semibold text-slate-600 dark:text-slate-300">
              {locale === "ar" ? "لا توجد أدوية مجدولة لهذا التاريخ" : "No medications scheduled for this date"}
            </p>
          </div>
        ) : (
          <div className="space-y-6">
            {groupedBySlot.map(({ slot, doses }) => {
              if (doses.length === 0) return null;
              return (
                <section key={slot} className="space-y-3">
                  <div className="flex items-center gap-2">
                    <span className="flex h-6 min-w-6 px-1 items-center justify-center rounded-lg bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300 text-xs font-bold">
                      <Ltr>{doses.filter((d) => d.log?.status === "taken").length}/{doses.length}</Ltr>
                    </span>
                    <h2 className="text-sm font-bold text-slate-700 dark:text-slate-300 uppercase tracking-wider">
                      {t.slots[slot as keyof typeof t.slots] || slot}
                    </h2>
                  </div>

                  <div className="grid gap-3 lg:grid-cols-2 2xl:grid-cols-3">
                    {doses.map((dose) => (
                      <MedicationSlotCard
                        key={`${selectedDate}:${dose.medication.id}:${dose.slot}`}
                        dose={dose}
                        scheduledDate={selectedDate}
                        onLoggedSuccess={() => loadSchedule(selectedDate)}
                      />
                    ))}
                  </div>
                </section>
              );
            })}
          </div>
        )}
      </main>
    </div>
  );
}
