"use client";

import React, { useState, useEffect, useCallback, useRef } from "react";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLiveSync } from "@/hooks/useLiveSync";
import { HandoverNote, ShiftType, Shift } from "@/lib/types";
import { canTakeActions, isViewer } from "@/lib/permissions";
import { fetchApi } from "@/lib/api";
import { formatShortDate, formatTime } from "@/lib/datetime";
import { Ltr } from "@/components/Ltr";
import { ClipboardList, Plus, AlertTriangle, Clock, Send, Loader2 } from "lucide-react";

export default function HandoversPage() {
  const { t, locale } = useLanguage();
  const { currentCaregiver, openPinModal } = useCaregiver();

  const [notesList, setNotesList] = useState<HandoverNote[]>([]);
  const [activeShifts, setActiveShifts] = useState<Shift[]>([]);
  const [loading, setLoading] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const inFlightRef = useRef(false); // synchronous double-submit guard
  const [showForm, setShowForm] = useState(false);

  // Form state
  const [shiftType, setShiftType] = useState<ShiftType>("morning");
  const [summary, setSummary] = useState<string>("");
  const [urgentFlag, setUrgentFlag] = useState<boolean>(false);

  const loadHandovers = useCallback(async () => {
    try {
      const [notesData, shiftsData] = await Promise.all([
        fetchApi<HandoverNote[]>("/handovers?limit=25"),
        fetchApi<Shift[]>("/api/v1/shifts"),
      ]);
      setNotesList(notesData || []);
      const validShifts = shiftsData || [];
      setActiveShifts(validShifts);
      if (validShifts.length > 0) {
        setShiftType((prev) => {
          if (!validShifts.some((s) => s.id === prev)) {
            return validShifts[0].id;
          }
          return prev;
        });
      }
    } catch (err) {
      console.warn("Failed fetching handovers:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadHandovers();
  }, [loadHandovers]);

  // Live SSE listener
  const { status: connectionStatus } = useLiveSync({
    onNoteAdded: () => {
      loadHandovers();
    },
    onShiftUpdated: () => {
      loadHandovers();
    },
    onResync: () => {
      loadHandovers();
    },
  });

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!summary.trim()) return;

    if (!currentCaregiver) {
      openPinModal();
      return;
    }

    if (!canTakeActions(currentCaregiver.role)) {
      alert(locale === "ar" ? "حسابك مخصص للمشاهدة فقط ولا يملك صلاحية إضافة ملاحظات تسليم الوردية" : "Viewer accounts cannot post handover notes");
      return;
    }

    if (inFlightRef.current) return;
    inFlightRef.current = true;
    setIsSubmitting(true);
    try {
      await fetchApi("/handovers", {
        method: "POST",
        body: JSON.stringify({
          shift_type: shiftType,
          summary: summary.trim(),
          urgent_flag: urgentFlag,
        }),
      });

      setSummary("");
      setUrgentFlag(false);
      setShowForm(false);
      loadHandovers();
    } catch (err: any) {
      alert(err.message || "Failed to post handover note");
    } finally {
      inFlightRef.current = false;
      setIsSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen">
      <CaregiverHeader connectionStatus={connectionStatus} />

      <main className="mx-auto max-w-3xl px-4 py-5 sm:px-6">
        {/* Header */}
        <div className="mb-6 flex items-center justify-between gap-3">
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-xl sm:text-2xl font-black text-slate-900 dark:text-white tracking-tight">
                {t.handoversTitle}
              </h1>
              <ClipboardList className="h-5 w-5 text-emerald-600 dark:text-emerald-400" />
            </div>
            <p className="text-xs text-slate-500 dark:text-slate-400 mt-1">
              {locale === "ar"
                ? "تسليم المهام والملاحظات الحيوية بين الأخوة عبر الورديات"
                : "Smooth shift handovers and critical sibling updates"}
            </p>
          </div>

          {!isViewer(currentCaregiver?.role) && (
            <button
              type="button"
              onClick={() => setShowForm(!showForm)}
              aria-expanded={showForm}
              className="flex min-h-12 shrink-0 items-center gap-1.5 rounded-xl bg-emerald-600 hover:bg-emerald-700 px-3.5 py-2 text-xs font-bold text-white shadow-emerald-500/20 shadow-sm transition-all active:scale-95"
            >
              <Plus className="h-4 w-4" />
              <span>{t.newHandover}</span>
            </button>
          )}
        </div>

        {/* New Handover Note Form */}
        {showForm && (
          <form
            onSubmit={handleSubmit}
            className="mb-8 rounded-3xl border border-slate-200 bg-white p-5 shadow-lg dark:border-slate-800 dark:bg-slate-900 animate-in fade-in slide-in-from-top-4 duration-200"
          >
            <h2 className="text-base font-bold text-slate-900 dark:text-white mb-4">
              {t.newHandover}
            </h2>

            {/* Shift Window Selector */}
            <div className="mb-4">
              <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300 mb-2">
                {t.shiftType}
              </label>
              <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
                {activeShifts.length > 0
                  ? activeShifts.map((s) => (
                      <button
                        type="button"
                        key={s.id}
                        onClick={() => setShiftType(s.id)}
                        aria-pressed={shiftType === s.id}
                        className={`min-h-12 py-2 px-3 rounded-xl border text-xs font-bold transition-all flex flex-col items-center justify-center gap-0.5 ${
                          shiftType === s.id
                            ? "border-emerald-600 bg-emerald-50 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300 ring-2 ring-emerald-500/20"
                            : "border-slate-200 text-slate-600 hover:border-slate-300 dark:border-slate-800 dark:text-slate-400"
                        }`}
                      >
                        <span>{locale === "ar" ? s.name_ar : s.name_en}</span>
                        <span className="text-[10px] opacity-75 font-mono">
                          {s.start_time} - {s.end_time}
                        </span>
                      </button>
                    ))
                  : (["morning", "evening", "night"] as const).map((st) => (
                      <button
                        type="button"
                        key={st}
                        onClick={() => setShiftType(st)}
                        aria-pressed={shiftType === st}
                        className={`min-h-12 py-2 px-3 rounded-xl border text-xs font-bold transition-all ${
                          shiftType === st
                            ? "border-emerald-600 bg-emerald-50 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300 ring-2 ring-emerald-500/20"
                            : "border-slate-200 text-slate-600 hover:border-slate-300 dark:border-slate-800 dark:text-slate-400"
                        }`}
                      >
                        {t.shifts[st]}
                      </button>
                    ))}
              </div>
            </div>

            {/* Urgent Notice Toggle */}
            <div className="mb-4">
              <label className="flex min-h-12 items-center gap-2 p-3 rounded-xl border border-rose-200 bg-rose-50/50 dark:border-rose-900/60 dark:bg-rose-950/20 cursor-pointer">
                <input
                  type="checkbox"
                  checked={urgentFlag}
                  onChange={(e) => setUrgentFlag(e.target.checked)}
                  className="h-5 w-5 shrink-0 rounded-sm text-rose-600 focus:ring-rose-500"
                />
                <span className="flex items-center gap-1.5 text-xs font-bold text-rose-700 dark:text-rose-400">
                  <AlertTriangle className="h-4 w-4" />
                  {t.urgentFlag}
                </span>
              </label>
            </div>

            {/* Note text */}
            <div className="mb-4">
              <textarea
                required
                rows={4}
                value={summary}
                onChange={(e) => setSummary(e.target.value)}
                placeholder={t.summaryPlaceholder}
                aria-label={t.newHandover}
                className="w-full rounded-2xl border border-slate-200 bg-slate-50 p-3 text-base text-slate-900 focus:border-emerald-500 focus:bg-white focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
              />
            </div>

            {/* Form actions */}
            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setShowForm(false)}
                className="min-h-12 rounded-xl px-4 py-2 text-xs font-semibold text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800"
              >
                {t.cancel}
              </button>
              <button
                type="submit"
                disabled={isSubmitting}
                aria-busy={isSubmitting}
                className="flex min-h-12 items-center gap-1.5 rounded-xl bg-emerald-600 hover:bg-emerald-700 px-5 py-2 text-xs font-bold text-white shadow-emerald-500/20 shadow-sm transition-all active:scale-95 disabled:opacity-50"
              >
                {isSubmitting ? (
                  <>
                    <Loader2 className="h-4 w-4 animate-spin" />
                    <span>{t.saving}</span>
                  </>
                ) : (
                  <>
                    <Send className="h-3.5 w-3.5" />
                    <span>{t.postNote}</span>
                  </>
                )}
              </button>
            </div>
          </form>
        )}

        {/* Handover Notes Timeline */}
        <div className="space-y-3">
          {loading ? (
            <div className="space-y-3">
              {[1, 2, 3].map((i) => (
                <div
                  key={i}
                  className="h-24 w-full animate-pulse rounded-2xl bg-slate-200 dark:bg-slate-800"
                />
              ))}
            </div>
          ) : notesList.length === 0 ? (
            <div className="rounded-2xl border border-dashed border-slate-300 p-8 text-center dark:border-slate-800">
              <ClipboardList className="mx-auto h-8 w-8 text-slate-400 mb-2" />
              <p className="text-sm font-semibold text-slate-600 dark:text-slate-300">
                {locale === "ar" ? "لا توجد ملاحظات وردية مسجلة" : "No handover notes recorded"}
              </p>
            </div>
          ) : (
            notesList.map((note) => {
              const dateStr = formatShortDate(note.created_at, locale);
              const timeStr = formatTime(note.created_at, locale);

              return (
                <div
                  key={note.id}
                  className={`rounded-2xl border p-4 shadow-xs transition-all ${
                    note.urgent_flag
                      ? "border-rose-300 bg-rose-50/70 dark:border-rose-900/60 dark:bg-rose-950/20"
                      : "border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900"
                  }`}
                >
                  <div className="flex items-center justify-between gap-2 mb-2">
                    <div className="flex items-center gap-2">
                      <span
                        className="flex h-7 w-7 items-center justify-center rounded-full text-xs font-bold text-white shadow-xs"
                        style={{ backgroundColor: note.avatar_color || "#059669" }}
                      >
                        {note.caregiver_name?.slice(0, 1)}
                      </span>
                      <span className="text-xs font-bold text-slate-900 dark:text-white">
                        {note.caregiver_name}
                      </span>
                      {(() => {
                        const matchedShift = activeShifts.find((s) => s.id === note.shift_type);
                        const label = matchedShift
                          ? `${locale === "ar" ? matchedShift.name_ar : matchedShift.name_en} (${matchedShift.start_time} - ${matchedShift.end_time})`
                          : (t.shifts as Record<string, string>)[note.shift_type] || note.shift_type;
                        return (
                          <span
                            className="rounded-md px-2 py-0.5 text-[10px] font-semibold bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300"
                            style={
                              matchedShift?.color
                                ? { borderInlineStart: `3px solid ${matchedShift.color}` }
                                : undefined
                            }
                          >
                            {label}
                          </span>
                        );
                      })()}
                    </div>

                    <div className="flex items-center gap-1 text-[11px] text-slate-400">
                      <Clock className="h-3 w-3" />
                      <Ltr>{dateStr} • {timeStr}</Ltr>
                    </div>
                  </div>

                  {note.urgent_flag && (
                    <div className="mb-2 inline-flex items-center gap-1 rounded-full bg-rose-600 px-2.5 py-0.5 text-[10px] font-bold text-white">
                      <AlertTriangle className="h-3 w-3" />
                      <span>{t.urgentFlag}</span>
                    </div>
                  )}

                  <p className="text-xs text-slate-800 dark:text-slate-200 whitespace-pre-wrap leading-relaxed">
                    {note.summary}
                  </p>
                </div>
              );
            })
          )}
        </div>
      </main>
    </div>
  );
}
