"use client";

import React, { useState, useEffect, useCallback, useRef } from "react";
import { CaregiverHeader } from "@/components/CaregiverHeader";
import { useLanguage } from "@/context/LanguageContext";
import { useCaregiver } from "@/context/CaregiverContext";
import { useLiveSync } from "@/hooks/useLiveSync";
import { HandoverNote, ShiftType, Shift, CurrentDutyStatus } from "@/lib/types";
import { canTakeActions, isViewer } from "@/lib/permissions";
import { fetchApi } from "@/lib/api";
import { firstName, saveAs } from "@/lib/actor";
import { HANDOVER_TAGS, isUrgentTag, tagLabel } from "@/lib/handoverTags";
import { VoiceNoteButton } from "@/components/VoiceNoteButton";
import { enqueue, isNetworkError, newRequestId, OUTBOX_RESULT_EVENT } from "@/lib/outbox";
import { formatShortDate, formatTime } from "@/lib/datetime";
import { Ltr } from "@/components/Ltr";
import { ClipboardList, AlertTriangle, Clock, Send, Loader2, ArrowRightLeft, Check } from "lucide-react";

export default function HandoversPage() {
  const { t, locale } = useLanguage();
  const { currentCaregiver, openPinModal, allCaregivers } = useCaregiver();
  const ar = locale === "ar";

  const [notesList, setNotesList] = useState<HandoverNote[]>([]);
  const [activeShifts, setActiveShifts] = useState<Shift[]>([]);
  const [loading, setLoading] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const inFlightRef = useRef(false); // synchronous double-submit guard
  const [duty, setDuty] = useState<CurrentDutyStatus | null>(null);
  const [formError, setFormError] = useState("");
  const [sentMessage, setSentMessage] = useState("");

  // Form state
  const [shiftType, setShiftType] = useState<ShiftType>("morning");
  const [summary, setSummary] = useState<string>("");
  const [urgentFlag, setUrgentFlag] = useState<boolean>(false);
  // Issue #18: quick tags and "pass duty to".
  const [tags, setTags] = useState<string[]>([]);
  const [passTo, setPassTo] = useState<string | null>(null);
  const tagUrgent = tags.some(isUrgentTag);

  const loadHandovers = useCallback(async () => {
    try {
      // Independent requests: a shifts failure must never hide the notes themselves.
      const [notesRes, shiftsRes, dutyRes] = await Promise.allSettled([
        fetchApi<HandoverNote[]>("/handovers?limit=25"),
        fetchApi<Shift[]>("/shifts"),
        fetchApi<CurrentDutyStatus>("/shifts/current-duty"),
      ]);
      setDuty(dutyRes.status === "fulfilled" ? dutyRes.value : null);
      if (notesRes.status === "fulfilled") setNotesList(notesRes.value || []);
      else console.warn("Failed fetching handovers:", notesRes.reason);
      const validShifts = shiftsRes.status === "fulfilled" ? shiftsRes.value || [] : [];
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

  // A note saved offline was sent: show it (issue #16).
  useEffect(() => {
    window.addEventListener(OUTBOX_RESULT_EVENT, loadHandovers);
    return () => window.removeEventListener(OUTBOX_RESULT_EVENT, loadHandovers);
  }, [loadHandovers]);

  useEffect(() => {
    loadHandovers();
  }, [loadHandovers]);

  const toggleTag = (key: string) => setTags((prev) => (prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key]));

  // Only the person on duty now can pass it on.
  const onDutyNow = !!currentCaregiver && duty?.is_on_duty && duty.active_assignment?.caregiver_id === currentCaregiver.id;
  const passTargets = allCaregivers.filter((c) => c.id !== currentCaregiver?.id);
  const passToName = firstName(passTargets.find((c) => c.id === passTo)?.name);

  // Live SSE listener
  const { status: connectionStatus } = useLiveSync({
    onNoteAdded: () => {
      loadHandovers();
    },
    onShiftUpdated: () => {
      loadHandovers();
    },
    onDutyUpdated: () => {
      loadHandovers();
    },
    onEvent: (ev) => {
      if (ev.type === "DUTY_PASSED" || ev.type === "DUTY_ACCEPTED") loadHandovers();
    },
    onResync: () => {
      loadHandovers();
    },
  });

  const resetForm = () => {
    setSummary("");
    setTags([]);
    setPassTo(null);
    setUrgentFlag(false);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError("");
    setSentMessage("");
    if (!summary.trim() && tags.length === 0) {
      setFormError(ar ? "اختر ملاحظة واحدة على الأقل أو اكتب شيئاً" : "Tap at least one item or write a note");
      return;
    }

    if (!currentCaregiver) {
      openPinModal();
      return;
    }

    if (!canTakeActions(currentCaregiver.role)) {
      setFormError(ar ? "حسابك مخصص للمشاهدة فقط ولا يملك صلاحية إضافة ملاحظات تسليم الوردية" : "Viewer accounts cannot post handover notes");
      return;
    }

    if (inFlightRef.current) return;
    inFlightRef.current = true;
    setIsSubmitting(true);
    const note = { shift_type: shiftType, summary: summary.trim(), urgent_flag: urgentFlag || tagUrgent, tags };
    // The same id is reused if this has to be resent from the outbox (issue #16).
    const requestId = newRequestId();
    try {
      if (passTo) {
        // Passing duty moves the roster, so it is never queued offline: it must
        // happen now or visibly fail.
        try {
          await fetchApi("/shifts/handover", {
            method: "POST",
            body: JSON.stringify({ to_caregiver_id: passTo, tags, summary: note.summary, urgent_flag: note.urgent_flag }),
          });
        } catch (err) {
          if (isNetworkError(err)) throw new Error(ar ? "لا يوجد اتصال — لم تُسلَّم المناوبة. حاول مجدداً." : "No connection — duty was not passed. Try again.");
          const code = (err as { data?: { error?: string } }).data?.error;
          if (code === "NOT_ON_DUTY") throw new Error(ar ? "لست المناوب الآن، لذلك لا يمكنك تسليم المناوبة." : "You are not on duty now, so you can't pass it on.");
          if (code === "CANNOT_TAKE_DUTY") throw new Error(ar ? "هذا الحساب للمشاهدة فقط ولا يمكنه استلام المناوبة." : "This account is view-only and can't take duty.");
          throw err;
        }
        setSentMessage(ar ? `سُلّمت المناوبة إلى ${passToName}. سيصلها تنبيه لقبولها.` : `Duty passed to ${passToName}. They'll get an alert to accept it.`);
      } else {
        try {
          await fetchApi("/handovers", { method: "POST", body: JSON.stringify({ ...note, client_request_id: requestId }) });
        } catch (err) {
          if (!isNetworkError(err)) throw err;
          await enqueue({
            id: requestId,
            kind: "note",
            endpoint: "/handovers",
            body: note,
            caregiver_id: currentCaregiver.id,
            caregiver_name: currentCaregiver.name,
            label_ar: "ملاحظة تسليم",
            label_en: "handover note",
          });
        }
      }
      resetForm();
      loadHandovers();
    } catch (err: any) {
      setFormError(err.message || (ar ? "تعذر الحفظ" : "Failed to post handover note"));
    } finally {
      inFlightRef.current = false;
      setIsSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen">
      <CaregiverHeader connectionStatus={connectionStatus} />

      <main className="mx-auto max-w-3xl px-4 py-5 sm:px-6 lg:max-w-none lg:px-8">
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

        </div>

        {/* Quick handover (issue #18): tap what happened, optionally pass duty, send. */}
        {!isViewer(currentCaregiver?.role) && (
          <form
            onSubmit={handleSubmit}
            data-testid="handover-form"
            className="mb-8 rounded-3xl border border-slate-200 bg-white p-4 sm:p-5 shadow-lg dark:border-slate-800 dark:bg-slate-900"
          >
            <h2 className="mb-3 text-base font-bold text-slate-900 dark:text-white">
              {ar ? "ماذا حدث في ورديتك؟ اضغط ما ينطبق" : "How was your shift? Tap what applies"}
            </h2>

            <div className="mb-4 grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
              {HANDOVER_TAGS.map((tag) => {
                const on = tags.includes(tag.key);
                return (
                  <button
                    type="button"
                    key={tag.key}
                    aria-pressed={on}
                    onClick={() => toggleTag(tag.key)}
                    className={`flex min-h-14 items-center justify-center gap-1.5 rounded-2xl border-2 px-2 text-center text-sm font-bold leading-tight transition-all active:scale-95 ${
                      on
                        ? tag.urgent
                          ? "border-rose-600 bg-rose-600 text-white"
                          : "border-emerald-600 bg-emerald-600 text-white"
                        : tag.urgent
                          ? "border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200"
                          : "border-slate-200 bg-slate-50 text-slate-700 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"
                    }`}
                  >
                    {on && <Check className="h-4 w-4 shrink-0" />}
                    <span>{ar ? tag.ar : tag.en}</span>
                  </button>
                );
              })}
            </div>

            {tagUrgent && (
              <p className="mb-4 flex items-center gap-1.5 rounded-xl bg-rose-50 p-3 text-sm font-bold text-rose-800 dark:bg-rose-950/40 dark:text-rose-200">
                <AlertTriangle className="h-4 w-4 shrink-0" />
                {ar ? "ستُعلَّم الملاحظة كعاجلة" : "This note will be marked urgent"}
              </p>
            )}

            {/* Optional free text, typed or spoken */}
            <div className="mb-4">
              <textarea
                rows={2}
                value={summary}
                onChange={(e) => setSummary(e.target.value)}
                placeholder={ar ? "شيء آخر؟ (اختياري)" : "Anything else? (optional)"}
                aria-label={ar ? "ملاحظة إضافية" : "Extra note"}
                className="w-full rounded-2xl border border-slate-200 bg-slate-50 p-3 text-base text-slate-900 focus:border-emerald-500 focus:bg-white focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
              />
              <div className="mt-2 flex justify-end">
                <VoiceNoteButton ar={ar} onText={(text) => setSummary((prev) => (prev.trim() ? `${prev.trim()} ${text}` : text))} />
              </div>
            </div>

            {/* Pass duty: only the person on duty now */}
            {onDutyNow && passTargets.length > 0 && (
              <div className="mb-4">
                <p className="mb-2 flex items-center gap-1.5 text-sm font-bold text-slate-700 dark:text-slate-200">
                  <ArrowRightLeft className="h-4 w-4" />
                  {ar ? "سلّم المناوبة إلى (اختياري)" : "Pass duty to (optional)"}
                </p>
                <div className="flex flex-wrap gap-2">
                  {passTargets.map((c) => {
                    const on = passTo === c.id;
                    return (
                      <button
                        type="button"
                        key={c.id}
                        aria-pressed={on}
                        onClick={() => setPassTo(on ? null : c.id)}
                        className={`flex min-h-14 items-center gap-2 rounded-2xl border-2 pe-4 ps-2 text-sm font-bold active:scale-95 ${
                          on ? "border-blue-600 bg-blue-600 text-white" : "border-slate-200 text-slate-700 dark:border-slate-700 dark:text-slate-200"
                        }`}
                      >
                        <span className="flex h-9 w-9 items-center justify-center rounded-full text-white" style={{ backgroundColor: c.avatar_color }}>
                          {c.name.slice(0, 1)}
                        </span>
                        {ar ? `إلى ${firstName(c.name)}` : `To ${firstName(c.name)}`}
                      </button>
                    );
                  })}
                </div>
              </div>
            )}

            {/* Shift window: only for a plain note (passing duty uses the current shift) */}
            {!passTo && (
              <details className="mb-4">
                <summary className="min-h-12 cursor-pointer py-3 text-xs font-semibold text-slate-600 dark:text-slate-300">
                  {t.shiftType}:{" "}
                  {(() => {
                    const s = activeShifts.find((x) => x.id === shiftType);
                    return s ? (ar ? s.name_ar : s.name_en) : (t.shifts as Record<string, string>)[shiftType] || shiftType;
                  })()}
                </summary>
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
                          <span>{ar ? s.name_ar : s.name_en}</span>
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
              </details>
            )}

            {/* Urgent toggle (pain / swallowing make it urgent anyway) */}
            {!tagUrgent && (
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
            )}

            {formError && (
              <p role="alert" className="mb-3 rounded-xl bg-rose-50 p-3 text-sm font-bold text-rose-800 dark:bg-rose-950/40 dark:text-rose-200">
                {formError}
              </p>
            )}
            {sentMessage && (
              <p role="status" className="mb-3 rounded-xl bg-emerald-50 p-3 text-sm font-bold text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200">
                {sentMessage}
              </p>
            )}

            <button
              type="submit"
              disabled={isSubmitting}
              aria-busy={isSubmitting}
              className={`flex min-h-14 w-full items-center justify-center gap-2 rounded-2xl px-5 text-base font-black text-white shadow-sm transition-all active:scale-95 disabled:opacity-50 ${
                passTo ? "bg-blue-600 hover:bg-blue-700" : "bg-emerald-600 hover:bg-emerald-700"
              }`}
            >
              {isSubmitting ? (
                <>
                  <Loader2 className="h-5 w-5 animate-spin" />
                  <span>{t.saving}</span>
                </>
              ) : passTo ? (
                <>
                  <ArrowRightLeft className="h-5 w-5" />
                  <span>{ar ? `أرسل وسلّم المناوبة إلى ${passToName}` : `Send and pass duty to ${passToName}`}</span>
                </>
              ) : (
                <>
                  <Send className="h-5 w-5" />
                  <span>{saveAs(currentCaregiver?.name, ar, t.postNote)}</span>
                </>
              )}
            </button>
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

                  {note.tags && note.tags.length > 0 && (
                    <div className="mb-1.5 flex flex-wrap gap-1.5">
                      {note.tags.map((k) => (
                        <span
                          key={k}
                          className={`rounded-full px-2.5 py-1 text-xs font-bold ${
                            isUrgentTag(k) ? "bg-rose-100 text-rose-800 dark:bg-rose-950 dark:text-rose-200" : "bg-emerald-50 text-emerald-800 dark:bg-emerald-950/50 dark:text-emerald-200"
                          }`}
                        >
                          {tagLabel(k, ar)}
                        </span>
                      ))}
                    </div>
                  )}

                  {note.summary && (
                    <p className="text-xs text-slate-800 dark:text-slate-200 whitespace-pre-wrap leading-relaxed">
                      {note.summary}
                    </p>
                  )}

                  {note.passed_to_name && (
                    <p className="mt-2 flex items-center gap-1.5 text-xs font-bold text-blue-800 dark:text-blue-300">
                      <ArrowRightLeft className="h-3.5 w-3.5" />
                      <span>
                        {ar ? "سُلّمت المناوبة إلى" : "Duty passed to"} <bdi>{note.passed_to_name}</bdi>
                        {" — "}
                        {note.accepted_at
                          ? ar ? `قبلها ${formatTime(note.accepted_at, locale)}` : `accepted ${formatTime(note.accepted_at, locale)}`
                          : ar ? "بانتظار القبول" : "waiting to be accepted"}
                      </span>
                    </p>
                  )}
                </div>
              );
            })
          )}
        </div>
      </main>
    </div>
  );
}
